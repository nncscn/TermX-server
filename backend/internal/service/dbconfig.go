package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"ky/internal/config"
	"ky/internal/database"
	"ky/internal/model"
	"ky/internal/pkg/portprobe"
	"ky/internal/pkg/restart"
)

var (
	ErrDbConnect       = errors.New("数据库连接失败")
	ErrDbCfgInvalid    = errors.New("请填写主机地址与数据库名")
	ErrDbSameTarget    = errors.New("与当前使用的库相同，无需切换")
	ErrRestartCooldown = errors.New("重启冷却中，请稍后再试")
)

// 切库重启冷却窗口与时间戳文件（跨重启存续,防反复触发自重启 DoS）。
const (
	restartCooldown = 30 * time.Second
	lastRestartFile = "data/last_restart"
)

type DbNameConflict struct {
	Requested string
	Suggested string
}

func (e *DbNameConflict) Error() string { return "目标库已存在，已自动避开同名库" }

const dbTestTimeout = 5 * time.Second

const restartDelay = 900 * time.Millisecond

var defaultDbPorts = map[string]int{"mysql": 3306, "postgresql": 5432, "sqlserver": 1433}

var migrateModels = []any{
	&model.Account{}, &model.Session{},
	&model.SshKey{}, &model.Credential{}, &model.Preference{},
	&model.TermxState{},
}

// 各引擎所需权限说明：权限不足时随错误返回,前端切换前也会展示
var requiredPrivileges = map[string]string{
	"mysql":      "服务器级 CREATE（建库）+ 目标库的 CREATE/ALTER/INDEX/SELECT/INSERT/UPDATE/DELETE",
	"postgresql": "角色需 CREATEDB（建库）+ 目标库的 CREATE/ALTER/SELECT/INSERT/UPDATE/DELETE",
	"sqlserver":  "dbcreator 服务器角色（建库）+ 目标库 db_owner（建表与读写）",
	"sqlite":     "对数据目录的文件读写权限",
}

// RunningEngine 为进程实际使用的引擎（前端据此置灰"当前使用"项）；
// 两者不同时 PendingEngine 非空（已保存待重启）。密码不回显明文。
type DbConfig struct {
	Engine        string            `json:"engine"`
	RunningEngine string            `json:"running_engine"`
	PendingEngine string            `json:"pending_engine,omitempty"`
	File          string            `json:"file,omitempty"` // sqlite 数据文件路径
	Host          string            `json:"host,omitempty"`
	Port          int               `json:"port,omitempty"`
	User          string            `json:"user,omitempty"`
	Name          string            `json:"name,omitempty"`
	HasPassword   bool              `json:"has_password,omitempty"`
	Privileges    map[string]string `json:"privileges"`
}

// DbConnInput 数据库连接参数（测试/保存共用）。Migrate 表示切换时是否迁移现有数据
type DbConnInput struct {
	Engine   string
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	File     string
	Migrate  bool
}

// DbConfigService 数据库配置业务逻辑
type DbConfigService struct {
	run  config.Database // 进程启动时实际加载的数据库配置（运行态事实）
	srv  config.Server   // 运行态服务配置（端口检测用）
	db   *gorm.DB        // 当前库连接（迁移数据时作为源）
	auth *AuthService
}

func NewDbConfigService(run *config.Config, db *gorm.DB, auth *AuthService) *DbConfigService {
	return &DbConfigService{run: run.Database, srv: run.Server, db: db, auth: auth}
}

func (s *DbConfigService) Get() (*DbConfig, error) {
	cfg, err := config.Read()
	if err != nil {
		return nil, fmt.Errorf("读取配置: %w", err)
	}
	driver, dsn := cfg.Database.Driver, cfg.Database.DSN
	out := &DbConfig{
		Engine:        driver,
		RunningEngine: s.run.Driver,
		Privileges:    requiredPrivileges,
	}
	if driver != s.run.Driver {
		out.PendingEngine = driver
	}
	switch driver {
	case "sqlite":
		out.File = dsn
	default:
		out.Host, out.Port, out.User, out.Name, out.HasPassword = parseDSN(driver, dsn)
	}
	return out, nil
}

