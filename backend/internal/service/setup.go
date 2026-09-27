package service

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"gorm.io/gorm"

	"ky/internal/config"
	"ky/internal/database"
	"ky/internal/pkg/console"
	"ky/internal/pkg/restart"
	"ky/internal/pkg/vault"
	"ky/internal/repository"
)

var (
	ErrSetupDone  = errors.New("系统已完成初始化，无需重复安装")
	ErrSetupParam = errors.New("安装参数不合法")
	ErrSetupPort  = errors.New("监听地址不可用")
	ErrSetupCert  = errors.New("证书配置不可用")
)

var (
	reSetupKey   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	reSetupTermx = regexp.MustCompile(`^/[A-Za-z0-9_/-]{1,64}$`)
	setupModels  = migrateModels // 与切库迁移保持同一份表清单
)

// ServerInput 服务配置（监听地址/端口/HTTPS 证书）。
type ServerInput struct {
	Host     string
	Port     int
	HTTPS    bool
	CertFile string
	KeyFile  string
}

type SetupInput struct {
	Database  DbConnInput
	Server    ServerInput
	TermxPath string
	Username  string
	Password  string
	MasterKey string // 43 位 Base64URL；解码为 32 字节库密钥，仅以包裹形态落库
}

type SetupResult struct {
	RecoveryKey string   `json:"recovery_key"` // 一次性恢复密钥明文
	TermxKey    string   `json:"termx_key"`    // TermX 接入密钥（完成页展示，客户端配对用）
	AccessURL   string   `json:"access_url"`   // 重启后的访问地址
	Phases      []string `json:"phases"`       // 已完成的阶段（前端清单逐项点亮）
}

// SetupService 安装引导业务逻辑。
type SetupService struct {
	run      *config.Config // 进程启动配置（端口探测豁免、横幅用）
	authRepo *repository.AuthRepository
	dbCfg    *DbConfigService // 复用连接测试 / DSN 组装 / 同名库检测
	termxKey string           // TermX 接入密钥（完成响应一并下发，完成页展示）
	mu       sync.Mutex       // complete 互斥（防并发抢注）
}

// NewSetupService 构造服务（termxKey 为启动时解析出的接入密钥）
func NewSetupService(run *config.Config, db *gorm.DB, dbCfg *DbConfigService, termxKey string) *SetupService {
	return &SetupService{
		run:      run,
		authRepo: repository.NewAuthRepository(db),
		dbCfg:    dbCfg,
		termxKey: termxKey,
	}
}

func (s *SetupService) Status(ctx context.Context) (bool, error) {
	n, err := s.authRepo.CountAccounts(ctx)
	if err != nil {
		return false, fmt.Errorf("统计账户: %w", err)
	}
	return n > 0, nil
}

func (s *SetupService) guardDone(ctx context.Context) error {
	done, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if done {
		return ErrSetupDone
	}
	return nil
}

// 与账号可登录，并尽力探测建库权限——权限不足给出预警，由 complete 兜底拦截）
type TestConnInfo struct {
	OK            bool   `json:"ok"`
	Message       string `json:"message"`
	CanCreateDB   bool   `json:"can_create_db"`
	PrivilegeHint string `json:"privilege_hint,omitempty"`
}

// TestConnection 数据库连通测试。SQLite 沿用切库测试逻辑（本地文件即库）；
// 外部引擎连服务器管理库验证（不带业务库名——全新环境库还不存在），
func (s *SetupService) TestConnection(ctx context.Context, in DbConnInput) (*TestConnInfo, error) {
	if err := s.guardDone(ctx); err != nil {
		return nil, err
	}
	if in.Engine == "sqlite" {
		if err := s.dbCfg.Test(in); err != nil {
			return nil, err
		}
		return &TestConnInfo{OK: true, Message: "连接成功", CanCreateDB: true}, nil
	}
	if err := database.TestAdmin(in.Engine, in.Host, in.Port, in.User, in.Password, dbTestTimeout); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDbConnect, err)
	}
	info := &TestConnInfo{OK: true, Message: "服务器连接成功", CanCreateDB: true}
	if ok, hint := database.CanCreateDatabase(in.Engine, in.Host, in.Port, in.User, in.Password); !ok {
		info.CanCreateDB = false
		info.PrivilegeHint = fmt.Sprintf("%s（所需权限：%s）", hint, requiredPrivileges[in.Engine])
		info.Message = "服务器连接成功，但账号可能缺少建库权限"
	}
	return info, nil
}

