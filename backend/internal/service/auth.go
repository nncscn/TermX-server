// Package service 承载登录业务：零知识保险库的钥匙管理、登录防爆破、会话签发与初始账户播种。
// 零知识模型：登录只比对 PBKDF2 单向哈希；业务数据的库密钥（vaultKey）只在
// 内存中存在（登录/验密成功时解包），磁盘上仅有两种包裹形态（主密码派生密钥
package service

// ⚠ 信任模型声明（安全审计 M1）：运行期服务器在内存持有库密钥，并在登录响应中
// 全量数据泄露；HTTP 明文下 data_key 可被同网段嗅探。详见 docs/信任模型与安全边界.md

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"ky/internal/model"
	"ky/internal/pkg/captcha"
	"ky/internal/pkg/console"
	"ky/internal/pkg/keyring"
	"ky/internal/pkg/vault"
	"ky/internal/repository"
)

// 防爆破参数（与前端规则一致：连错 5 次起需验证码，10 次锁定 60 秒）。
const (
	maxFailedAttempts = 10
	captchaAfter      = 5
	lockDuration      = 60 * time.Second
	sessionTTL        = 7 * 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("账户或密码错误")
	ErrPasswordWrong      = errors.New("主密码不正确")
	ErrVerifyLocked       = errors.New("尝试次数过多，请稍后再试")
	ErrAccountLocked      = errors.New("尝试次数过多，请稍后再试")
	ErrRecoveryNotBound   = errors.New("该账户未绑定恢复密钥")
	ErrRecoveryInvalid    = errors.New("恢复密钥不正确")
	ErrResetTicketInvalid = errors.New("重置凭证无效或已过期")
	ErrCaptchaRequired    = errors.New("请完成安全验证")
	ErrCaptchaWrong       = errors.New("验证码不正确，请重试")
	// 需重新登录以重建缓存；映射 40101 触发前端自动跳登录
	ErrVaultGone          = errors.New("登录状态已随服务重启失效，请重新登录")
	ErrVaultLegacy        = errors.New("账户需要升级，请退出后重新登录一次")
	ErrRecoveryUnmigrated = errors.New("请先登录一次完成账户升级，再使用找回功能")
)

type resetTicket struct {
	accountID uint
	expiresAt time.Time
}

type verifyFail struct {
	count       int
	lockedUntil time.Time
}

// 防爆破参数：主密码验证连错 5 次起锁定 60 秒（与登录的验证码阈值一致）。
const (
	verifyLockAfter = 5
	verifyLockTime  = 60 * time.Second
)

type AuthService struct {
	repo     *repository.AuthRepository
	keys     *repository.SshKeyRepository
	creds    *repository.CredentialRepository
	legacy   *keyring.Keyring // 仅用于旧数据（PasswordCipher 架构）迁移时解密
	captchas *captcha.Store
	tickets  map[string]resetTicket
	verify   map[uint]verifyFail
	vaults   map[uint][]byte // accountID → 内存中的库密钥（进程重启即清零）
	// AfterVaultReady 库密钥进入内存后的回调（router 注入：TermX 同步条目懒翻译）。
	AfterVaultReady func(accountID uint)
	mu              sync.Mutex
}

func NewAuthService(
	repo *repository.AuthRepository,
	keys *repository.SshKeyRepository,
	creds *repository.CredentialRepository,
	legacy *keyring.Keyring,
) *AuthService {
	return &AuthService{
		repo:     repo,
		keys:     keys,
		creds:    creds,
		legacy:   legacy,
		captchas: captcha.NewStore(),
		tickets:  map[string]resetTicket{},
		verify:   map[uint]verifyFail{},
		vaults:   map[uint][]byte{},
	}
}

// CaptchaPoint 前端上报的点击点（原图坐标）。
type CaptchaPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// GetCaptcha 生成一道汉字点选验证码（id + 图 + 提示语;答案只在内存）
func (s *AuthService) GetCaptcha() map[string]any {
	c := s.captchas.Generate()
	return map[string]any{
		"captcha_id": c.ID,
		"image":      c.SVGDataURI(),
		"prompt":     c.Prompt,
		"ttl":        60,
	}
}