// TODO: sqlserver超时处理不太对
func (s *DbConfigService) Test(in DbConnInput) error {
	dsn, err := s.buildDSN(in, false)
	if err != nil {
		return err
	}
	if err := database.TestConnection(in.Engine, dsn, dbTestTimeout); err != nil {
		return fmt.Errorf("%w: %w", ErrDbConnect, err)
	}
	return nil
}

// Save 保存并切换：验主密码 → 同名库检测（存在即避开，必要时返回建议名征询用户）
// → 自动建库 → 真实连通测试 →（可选）结构与数据整体迁移 → 备份写回 config.yaml
func (s *DbConfigService) Save(ctx context.Context, accountID uint, in DbConnInput, masterPassword string) error {
	if _, err := s.auth.VerifyPassword(ctx, accountID, masterPassword); err != nil {
		return err
	}
	//验密之后校验冷却：防止反复触发自重启打断服务（DoS）
	if err := checkRestartCooldown(); err != nil {
		return err
	}
	if err := s.checkNotSameAsRunning(in); err != nil {
		return err
	}
	// 目标名解析：已存在的库视为他人数据,自动建议 _1/_2… 后缀名
	if err := s.resolveTargetName(&in); err != nil {
		return err
	}
	dsn, err := s.buildDSN(in, true)
	if err != nil {
		return err
	}
	if err := database.EnsureDatabase(in.Engine, in.Host, in.Port, in.User, in.Password, in.Name); err != nil {
		return fmt.Errorf("%w: %w（所需权限：%s）", ErrDbConnect, err, requiredPrivileges[in.Engine])
	}
	if err := database.TestConnection(in.Engine, dsn, dbTestTimeout); err != nil {
		return fmt.Errorf("%w: %w", ErrDbConnect, err)
	}
	if in.Migrate {
		if err := database.MigrateData(s.db, in.Engine, dsn, migrateModels...); err != nil {
			return fmt.Errorf("%w: 迁移数据: %w（所需权限：%s）", ErrDbConnect, err, requiredPrivileges[in.Engine])
		}
	}
	// sqlite 新库文件立即收紧权限（外部引擎无文件权限问题）
	if in.Engine == "sqlite" {
		_ = os.Chmod(in.File, 0o600)
	}

	// 备份现有配置（整文件拷贝，原文件先不动；0600 防 DSN 密码被同机用户读取）
	if data, err := os.ReadFile(config.File); err == nil {
		if err := os.WriteFile(config.BackupFile, data, 0o600); err != nil {
			return fmt.Errorf("备份配置文件: %w", err)
		}
	}

	cfg, err := config.Read()
	if err != nil {
		return fmt.Errorf("读取配置: %w", err)
	}
	cfg.Database.Driver = in.Engine
	cfg.Database.DSN = dsn
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("写入配置文件: %w", err)
	}
	writeRestartStamp()
	restart.Self(restartDelay)
	return nil
}

// checkNotSameAsRunning 目标与当前运行的库相同则拒绝（前端该选项已置灰,防御式兜底）。
func (s *DbConfigService) checkNotSameAsRunning(in DbConnInput) error {
	if in.Engine != s.run.Driver {
		return nil
	}
	if in.Engine == "sqlite" {
		if in.File == s.run.DSN {
			return ErrDbSameTarget
		}
		return nil
	}
	h, p, _, _, nm := parseDSNFull(s.run.Driver, s.run.DSN)
	if h == in.Host && p == in.Port && nm == in.Name {
		return ErrDbSameTarget
	}
	return nil
}

