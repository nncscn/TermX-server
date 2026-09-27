package config

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 服务全局配置,字段与 config.yaml 一一对应。
type Config struct {
	Server   Server   `yaml:"server"`
	Database Database `yaml:"database"`
	Security Security `yaml:"security"`
	// 接口层的"已初始化"判定以账户表非空为准，此标记仅作横幅提示与防御性用途。
	SetupCompleted bool `yaml:"setup_completed"`
}

type Server struct {
	Host  string `yaml:"host"`
	Port  int    `yaml:"port"`
	HTTPS bool   `yaml:"https"`
	// CertFile/KeyFile TLS 证书与私钥文件路径（HTTPS 开启时生效）。
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	// TermxBasePath TermX 桌面客户端同步接口的挂载前缀（安装引导可自定义，
	TermxBasePath  string   `yaml:"termx_base_path"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

func (s Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// DisplayAddr 返回给用户展示的访问地址（0.0.0.0 按本机回环展示）
func (s Server) DisplayAddr() string {
	host := s.Host
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}
	scheme := "http"
	if s.HTTPS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, s.Port)
}

type Database struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// Security 安全配置：登录密码加密密钥文件与初始管理员账户。
type Security struct {
	KeyFile      string `yaml:"key_file"`      // AES-256 密钥文件路径（base64 文本，不入库）
	SeedAdmin    bool   `yaml:"seed_admin"`    // 启动时账户表为空是否播种管理员
	SeedUsername string `yaml:"seed_username"` // 播种账户名
	SeedPassword string `yaml:"seed_password"` // 播种账户密码（加密后落库）
	// TermxApiKey TermX 桌面客户端的同步接入密钥（X-Server-Key 头）。
	// 接口默认开启：留空 = 启动时自动生成并以 keyring 加密落盘 data/termx-key.bin
	// （非明文，生成时日志展示一次）；显式填写 = 优先使用（调试/过渡用）
	TermxApiKey string `yaml:"termx_api_key"`
}

// 内含配置、数据库、密钥等全部运行时文件）。旧版把 config.yaml 放在根目录，
const (
	Dir        = "data"                 // 运行时目录
	File       = "data/config.yaml"     // 配置文件
	BackupFile = "data/config.yaml.bak" // 配置备份（切库/自愈用）
)

// migrateLegacyConfig 兼容旧布局：根目录的 config.yaml（及备份）搬进 data/。
func migrateLegacyConfig() {
	_ = os.MkdirAll(Dir, 0o755)
	if _, err := os.Stat(File); err == nil {
		return
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		_ = os.Rename("config.yaml", File)
	}
	if _, err := os.Stat("config.yaml.bak"); err == nil {
		if _, err2 := os.Stat(BackupFile); err2 != nil {
			_ = os.Rename("config.yaml.bak", BackupFile)
		}
	}
}

func Load() *Config {
	cfg, err := Read()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	return cfg
}

func Read() (*Config, error) {
	migrateLegacyConfig()
	cfg := Default()
	data, err := os.ReadFile(File)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // 首次运行没有配置文件，全部走默认值
		}
		return nil, fmt.Errorf("读取配置文件: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件: %w", err)
	}
	return cfg, nil
}

// Default 返回默认配置
func Default() *Config {
	return &Config{
		Server: Server{
			Host:          "0.0.0.0",
			Port:          18080,
			TermxBasePath: "/api",
		},
		Database: Database{Driver: "sqlite", DSN: "data/app.db"},
		Security: Security{
			KeyFile:      "data/auth.key",
			SeedAdmin:    false,
			SeedUsername: "admin",
			SeedPassword: "",
		},
	}
}

// Save 把当前配置写回（调用方需先自行备份原文件）
// 注：整文件重写，原有注释不会保留。
func (c *Config) Save() error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置: %w", err)
	}
	_ = os.MkdirAll(Dir, 0o755)
	if err := os.WriteFile(File, data, 0o600); err != nil {
		return fmt.Errorf("写入配置: %w", err)
	}
	return nil
}

// RestoreBackupIfChanged 自愈用：当前配置与备份内容不同时，用备份覆盖当前配置，
// 返回是否执行了回滚（两次连续失败时备份与当前一致，返回 false 终止自愈循环）
func RestoreBackupIfChanged() bool {
	cur, err1 := os.ReadFile(File)
	bak, err2 := os.ReadFile(BackupFile)
	if err1 != nil || err2 != nil || string(cur) == string(bak) {
		return false
	}
	return os.WriteFile(File, bak, 0o644) == nil
}