// 无权限），HTTPS 开启时校验证书文件对可加载。目标端口与当前运行端口相同时
func (s *SetupService) TestServer(ctx context.Context, in ServerInput) error {
	if err := s.guardDone(ctx); err != nil {
		return err
	}
	if in.Host == "" || in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("%w: 监听地址或端口不合法", ErrSetupParam)
	}
	if in.HTTPS {
		if in.CertFile == "" || in.KeyFile == "" {
			return fmt.Errorf("%w: 启用 HTTPS 需填写证书与私钥文件路径", ErrSetupParam)
		}
		if _, err := tls.LoadX509KeyPair(in.CertFile, in.KeyFile); err != nil {
			return fmt.Errorf("%w: %v", ErrSetupCert, err)
		}
	}
	if in.Port == s.run.Server.Port {
		return nil // 当前服务端口：重启时释放并接管
	}
	addr := fmt.Sprintf("%s:%d", in.Host, in.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		switch {
		case errors.Is(err, syscall.EADDRINUSE):
			return fmt.Errorf("%w: 端口 %d 已被其他程序占用", ErrSetupPort, in.Port)
		case errors.Is(err, syscall.EADDRNOTAVAIL):
			return fmt.Errorf("%w: 本机不存在地址 %s", ErrSetupPort, in.Host)
		case errors.Is(err, syscall.EACCES):
			return fmt.Errorf("%w: 无权限绑定 %s（1024 以下端口需要管理员权限）", ErrSetupPort, addr)
		default:
			return fmt.Errorf("%w: %v", ErrSetupPort, err)
		}
	}
	_ = ln.Close()
	return nil
}

// 创建账户（库密钥以主密码包裹落库）→ 备份并写配置 → 自重启
func (s *SetupService) Complete(ctx context.Context, in SetupInput) (*SetupResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.guardDone(ctx); err != nil {
		return nil, err
	}

	//主密钥：用户显式提供则校验解码；留空则系统随机生成（日常由系统自动管理）
	var vaultKey []byte
	if in.MasterKey != "" {
		var derr error
		vaultKey, derr = decodeSetupKey(in.MasterKey)
		if derr != nil {
			return nil, derr
		}
	} else {
		vaultKey = vault.RandomKey()
	}
	if l := len(in.Username); l < 2 || l > 64 {
		return nil, fmt.Errorf("%w: 账户名长度需 2-64", ErrSetupParam)
	}
	if l := len(in.Password); l < 8 || l > 128 {
		return nil, fmt.Errorf("%w: 主密码至少 8 位", ErrSetupParam)
	}
	srv := in.Server
	if srv.Host == "" || srv.Port < 1 || srv.Port > 65535 {
		return nil, fmt.Errorf("%w: 监听地址或端口不合法", ErrSetupParam)
	}
	if srv.HTTPS {
		if _, err := tls.LoadX509KeyPair(srv.CertFile, srv.KeyFile); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSetupCert, err)
		}
	}
	termxPath, err := normalizeTermxPath(in.TermxPath)
	if err != nil {
		return nil, err
	}

	_ = os.MkdirAll("data", 0o755) // 数据目录（日志/密钥文件/默认 sqlite 均在此）
	if in.Database.Engine == "sqlite" && in.Database.File == "" {
		in.Database.File = "data/app.db"
	}
	if err := s.dbCfg.resolveTargetName(&in.Database); err != nil {
		return nil, err
	}
	dsn, err := s.dbCfg.buildDSN(in.Database, false)
	if err != nil {
		return nil, err
	}
	if in.Database.Engine != "sqlite" {
		if err := database.EnsureDatabase(
			in.Database.Engine, in.Database.Host, in.Database.Port,
			in.Database.User, in.Database.Password, in.Database.Name,
		); err != nil {
			return nil, fmt.Errorf("%w: %w（所需权限：%s）",
				ErrDbConnect, err, requiredPrivileges[in.Database.Engine])
		}
	}
	phases := []string{"数据库已就绪"}
	console.Phase("数据库已就绪（" + in.Database.Engine + "）")

	newDB, err := database.Open(in.Database.Engine, dsn)
	if err != nil {
		s.cleanupFreshSqlite(in.Database)
		return nil, fmt.Errorf("%w: %w", ErrDbConnect, err)
	}
	if err := newDB.AutoMigrate(setupModels...); err != nil {
		s.closeDB(newDB)
		s.cleanupFreshSqlite(in.Database)
		return nil, fmt.Errorf("%w: 建表失败: %w（所需权限：%s）",
			ErrDbConnect, err, requiredPrivileges[in.Database.Engine])
	}
	recovery, err := CreateVaultAccount(ctx, repository.NewAuthRepository(newDB), in.Username, in.Password, vaultKey)
	if err != nil {
		s.closeDB(newDB)
		s.cleanupFreshSqlite(in.Database)
		return nil, fmt.Errorf("%w: %w", ErrSetupParam, err)
	}
	s.closeDB(newDB)
	phases = append(phases, "数据表已创建", "账户已创建")
	console.Phase("数据表已创建")
	console.Phase("账户已创建（" + in.Username + "）")

	if data, err := os.ReadFile(config.File); err == nil {
		if err := os.WriteFile(config.BackupFile, data, 0o600); err != nil {
			return nil, fmt.Errorf("备份配置文件: %w", err)
		}
	}
	cfg, err := config.Read()
	if err != nil {
		s.restoreConfigBackup()
		return nil, fmt.Errorf("读取配置: %w", err)
	}
	cfg.Server.Host = srv.Host
	cfg.Server.Port = srv.Port
	cfg.Server.HTTPS = srv.HTTPS
	cfg.Server.CertFile = srv.CertFile
	cfg.Server.KeyFile = srv.KeyFile
	cfg.Server.TermxBasePath = termxPath
	cfg.Database = config.Database{Driver: in.Database.Engine, DSN: dsn}
	cfg.Security.SeedAdmin = false
	cfg.Security.TermxApiKey = ""
	cfg.SetupCompleted = true
	if err := cfg.Save(); err != nil {
		s.restoreConfigBackup()
		return nil, fmt.Errorf("写入配置文件: %w", err)
	}
	if in.Database.Engine == "sqlite" {
		_ = os.Chmod(in.Database.File, 0o600)
	}
	phases = append(phases, "配置已写入")
	console.Phase("配置已写入（data/config.yaml）")

	// ---- 阶段 4：转入后台并应用新配置（延迟给响应留出送达时间；冷却戳防反复触发） ----
	writeRestartStamp()
	if restart.IsDaemon() {
		restart.Self(restartDelay) // 已是后台：原地替换保持后台
	} else {
		restart.SelfDaemon(restartDelay) // 前台运行：转后台并让出终端
	}
	phases = append(phases, "服务重启中")
	console.Phase("服务转入后台并重启中…")

	log.Printf("安装引导完成：账户 %s 已创建，%s 模式，重启后生效（访问地址 %s）",
		in.Username, in.Database.Engine, displayAccessURL(srv))
	return &SetupResult{
		RecoveryKey: recovery,
		TermxKey:    s.termxKey,
		AccessURL:   displayAccessURL(srv),
		Phases:      phases,
	}, nil
}