// LoginResult 登录结果：Token 为原始令牌（仅此一次返回）,服务端只存其哈希;
type LoginResult struct {
	Token               string
	ExpiresAt           time.Time
	MigratedRecoveryKey string
	DataKey             string
}

// Login 校验账户密码,成功签发会话令牌并解包库密钥入内存。
// 旧架构账户（存有 PasswordCipher）在密码验证通过后自动迁移为零知识结构，
// TODO: 多账户支持以后再说
func (s *AuthService) Login(ctx context.Context, username, password, captchaID string, captchaClicks []CaptchaPoint) (*LoginResult, error) {
	// 验证码校验最先执行（与账户是否存在无关，防枚举与状态不同步）
	if captchaID != "" {
		pts := make([]captcha.Point, len(captchaClicks))
		for i, c := range captchaClicks {
			pts[i] = captcha.Point{X: c.X, Y: c.Y}
		}
		if !s.captchas.Consume(captchaID, pts) {
			return nil, ErrCaptchaWrong
		}
	}

	account, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			//P5：做一次假 PBKDF2 计算拉平响应时间——使"用户不存在"与
			//"密码错误"不可通过时间差区分（防用户名枚举）
			_ = vault.Derive("timing-equalizer", vault.RandomSalt())
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("查询账户: %w", err)
	}

	// 锁定期内直接拒绝，不消耗比对逻辑
	if account.LockedUntil != nil && account.LockedUntil.After(time.Now()) {
		return nil, ErrAccountLocked
	}

	if account.FailedAttempts >= captchaAfter && captchaID == "" {
		return nil, ErrCaptchaRequired
	}

	// ---- 密码验证：新架构比对哈希;旧架构解密比对并触发迁移 ----
	migratedRecovery := ""
	loginDataKey := ""
	if len(account.AuthHash) > 0 {
		if subtle.ConstantTimeCompare(
			vault.Derive(password, account.AuthSalt), account.AuthHash) != 1 {
			s.recordFailure(ctx, account)
			return nil, ErrInvalidCredentials
		}
		vaultKey, err := vault.Open(vault.Derive(password, account.VaultSalt), account.VaultCipher)
		if err != nil {
			s.recordFailure(ctx, account)
			return nil, ErrInvalidCredentials
		}
		s.putVault(account.ID, vaultKey)
		loginDataKey = base64.StdEncoding.EncodeToString(vaultKey)
	} else if len(account.PasswordCipher) > 0 {
		plain, err := s.legacy.Decrypt(account.PasswordCipher)
		if err != nil {
			return nil, fmt.Errorf("解密旧密码: %w", err)
		}
		if subtle.ConstantTimeCompare(plain, []byte(password)) != 1 {
			s.recordFailure(ctx, account)
			return nil, ErrInvalidCredentials
		}
		recovery, err := newRecoveryKey()
		if err != nil {
			return nil, fmt.Errorf("生成恢复密钥: %w", err)
		}
		if err := s.migrateAccount(ctx, account, password, recovery); err != nil {
			return nil, err
		}
		migratedRecovery = recovery
		loginDataKey = base64.StdEncoding.EncodeToString(s.mustVault(account.ID))
	} else {
		return nil, fmt.Errorf("账户数据异常（既无哈希也无旧密文）")
	}

	// 成功登录：清零失败计数
	if account.FailedAttempts != 0 || account.LockedUntil != nil {
		account.FailedAttempts = 0
		account.LockedUntil = nil
		if err := s.repo.SaveLoginState(ctx, account); err != nil {
			return nil, fmt.Errorf("重置登录状态: %w", err)
		}
	}

	token := randomToken()
	session := &model.Session{
		TokenHash: hashToken(token),
		AccountID: account.ID,
		ExpiresAt: time.Now().Add(sessionTTL),
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("创建会话: %w", err)
	}
	return &LoginResult{
		Token:               token,
		ExpiresAt:           session.ExpiresAt,
		MigratedRecoveryKey: migratedRecovery,
		DataKey:             loginDataKey,
	}, nil
}

