// 仓库密码材料（salt/dk 零知识比对）、凭据条目的增量下发与幂等推送。
//   - 明文元数据（名称/协议/认证方式/分组/备注）落 ky 既有明文列，Web 端可直接展示编辑；
//   - 密文字段（TermX 端到端 cipher+nonce）以 JSON 包装存 Secret 列,本服务零知识;
//     推回时反查主键回填幂等键（防重复建行）。
package service

// FIXME: TermX翻译这块临时方案先用着
import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ky/internal/model"
	"ky/internal/pkg/termxcrypt"
	"ky/internal/pkg/termxtoken"
	"ky/internal/pkg/vault"
	"ky/internal/repository"
)

// TermX 同步模块哨兵错误：handler 层据此映射 HTTP 状态（文案直接下发给客户端展示）。
var (
	ErrTermxVaultNotSet  = errors.New("尚未设置仓库密码,请先设置")
	ErrTermxVaultAlready = errors.New("仓库密码已设置,无需重复设置")
	ErrTermxVaultWrong   = errors.New("仓库密码错误")
	ErrTermxBadMaterial  = errors.New("salt/dk 非法")
	ErrTermxBadSince     = errors.New("since 时间格式错误")
)

var (
	reTermxClientID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	reTermxProtocol = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,15}$`)
	reTermxAuth     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,23}$`)
)

// TermxEntryOut 条目下发线格式（字段名与客户端 ServerEntry 严格对应）。
type TermxEntryOut struct {
	ClientID   string  `json:"clientId"`
	Label      string  `json:"label"`
	Protocol   string  `json:"protocol"`
	AuthMethod string  `json:"authMethod"`
	Group      string  `json:"group"`
	Note       string  `json:"note"`
	Cipher     string  `json:"cipher"`
	Nonce      string  `json:"nonce"`
	UpdatedAt  string  `json:"updatedAt"`
	DeletedAt  *string `json:"deletedAt"`
}

type TermxEntryIn struct {
	ClientID      string `json:"clientId"`
	Label         string `json:"label"`
	Protocol      string `json:"protocol"`
	AuthMethod    string `json:"authMethod"`
	Group         string `json:"group"`
	Note          string `json:"note"`
	Cipher        string `json:"cipher"`
	Nonce         string `json:"nonce"`
	Deleted       bool   `json:"deleted"`
	LastConnected string `json:"lastConnected"`
}