func decodeSetupKey(s string) ([]byte, error) {
	if !reSetupKey.MatchString(s) {
		return nil, fmt.Errorf("%w: 主密钥须为 43 位 Base64URL 字符", ErrSetupParam)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("%w: 主密钥解码失败", ErrSetupParam)
	}
	return raw, nil
}

// normalizeTermxPath 归一化 TermX 接口挂载前缀：空值回落默认 /api/client;
// 去掉末尾斜杠；白名单校验并禁止与 /api/v1 业务面前缀冲突
func normalizeTermxPath(p string) (string, error) {
	if p == "" {
		return "/api", nil
	}
	p = strings.TrimRight(p, "/")
	if strings.HasSuffix(p, "/client") { // 旧语义（完整前缀 /api/client）兼容
		p = strings.TrimSuffix(p, "/client")
	}
	if !reSetupTermx.MatchString(p) || strings.Contains(p, "//") {
		return "", fmt.Errorf("%w: 同步接口路径须以 / 开头，段内仅限字母/数字/-/_（1-64 位）", ErrSetupParam)
	}
	if p == "/api/v1" || strings.HasPrefix(p, "/api/v1/") {
		return "", fmt.Errorf("%w: 同步接口路径不能占用 /api/v1 业务前缀", ErrSetupParam)
	}
	return p, nil
}

// displayAccessURL 展示用访问地址（0.0.0.0 按本机回环展示）
func displayAccessURL(srv ServerInput) string {
	host := srv.Host
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}
	scheme := "http"
	if srv.HTTPS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, srv.Port)
}

func (s *SetupService) cleanupFreshSqlite(in DbConnInput) {
	if in.Engine == "sqlite" && in.File != "" {
		_ = os.Remove(in.File)
	}
}

func (s *SetupService) closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func (s *SetupService) restoreConfigBackup() {
	if data, err := os.ReadFile(config.BackupFile); err == nil {
		_ = os.WriteFile(config.File, data, 0o600)
	}
}