func (s *AuthService) mustVault(accountID uint) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	vk := s.vaults[accountID]
	out := make([]byte, len(vk))
	copy(out, vk)
	return out
}

// migrateAccount 旧架构 → 零知识架构：生成库密钥与双包裹、改存登录哈希,
// 并把该账户全部密钥/凭据密文从旧密钥文件体系重新加密到新库密钥下
func (s *AuthService) migrateAccount(ctx context.Context, account *model.Account, password, recovery string) error {
	vaultSalt := vault.RandomSalt()
	vaultKey := vault.RandomKey()
	vaultCipher, err := vault.Seal(vault.Derive(password, vaultSalt), vaultKey)
	if err != nil {
		return fmt.Errorf("包裹库密钥: %w", err)
	}
	recoveryCipher, err := vault.Seal(vault.Derive(recovery, vaultSalt), vaultKey)
	if err != nil {
		return fmt.Errorf("包裹库密钥(恢复): %w", err)
	}
	authSalt := vault.RandomSalt()
	authHash := vault.Derive(password, authSalt)

	// 重加密该账户全部业务密文（旧密钥文件 → 新库密钥）
	keyRows, _, err := s.keys.List(ctx, account.ID, repository.KeyFilter{IncludeDeleted: true})
	if err != nil {
		return fmt.Errorf("读取密钥(迁移): %w", err)
	}
	for i := range keyRows {
		plain, err := s.legacy.Decrypt(keyRows[i].Secret)
		if err != nil {
			return fmt.Errorf("解密旧密钥内容: %w", err)
		}
		if keyRows[i].Secret, err = vault.Seal(vaultKey, plain); err != nil {
			return fmt.Errorf("重加密密钥内容: %w", err)
		}
		if err := s.keys.Save(ctx, &keyRows[i]); err != nil {
			return fmt.Errorf("保存密钥(迁移): %w", err)
		}
	}
	credRows, _, err := s.creds.List(ctx, account.ID, repository.CredFilter{IncludeDeleted: true})
	if err != nil {
		return fmt.Errorf("读取凭据(迁移): %w", err)
	}
	for i := range credRows {
		plain, err := s.legacy.Decrypt(credRows[i].Secret)
		if err != nil {
			return fmt.Errorf("解密旧连接信息: %w", err)
		}
		if credRows[i].Secret, err = vault.Seal(vaultKey, plain); err != nil {
			return fmt.Errorf("重加密连接信息: %w", err)
		}
		if err := s.creds.Save(ctx, &credRows[i]); err != nil {
			return fmt.Errorf("保存凭据(迁移): %w", err)
		}
	}

	if err := s.repo.UpdateVault(ctx, account.ID, repository.VaultUpdate{
		AuthSalt:       authSalt,
		AuthHash:       authHash,
		VaultSalt:      vaultSalt,
		VaultCipher:    vaultCipher,
		RecoveryCipher: recoveryCipher,
		RecoveryHash:   hashRecovery(recovery),
		ClearLegacy:    true,
	}); err != nil {
		return fmt.Errorf("更新账户(迁移): %w", err)
	}
	s.putVault(account.ID, vaultKey)
	log.Printf("账户 %s 已迁移至零知识加密结构（旧密文已作废）", account.Username)
	return nil
}

func (s *AuthService) VaultKey(accountID uint) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vk, ok := s.vaults[accountID]
	if !ok || len(vk) == 0 {
		return nil, ErrVaultGone
	}
	out := make([]byte, len(vk))
	copy(out, vk)
	return out, nil
}

func (s *AuthService) putVault(accountID uint, vaultKey []byte) {
	s.mu.Lock()
	s.vaults[accountID] = vaultKey
	s.mu.Unlock()
	if s.AfterVaultReady != nil {
		go s.AfterVaultReady(accountID)
	}
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	if err := s.repo.DeleteSessionByToken(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("删除会话: %w", err)
	}
	return nil
}