// resolveTargetName 就地解析最终目标名：不存在则原样使用;已存在则返回建议的
func (s *DbConfigService) resolveTargetName(in *DbConnInput) error {
	if in.Engine == "sqlite" {
		if _, err := os.Stat(in.File); err != nil {
			return nil // 不存在，直接用
		}
		//首次启动自动建的空库（无账户）视为可用目标；有账户数据才构成冲突
		if database.SqliteAccountsEmpty(in.File) {
			return nil
		}
		sug := suffixFile(in.File, func(n string) bool {
			_, err := os.Stat(n)
			return err != nil
		})
		return &DbNameConflict{Requested: in.File, Suggested: sug}
	}
	exists, err := database.DbExists(in.Engine, in.Host, in.Port, in.User, in.Password, in.Name)
	if err != nil {
		return fmt.Errorf("%w: 检查同名库: %w（所需权限：%s）", ErrDbConnect, err, requiredPrivileges[in.Engine])
	}
	if !exists {
		return nil
	}
	free := func(n string) bool {
		ok, err := database.DbExists(in.Engine, in.Host, in.Port, in.User, in.Password, n)
		return err == nil && !ok
	}
	for i := 1; i <= 99; i++ {
		sug := fmt.Sprintf("%s_%d", in.Name, i)
		if free(sug) {
			return &DbNameConflict{Requested: in.Name, Suggested: sug}
		}
	}
	return fmt.Errorf("%w: 同名库及后缀名均被占用", ErrDbCfgInvalid)
}

// suffixFile 为 sqlite 文件名找第一个不存在的 _N 后缀路径（保留扩展名）
func suffixFile(file string, free func(string) bool) string {
	ext := filepath.Ext(file)
	base := strings.TrimSuffix(file, ext)
	for i := 1; i <= 99; i++ {
		sug := fmt.Sprintf("%s_%d%s", base, i, ext)
		if free(sug) {
			return sug
		}
	}
	return file
}

func (s *DbConfigService) Revert(ctx context.Context, accountID uint, masterPassword string) error {
	if _, err := s.auth.VerifyPassword(ctx, accountID, masterPassword); err != nil {
		return err
	}
	cfg, err := config.Read()
	if err != nil {
		return fmt.Errorf("读取配置: %w", err)
	}
	if cfg.Database.Driver == s.run.Driver && cfg.Database.DSN == s.run.DSN {
		return nil // 无待生效变更
	}
	if data, err := os.ReadFile(config.File); err == nil {
		_ = os.WriteFile(config.BackupFile, data, 0o600)
	}
	cfg.Database = s.run
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("写入配置文件: %w", err)
	}
	return nil
}

// buildDSN 校验参数并组装 DSN。keepOldPass=true 时密码留空表示沿用当前配置中的密码
// 防止借切库之机在任意路径创建/覆写文件
func (s *DbConfigService) buildDSN(in DbConnInput, keepOldPass bool) (string, error) {
	if in.Engine != "sqlite" && in.Engine != "mysql" && in.Engine != "postgresql" && in.Engine != "sqlserver" {
		return "", ErrDbCfgInvalid
	}
	if in.Engine == "sqlite" {
		if in.File == "" {
			in.File = "data/app.db"
		}
		if err := validSqlitePath(in.File); err != nil {
			return "", err
		}
		return in.File, nil
	}
	if strings.TrimSpace(in.Host) == "" || strings.TrimSpace(in.Name) == "" {
		return "", ErrDbCfgInvalid
	}
	if in.Port <= 0 {
		in.Port = defaultDbPorts[in.Engine]
	}
	password := in.Password
	if keepOldPass && password == "" {
		cfg, err := config.Read()
		if err != nil {
			return "", fmt.Errorf("读取配置: %w", err)
		}
		if cfg.Database.Driver == in.Engine {
			_, _, _, password, _ = parseDSNFull(cfg.Database.Driver, cfg.Database.DSN)
		}
	}
	switch in.Engine {
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			in.User, password, in.Host, in.Port, in.Name), nil
	case "postgresql":
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Shanghai",
			in.Host, in.Port, in.User, password, in.Name), nil
	default: // sqlserver
		return fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			url.QueryEscape(in.User), url.QueryEscape(password), in.Host, in.Port, url.QueryEscape(in.Name)), nil
	}
}