// TermxAck 单条推送结果（accepted=false 时携带原因）。
type TermxAck struct {
	ClientID string `json:"clientId"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

// 深融合：条目同时维护两种密文——TermxSecret（TermX 线格式,客户端密钥加密）
// 与 Secret（ky 原生格式，库密钥加密）。服务端持有两把密钥材料，写入时双向
// 翻译：TermX 推送 → 翻译成原生密文（Web 端保持"输数据密钥→全字段编辑"的
type TermxService struct {
	repo      *repository.TermxRepository
	auth      *AuthService
	serverKey string // 启动时解析出的接入密钥（客户端数据密钥的派生口令）
}

func NewTermxService(repo *repository.TermxRepository, auth *AuthService, serverKey string) *TermxService {
	return &TermxService{repo: repo, auth: auth, serverKey: serverKey}
}

func (s *TermxService) AccountID(ctx context.Context) (uint, error) {
	return s.repo.FirstAccountID(ctx)
}

// HasVault 工作区是否已设置仓库密码（verify 响应,客户端据此分叉设置/解锁）
func (s *TermxService) HasVault(accountID uint) bool {
	st, err := s.repo.StateOf(accountID)
	return err == nil && st != nil && st.VaultDk != ""
}

func (s *TermxService) VaultParams(accountID uint) (salt string, gen int, err error) {
	st, err := s.repo.StateOf(accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", 0, ErrTermxVaultNotSet
		}
		return "", 0, err
	}
	return st.VaultSalt, st.VaultGen, nil
}

// 并生成令牌 HMAC 密钥；仓库代数/令牌代数各 +1（旧令牌即刻失效）。
func (s *TermxService) VaultSetup(ctx context.Context, accountID uint, salt, dk string) (string, error) {
	if !validHex(salt, 32) || !validHex(dk, 64) {
		return "", ErrTermxBadMaterial
	}
	st, err := s.repo.StateOf(accountID)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return "", err
		}
		st = &model.TermxState{AccountID: accountID}
	} else if st.VaultDk != "" {
		return "", ErrTermxVaultAlready
	}
	secret, err := randomHex(32)
	if err != nil {
		return "", err
	}
	st.VaultSalt, st.VaultDk = salt, dk
	st.VaultGen++
	st.VaultEpoch++
	st.TokenSecret = secret
	if err := s.repo.SaveState(ctx, st); err != nil {
		return "", err
	}
	return termxtoken.Sign(st.TokenSecret, accountID, st.VaultEpoch), nil
}

// VaultUnlock 解锁：常数时间比对派生键，通过签发 1h 仓库令牌
func (s *TermxService) VaultUnlock(accountID uint, dk string) (string, error) {
	st, err := s.repo.StateOf(accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", ErrTermxVaultNotSet
		}
		return "", err
	}
	if subtle.ConstantTimeCompare([]byte(st.VaultDk), []byte(dk)) != 1 {
		return "", ErrTermxVaultWrong
	}
	return termxtoken.Sign(st.TokenSecret, accountID, st.VaultEpoch), nil
}

// 时 cipher 置空——客户端按"仅元数据"骨架处理。
func (s *TermxService) ListEntries(ctx context.Context, accountID uint, since string) ([]TermxEntryOut, []string, string, error) {
	var sinceT *time.Time
	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return nil, nil, "", ErrTermxBadSince
		}
		utc := t.UTC()
		sinceT = &utc
	}
	serverTime := time.Now().UTC()
	rows, err := s.repo.CredListSince(ctx, accountID, sinceT)
	if err != nil {
		return nil, nil, "", fmt.Errorf("查询条目: %w", err)
	}
	groups, err := s.repo.CredDistinctGroups(ctx, accountID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("查询分组: %w", err)
	}
	entries := make([]TermxEntryOut, 0, len(rows))
	for i := range rows {
		entries = append(entries, termxRowToEntry(&rows[i]))
	}
	return entries, groups, serverTime.Format(time.RFC3339Nano), nil
}

// PushEntries 逐条校验并幂等落库；单条失败不拖垮整批。
func (s *TermxService) PushEntries(ctx context.Context, accountID uint, entries []TermxEntryIn) ([]TermxAck, string, error) {
	acks := make([]TermxAck, 0, len(entries))
	now := time.Now().UTC()
	for _, in := range entries {
		ack := TermxAck{ClientID: in.ClientID, Accepted: true}
		if err := s.pushOne(ctx, accountID, in, now); err != nil {
			ack.Accepted = false
			ack.Reason = err.Error()
		}
		acks = append(acks, ack)
	}
	return acks, now.Format(time.RFC3339Nano), nil
}

func (s *TermxService) pushOne(ctx context.Context, accountID uint, in TermxEntryIn, now time.Time) error {
	if !reTermxClientID.MatchString(in.ClientID) {
		return errors.New("clientId 非法")
	}
	label := strings.TrimSpace(in.Label)
	if len(label) < 1 || len(label) > 64 {
		return errors.New("label 非法")
	}
	if !reTermxProtocol.MatchString(in.Protocol) {
		return errors.New("protocol 非法")
	}
	if !reTermxAuth.MatchString(in.AuthMethod) {
		return errors.New("authMethod 非法")
	}
	if len(in.Group) > 64 {
		return errors.New("group 非法")
	}
	if len(in.Note) > 120 {
		return errors.New("note 非法")
	}

	existing, err := s.repo.CredFindByClientID(ctx, accountID, in.ClientID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	if existing == nil {
		// "ky-N" 反查：Web 端建的行（client_id NULL，列表以 ky-ID 下发）首次被
		if id, ok := parseKyRefID(in.ClientID); ok {
			if row, err2 := s.repo.CredGetByID(ctx, accountID, id); err2 == nil {
				existing = row
				cid := in.ClientID
				existing.ClientID = &cid
			} else if !errors.Is(err2, repository.ErrNotFound) {
				return err2
			}
		}
	}

	//空密文墓碑：Web 端建的条目（本服务无其 TermX 密文）被客户端"从云端删除"
	if in.Cipher == "" || in.Nonce == "" {
		if in.Deleted && existing != nil {
			existing.DeletedAt = &now
			existing.UpdatedAt = now
			if err := s.repo.CredSave(ctx, existing); err != nil {
				return fmt.Errorf("保存失败: %w", err)
			}
			return nil
		}
		return errors.New("cipher/nonce 不是合法 base64")
	}
	cipherRaw, err := base64.StdEncoding.DecodeString(in.Cipher)
	if err != nil || base64.StdEncoding.EncodeToString(cipherRaw) != in.Cipher {
		return errors.New("cipher/nonce 不是合法 base64")
	}
	nonceRaw, err := base64.StdEncoding.DecodeString(in.Nonce)
	if err != nil || base64.StdEncoding.EncodeToString(nonceRaw) != in.Nonce {
		return errors.New("cipher/nonce 不是合法 base64")
	}
	if len(nonceRaw) != 12 || len(cipherRaw) < 16 || len(cipherRaw) > 8192 {
		return errors.New("cipher/nonce 长度非法")
	}

	var lastUsed *time.Time
	if t, err := time.Parse(time.RFC3339, in.LastConnected); err == nil {
		utc := t.UTC()
		lastUsed = &utc
	}
	var delAt *time.Time
	if in.Deleted {
		delAt = &now
	}

	wrapJSON := string(mustJSON(termxcrypt.Wrap{V: 1, C: in.Cipher, N: in.Nonce}))

	if existing == nil {
		cid := in.ClientID
		row := &model.Credential{
			AccountID:   accountID,
			ClientID:    &cid,
			Name:        label,
			Protocol:    in.Protocol,
			AuthType:    in.AuthMethod,
			Group:       in.Group,
			TermxSecret: &wrapJSON,
			Remark:      in.Note,
			LastUsedAt:  lastUsed,
			DeletedAt:   delAt,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		// 同步翻译为 ky 原生密文（Web 端原生编辑体验）；库密钥未就绪则留空，
		// 待 Web 登录后懒翻译补齐
		if err := s.translateToNative(accountID, row); err != nil {
			row.Secret = nil
		}
		if err := s.repo.CredCreate(ctx, row); err != nil {
			return fmt.Errorf("写入失败: %w", err)
		}
		return nil
	}
	existing.Name = label
	existing.Protocol = in.Protocol
	existing.AuthType = in.AuthMethod
	existing.Group = in.Group
	existing.TermxSecret = &wrapJSON
	existing.Remark = in.Note
	existing.LastUsedAt = lastUsed
	existing.DeletedAt = delAt // true=盖墓碑；false=清墓碑（复活）
	existing.UpdatedAt = now   // 服务端权威盖章（最后写入胜出）
	if err := s.translateToNative(accountID, existing); err != nil {
		existing.Secret = nil // 宁缺勿旧：库密钥未就绪时清空，登录后懒翻译
	}
	if err := s.repo.CredSave(ctx, existing); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	return nil
}

func termxRowToEntry(c *model.Credential) TermxEntryOut {
	out := TermxEntryOut{
		ClientID:   termxClientIDOf(c),
		Label:      c.Name,
		Protocol:   c.Protocol,
		AuthMethod: c.AuthType,
		Group:      c.Group,
		Note:       c.Remark,
		UpdatedAt:  c.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if c.DeletedAt != nil {
		s := c.DeletedAt.UTC().Format(time.RFC3339Nano)
		out.DeletedAt = &s
	}
	if w, ok := termxWrapOf(c); ok {
		out.Cipher, out.Nonce = w.C, w.N
	}
	return out
}

func termxClientIDOf(c *model.Credential) string {
	if c.ClientID != nil && *c.ClientID != "" {
		return *c.ClientID
	}
	return "ky-" + strconv.FormatUint(uint64(c.ID), 10)
}

func termxWrapOf(c *model.Credential) (termxcrypt.Wrap, bool) {
	if c.TermxSecret == nil || *c.TermxSecret == "" {
		return termxcrypt.Wrap{}, false
	}
	var w termxcrypt.Wrap
	if err := json.Unmarshal([]byte(*c.TermxSecret), &w); err != nil || w.V != 1 {
		return termxcrypt.Wrap{}, false
	}
	return w, true
}

// termxDataKey 派生该账户的 TermX 客户端数据密钥（服务器密钥 + 仓库盐）
func (s *TermxService) termxDataKey(accountID uint) ([]byte, error) {
	st, err := s.repo.StateOf(accountID)
	if err != nil || st.VaultSalt == "" {
		return nil, errors.New("TermX 仓库未初始化")
	}
	return termxcrypt.VaultKey(s.serverKey, st.VaultSalt)
}

// 需要内存库密钥（Web 登录后存在）；缺失返回 ErrVaultGone，调用方置空等待懒翻译
func (s *TermxService) translateToNative(accountID uint, c *model.Credential) error {
	w, ok := termxWrapOf(c)
	if !ok {
		return nil // 无 TermX 视图（纯 Web 条目）：不翻译
	}
	tk, err := s.termxDataKey(accountID)
	if err != nil {
		return err
	}
	plain, err := termxcrypt.Open(tk, w)
	if err != nil {
		return err
	}
	var payload struct {
		Session map[string]any `json:"session"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		return err
	}
	sec, err := sessionToCredSecret(payload.Session)
	if err != nil {
		return err
	}
	vk, err := s.auth.VaultKey(accountID)
	if err != nil {
		return err // ErrVaultGone：Web 未登录过
	}
	blob, err := vault.Seal(vk, mustJSON(sec))
	if err != nil {
		return err
	}
	c.Secret = blob
	return nil
}