// 滑动续期（只延长不缩短）：剩余有效期不足 3 天时自动续到完整的 7 天——
func (s *AuthService) ValidateToken(ctx context.Context, token string) (*model.Session, error) {
	session, err := s.repo.GetValidSession(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("查询会话: %w", err)
	}
	if time.Until(session.ExpiresAt) < sessionTTL/2 {
		_ = s.repo.ExtendSession(ctx, session.ID, time.Now().Add(sessionTTL))
	}
	return session, nil
}

// SeedAdmin 账户表为空时播种初始管理员（零知识结构；恢复密钥明文仅日志展示一次）。
func (s *AuthService) SeedAdmin(ctx context.Context, username, password string) error {
	n, err := s.repo.CountAccounts(ctx)
	if err != nil {
		return fmt.Errorf("统计账户: %w", err)
	}
	if n > 0 {
		return nil
	}
	recovery, err := CreateVaultAccount(ctx, s.repo, username, password, vault.RandomKey())
	if err != nil {
		return fmt.Errorf("创建初始账户: %w", err)
	}
	console.Secret("初始账户 "+username+" 的恢复密钥 · 仅此一次展示，请抄写备份", recovery)
	return nil
}

// CreateVaultAccount 在指定仓库上创建零知识结构的账户（安装引导与初始播种共用）。
func CreateVaultAccount(ctx context.Context, repo *repository.AuthRepository, username, password string, vaultKey []byte) (string, error) {
	if len(vaultKey) != vault.KeyLen {
		return "", fmt.Errorf("库密钥长度不合法")
	}
	recovery, err := newRecoveryKey()
	if err != nil {
		return "", fmt.Errorf("生成恢复密钥: %w", err)
	}
	vaultSalt := vault.RandomSalt()
	vaultCipher, err := vault.Seal(vault.Derive(password, vaultSalt), vaultKey)
	if err != nil {
		return "", fmt.Errorf("包裹库密钥: %w", err)
	}
	recoveryCipher, err := vault.Seal(vault.Derive(recovery, vaultSalt), vaultKey)
	if err != nil {
		return "", fmt.Errorf("包裹库密钥(恢复): %w", err)
	}
	account := &model.Account{
		Username:       username,
		AuthSalt:       vault.RandomSalt(),
		VaultSalt:      vaultSalt,
		VaultCipher:    vaultCipher,
		RecoveryCipher: recoveryCipher,
		RecoveryHash:   hashRecovery(recovery),
	}
	account.AuthHash = vault.Derive(password, account.AuthSalt)
	if err := repo.CreateAccount(ctx, account); err != nil {
		return "", fmt.Errorf("创建账户: %w", err)
	}
	return recovery, nil
}

// 连错 verifyLockAfter 次锁定 verifyLockTime，成功即清零——防止持令牌者
// 无限暴力试主密码。旧架构账户（未迁移）返回 ErrVaultLegacy 提示重新登录
func (s *AuthService) VerifyPassword(ctx context.Context, accountID uint, password string) ([]byte, error) {
	// 频控检查与计数在同一临界区内完成，防并发请求穿透阈值
	s.mu.Lock()
	if st, ok := s.verify[accountID]; ok && time.Now().Before(st.lockedUntil) {
		s.mu.Unlock()
		return nil, ErrVerifyLocked
	}
	s.mu.Unlock()

	account, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrPasswordWrong
		}
		return nil, fmt.Errorf("查询账户: %w", err)
	}
	if len(account.AuthHash) == 0 {
		return nil, ErrVaultLegacy
	}
	vaultKey, err := vault.Open(vault.Derive(password, account.VaultSalt), account.VaultCipher)
	if err != nil {
		s.mu.Lock()
		st := s.verify[accountID]
		st.count++
		if st.count >= verifyLockAfter {
			st.lockedUntil = time.Now().Add(verifyLockTime)
			st.count = 0
		}
		s.verify[accountID] = st
		s.mu.Unlock()
		return nil, ErrPasswordWrong
	}
	s.mu.Lock()
	delete(s.verify, accountID)
	s.mu.Unlock()
	s.putVault(accountID, vaultKey)
	out := make([]byte, len(vaultKey))
	copy(out, vaultKey)
	return out, nil
}