// 文件已存在时校验真实路径（EvalSymlinks）防软链逃逸,拒绝绝对路径与 .. 穿越。
func validSqlitePath(file string) error {
	if !strings.HasSuffix(file, ".db") {
		return fmt.Errorf("%w: 数据文件必须以 .db 结尾", ErrDbCfgInvalid)
	}
	clean := filepath.Clean(file)
	if filepath.IsAbs(clean) {
		return fmt.Errorf("%w: 数据文件必须位于 data/ 目录内", ErrDbCfgInvalid)
	}
	if err := checkUnderData(clean); err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		return checkUnderData(real)
	}
	return nil
}

func checkUnderData(path string) error {
	rel, err := filepath.Rel("data", path)
	if err != nil || rel == ".." || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: 数据文件必须位于 data/ 目录内", ErrDbCfgInvalid)
	}
	return nil
}

// checkRestartCooldown 读取上次自重启时间戳，冷却窗口内拒绝再次切库
func checkRestartCooldown() error {
	data, err := os.ReadFile(lastRestartFile)
	if err != nil {
		return nil // 无记录视为不冷却
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return nil
	}
	if time.Since(time.Unix(0, n)) < restartCooldown {
		return ErrRestartCooldown
	}
	return nil
}

// writeRestartStamp 记录本次自重启时间（0600），供冷却判断跨重启使用
func writeRestartStamp() {
	_ = os.WriteFile(lastRestartFile, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o600)
}

func parseDSN(engine, dsn string) (host string, port int, user, name string, hasPass bool) {
	host, port, user, pass, dbname := parseDSNFull(engine, dsn)
	return host, port, user, dbname, pass != ""
}

func ParseDSN(engine, dsn string) (host string, port int, user, pass, name string) {
	return parseDSNFull(engine, dsn)
}

// parseDSNFull 把各引擎 DSN 解析回分字段（尽力解析，失败的字段留空）。
func parseDSNFull(engine, dsn string) (host string, port int, user, pass, name string) {
	switch engine {
	case "mysql":
		// user:pass@tcp(host:port)/name?params
		rest := dsn
		if i := strings.LastIndex(rest, "@tcp("); i >= 0 {
			userpart := rest[:i]
			if j := strings.Index(userpart, ":"); j >= 0 {
				user, pass = userpart[:j], userpart[j+1:]
			} else {
				user = userpart
			}
			rest = rest[i+len("@tcp("):]
			if k := strings.Index(rest, ")"); k >= 0 {
				if h, p, err := net.SplitHostPort(rest[:k]); err == nil {
					host = h
					port, _ = strconv.Atoi(p)
				}
				rest = rest[k+1:]
			}
		}
		rest = strings.TrimPrefix(rest, "/")
		if q := strings.Index(rest, "?"); q >= 0 {
			rest = rest[:q]
		}
		name = rest
	case "postgresql":
		m := map[string]string{}
		for _, kv := range strings.Fields(dsn) {
			if i := strings.Index(kv, "="); i > 0 {
				m[kv[:i]] = kv[i+1:]
			}
		}
		host = m["host"]
		port, _ = strconv.Atoi(m["port"])
		user = m["user"]
		pass = m["password"]
		name = m["dbname"]
	case "sqlserver":
		if u, err := url.Parse(dsn); err == nil && u.Host != "" {
			h, p, err := net.SplitHostPort(u.Host)
			if err == nil {
				host = h
				port, _ = strconv.Atoi(p)
			} else {
				host = u.Host
			}
			if u.User != nil {
				user = u.User.Username()
				pass, _ = u.User.Password()
			}
			name = u.Query().Get("database")
		}
	}
	return host, port, user, pass, name
}

// CheckPort 检测本机端口占用情况（设置页端口检测用）。
func (s *DbConfigService) CheckPort(port int) (occupied bool, detail string) {
	if port == s.srv.Port {
		return true, "当前服务正在使用该端口"
	}
	if portprobe.Free(s.srv.Host, port) {
		return false, "端口空闲，可使用"
	}
	return true, "端口已被其他程序占用"
}