// 已有 TermX 视图 → 解开并把新密文字段覆盖回去（保留 TermX 专属字段）;
//
//	没有视图（Web 新建/存量行）→ 由行元数据+连接信息构造全新会话对象；
func (s *TermxService) SyncNativeToTermx(accountID uint, c *model.Credential) {
	if len(c.Secret) == 0 {
		return
	}
	oldWrap, hadView := termxWrapOf(c)
	vk, err := s.auth.VaultKey(accountID)
	if err != nil {
		if hadView {
			c.TermxSecret = nil
		}
		return // 未登录：新建行留待登录后懒回填
	}
	raw, err := vault.Open(vk, c.Secret)
	if err != nil {
		c.TermxSecret = nil
		return
	}
	var sec CredSecret
	if err := json.Unmarshal(raw, &sec); err != nil {
		c.TermxSecret = nil
		return
	}
	tk, err := s.termxDataKey(accountID)
	if err != nil {
		return // TermX 同步未初始化（该账户从未用过）：无需视图
	}
	var sess map[string]any
	if hadView {
		plain, err := termxcrypt.Open(tk, oldWrap)
		if err != nil {
			c.TermxSecret = nil
			return
		}
		var payload struct {
			Session map[string]any `json:"session"`
		}
		if err := json.Unmarshal(plain, &payload); err != nil || payload.Session == nil {
			c.TermxSecret = nil
			return
		}
		sess = payload.Session
	} else {
		// 全新构造：以行的下发标识 "ky-ID" 为会话 id——客户端拉取/推回时
		// 与幂等反查天然对齐；元数据取行明文列，密文字段由 CredSecret 覆盖
		sess = map[string]any{
			"id":       termxClientIDOf(c),
			"name":     c.Name,
			"group":    c.Group,
			"protocol": c.Protocol,
			"auth":     c.AuthType,
			"note":     c.Remark,
			"status":   "disconnected",
		}
	}
	applyCredSecret(sess, sec)
	nw, err := termxcrypt.Seal(tk, map[string]any{"v": 1, "session": sess})
	if err != nil {
		c.TermxSecret = nil
		return
	}
	nj := string(mustJSON(nw))
	c.TermxSecret = &nj
}