// （业务数据无需重加密），更新登录哈希，并踢掉除当前会话外的全部旧会话。
func (s *AuthService) ChangePassword(ctx context.Context, accountID uint, oldPassword, newPassword, currentToken string) error {
	vaultKey, err := s.VerifyPassword(ctx, accountID, oldPassword)
	if err != nil {
		if errors.Is(err, ErrPasswordWrong) {
			return ErrInvalidCredentials // 保留"原密码不正确"的既有文案映射
		}
		return err
	}
	account, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("查询账户: %w", err)
	}
	vaultCipher, err := vault.Seal(vault.Derive(newPassword, account.VaultSalt), vaultKey)
	if err != nil {
		return fmt.Errorf("重包库密钥: %w", err)
	}
	authSalt := vault.RandomSalt()
	if err := s.repo.UpdateVault(ctx, accountID, repository.VaultUpdate{
		AuthSalt:    authSalt,
		AuthHash:    vault.Derive(newPassword, authSalt),
		VaultCipher: vaultCipher,
	}); err != nil {
		return fmt.Errorf("更新账户: %w", err)
	}
	// 保留当前会话，踢掉其他设备/被盗令牌
	if err := s.repo.DeleteSessionsByAccountExcept(ctx, accountID, hashToken(currentToken)); err != nil {
		return fmt.Errorf("清理旧会话: %w", err)
	}
	return nil
}

// GenerateRecoveryKey 为登录账户生成新恢复密钥：需验主密码取库密钥后重包恢复层，
// 新密钥只存哈希、明文仅返回一次
func (s *AuthService) GenerateRecoveryKey(ctx context.Context, accountID uint, password string) (string, error) {
	vaultKey, err := s.VerifyPassword(ctx, accountID, password)
	if err != nil {
		return "", err
	}
	account, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return "", fmt.Errorf("查询账户: %w", err)
	}
	recovery, err := newRecoveryKey()
	if err != nil {
		return "", fmt.Errorf("生成恢复密钥: %w", err)
	}
	recoveryCipher, err := vault.Seal(vault.Derive(recovery, account.VaultSalt), vaultKey)
	if err != nil {
		return "", fmt.Errorf("包裹库密钥(恢复): %w", err)
	}
	if err := s.repo.UpdateRecovery(ctx, accountID, recoveryCipher, hashRecovery(recovery)); err != nil {
		return "", fmt.Errorf("保存恢复密钥: %w", err)
	}
	return recovery, nil
}

func (s *AuthService) ForgotVerify(ctx context.Context, username, recoveryKey string) (string, error) {
	recoveryKey = normalizeRecoveryInput(recoveryKey)
	account, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", ErrRecoveryInvalid // 与密钥错误同文案，防枚举
		}
		return "", fmt.Errorf("查询账户: %w", err)
	}
	if account.RecoveryLockedUntil != nil && account.RecoveryLockedUntil.After(time.Now()) {
		return "", ErrAccountLocked
	}
	if account.RecoveryHash == "" {
		return "", ErrRecoveryNotBound
	}
	if subtle.ConstantTimeCompare([]byte(account.RecoveryHash), []byte(hashRecovery(recoveryKey))) != 1 {
		account.RecoveryAttempts++
		if account.RecoveryAttempts >= maxFailedAttempts {
			until := time.Now().Add(lockDuration)
			account.RecoveryLockedUntil = &until
		}
		_ = s.repo.SaveRecoveryState(ctx, account)
		return "", ErrRecoveryInvalid
	}
	if account.RecoveryAttempts != 0 || account.RecoveryLockedUntil != nil {
		account.RecoveryAttempts = 0
		account.RecoveryLockedUntil = nil
		_ = s.repo.SaveRecoveryState(ctx, account)
	}

	ticket := randomToken()
	s.mu.Lock()
	s.cleanupTickets()
	s.tickets[ticket] = resetTicket{accountID: account.ID, expiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	return ticket, nil
}

// ForgotReset 第二步：凭重置凭证 + 恢复密钥设置新密码。库密钥经恢复层解包后
// 用新密码重新包裹——忘记主密码不丢任何数据;成功后该账户全部会话失效
func (s *AuthService) ForgotReset(ctx context.Context, ticket, recoveryKey, newPassword string) error {
	recoveryKey = normalizeRecoveryInput(recoveryKey)
	s.mu.Lock()
	t, ok := s.tickets[ticket]
	if ok {
		delete(s.tickets, ticket) // 一次性使用
	}
	s.mu.Unlock()
	if !ok || t.expiresAt.Before(time.Now()) {
		return ErrResetTicketInvalid
	}

	account, err := s.repo.GetByID(ctx, t.accountID)
	if err != nil {
		return fmt.Errorf("查询账户: %w", err)
	}
	if len(account.RecoveryCipher) == 0 {
		return ErrRecoveryUnmigrated
	}
	vaultKey, err := vault.Open(vault.Derive(recoveryKey, account.VaultSalt), account.RecoveryCipher)
	if err != nil {
		return ErrRecoveryInvalid
	}
	vaultCipher, err := vault.Seal(vault.Derive(newPassword, account.VaultSalt), vaultKey)
	if err != nil {
		return fmt.Errorf("重包库密钥: %w", err)
	}
	authSalt := vault.RandomSalt()
	if err := s.repo.UpdateVault(ctx, account.ID, repository.VaultUpdate{
		AuthSalt:    authSalt,
		AuthHash:    vault.Derive(newPassword, authSalt),
		VaultCipher: vaultCipher,
	}); err != nil {
		return fmt.Errorf("更新账户: %w", err)
	}
	if err := s.repo.DeleteSessionsByAccount(ctx, account.ID); err != nil {
		return fmt.Errorf("清理会话: %w", err)
	}
	return nil
}

// normalizeRecoveryInput 恢复密钥输入容错清洗：去掉空白与横杠后，若剩余内容
// 恰为 32 位大写字母/数字（即密钥本体），重组成标准的 8×4 带横杠格式——
func normalizeRecoveryInput(s string) string {
	s = strings.TrimSpace(s)
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '-', '—', '–':
			return -1
		}
		return r
	}, s)
	if len(cleaned) == 32 {
		valid := true
		for _, c := range cleaned {
			if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
				valid = false
				break
			}
		}
		if valid {
			var b strings.Builder
			for i := 0; i < len(cleaned); i++ {
				if i > 0 && i%4 == 0 {
					b.WriteByte('-')
				}
				b.WriteByte(cleaned[i])
			}
			return b.String()
		}
	}
	return s
}

func (s *AuthService) cleanupTickets() {
	now := time.Now()
	for k, t := range s.tickets {
		if t.expiresAt.Before(now) {
			delete(s.tickets, k)
		}
	}
}

// newRecoveryKey 生成 8 组 4 字符的恢复密钥（XXXX-XXXX-…，A-Z0-9）
func newRecoveryKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去掉易混淆的 I O 0 1
	groups := make([]string, 8)
	for g := range groups {
		b := make([]byte, 4)
		for i := range b {
			b[i] = alphabet[int(raw[g*4+i])%len(alphabet)]
		}
		groups[g] = string(b)
	}
	return strings.Join(groups, "-"), nil
}

// hashToken 会话令牌的 SHA-256 十六进制哈希（库中存储形态）。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// hashRecovery 恢复密钥的 SHA-256 十六进制哈希。
func hashRecovery(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// lockBackoff 指数锁定时长：5 次→60 秒、10 次→5 分钟、15 次以上→30 分钟。
func lockBackoff(attempts int) time.Duration {
	switch {
	case attempts >= 15:
		return 30 * time.Minute
	case attempts >= 10:
		return 5 * time.Minute
	default:
		return lockDuration // 60 秒
	}
}

// recordFailure 失败计数并按档位指数锁定（正常用户输对即清零，无感知）
func (s *AuthService) recordFailure(ctx context.Context, account *model.Account) {
	account.FailedAttempts++
	if account.FailedAttempts >= captchaAfter {
		until := time.Now().Add(lockBackoff(account.FailedAttempts))
		account.LockedUntil = &until
	}
	_ = s.repo.SaveLoginState(ctx, account)
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(fmt.Sprintf("生成随机令牌失败: %v", err))
	}
	return hex.EncodeToString(b)
}