// LazyTranslateAll 库密钥就绪（Web 登录）后的双向补齐：
//
//	TermX 推送过但原生缺失 → 正向翻译（TermX→原生，Web 端可编辑）；
func (s *TermxService) LazyTranslateAll(ctx context.Context, accountID uint) {
	rows, err := s.repo.CredListSince(ctx, accountID, nil)
	if err != nil {
		log.Printf("[termx] 懒翻译查询失败: %v", err)
		return
	}
	n := 0
	for i := range rows {
		row := &rows[i]
		changed := false
		if row.TermxSecret != nil {
			if err := s.translateToNative(accountID, row); err != nil {
				continue
			}
			changed = true
		} else if len(row.Secret) > 0 {
			s.SyncNativeToTermx(accountID, row)
			changed = row.TermxSecret != nil
		}
		if !changed {
			continue
		}
		if err := s.repo.CredSave(ctx, row); err != nil {
			log.Printf("[termx] 懒翻译保存失败 id=%d: %v", row.ID, err)
			continue
		}
		n++
	}
	if n > 0 {
		log.Printf("[termx] 懒翻译完成：补齐 %d 条（双格式对齐）", n)
	}
}

// sessionToCredSecret TermX 会话对象（密文字段部分）→ ky 原生连接信息
func sessionToCredSecret(sess map[string]any) (CredSecret, error) {
	var sec CredSecret
	if sess == nil {
		return sec, errors.New("会话载荷为空")
	}
	sec.Host, _ = sess["host"].(string)
	if v, ok := sess["port"].(float64); ok && v >= 1 && v <= 65535 {
		p := int(v)
		sec.Port = &p
	}
	sec.Username, _ = sess["user"].(string)
	sec.Password, _ = sess["password"].(string)
	sec.KeyContent, _ = sess["keyContent"].(string)
	if p, ok := sess["proxy"].(map[string]any); ok {
		sec.Proxy.Host, _ = p["host"].(string)
		sec.Proxy.Username, _ = p["username"].(string)
		sec.Proxy.Password, _ = p["password"].(string)
	}
	if sr, ok := sess["serialBaud"].(float64); ok {
		sec.Serial = &SerialConf{BaudRate: int(sr)}
		if v, ok := sess["serialDataBits"].(float64); ok {
			sec.Serial.DataBits = int(v)
		}
		if v, ok := sess["serialStopBits"].(float64); ok {
			sec.Serial.StopBits = int(v)
		}
	}
	return sec, nil
}

func applyCredSecret(sess map[string]any, sec CredSecret) {
	sess["host"] = sec.Host
	if sec.Port != nil {
		sess["port"] = *sec.Port
	} else {
		delete(sess, "port")
	}
	sess["user"] = sec.Username
	sess["password"] = sec.Password
	sess["keyContent"] = sec.KeyContent
	sess["proxy"] = map[string]any{
		"host":     sec.Proxy.Host,
		"username": sec.Proxy.Username,
		"password": sec.Proxy.Password,
	}
	if sec.Serial != nil {
		sess["serialBaud"] = sec.Serial.BaudRate
		sess["serialDataBits"] = sec.Serial.DataBits
		sess["serialStopBits"] = sec.Serial.StopBits
	}
}

func parseKyRefID(cid string) (uint, bool) {
	if !strings.HasPrefix(cid, "ky-") {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(cid, "ky-"), 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}

func validHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
