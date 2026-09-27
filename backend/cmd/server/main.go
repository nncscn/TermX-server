// Package main 是后端服务唯一入口，只负责装配：加载配置 → 初始化数据库 →
// 注册路由 → 启动 HTTP 服务。严禁在此编写任何业务逻辑。
package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"

	"gorm.io/gorm"

	"ky/internal/config"
	"ky/internal/database"
	"ky/internal/model"
	"ky/internal/pkg/console"
	"ky/internal/pkg/portprobe"
	"ky/internal/pkg/restart"
	"ky/internal/router"
)

func main() {
	// 参数解析：-f/--foreground 为前台运行标志（可在任意位置），其余为子命令
	foreground := false
	rest := []string{}
	for _, a := range os.Args[1:] {
		if a == "-f" || a == "--foreground" {
			foreground = true
			continue
		}
		rest = append(rest, a)
	}
	// CLI 子命令：一键卸载 / 停止后台服务 / 查看密钥 / 帮助（拦截后不进入服务启动流程）
	if len(rest) > 0 {
		autoYes := len(rest) > 1 && (rest[1] == "-y" || rest[1] == "--yes")
		switch rest[0] {
		case "--uninstall", "-u":
			os.Exit(runUninstall(autoYes))
		case "--stop", "-s":
			os.Exit(runStop())
		case "--show-key":
			os.Exit(runShowKey())
		case "--help", "-h", "help":
			printUsage()
			return
		default:
			fmt.Printf("未知参数: %s（%s --help 查看用法）\n", rest[0], os.Args[0])
			os.Exit(2)
		}
	}

	cfg := config.Load()
	_ = os.MkdirAll("data", 0o755) // 数据目录：sqlite 库/密钥文件/日志均在此
	ensurePort(cfg)
	hardenFilePerms(cfg)

	db, err := initDatabase(cfg)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	engine := router.Setup(cfg, db)

	// 启动即后台：交互终端且非 -f 前台模式、非已是守护进程时，横幅打完
	// 立即分裂后台子进程并退出——终端马上回到提示符；失败回退前台运行。
	if !foreground && !restart.IsDaemon() && console.IsTTY() {
		if err := restart.SelfDaemonNow(); err != nil {
			log.Printf("自动后台化失败（%v），以前台模式继续运行", err)
		}
	}

	// HTTPS（安装引导设置）：证书不可用时按自愈模式回滚配置备份重启一次，
	// 仍失败则终止并明示（不静默降级 HTTP）。
	if cfg.Server.HTTPS {
		if _, err := tls.LoadX509KeyPair(cfg.Server.CertFile, cfg.Server.KeyFile); err != nil {
			log.Printf("HTTPS 证书加载失败: %v", err)
			if config.RestoreBackupIfChanged() {
				log.Printf("已自动回滚 config.yaml.bak，正在以原配置重启…")
				restart.Self(0)
				select {} // 等待 exec 替换进程
			}
			log.Fatalf("HTTPS 证书不可用且无备份可回滚，请检查 cert_file/key_file 配置后重启")
		}
		if err := engine.RunTLS(cfg.Server.Addr(), cfg.Server.CertFile, cfg.Server.KeyFile); err != nil {
			log.Fatalf("服务启动失败: %v", err)
		}
		return
	}
	if err := engine.Run(cfg.Server.Addr()); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// initDatabase 初始化数据库；失败时若能回滚到备份配置则自愈重启一次，
// 避免一次失败的切换把服务永久搞挂（回滚后仍失败则终止并留日志）。
func initDatabase(cfg *config.Config) (db *gorm.DB, err error) {
	db, err = database.Init(cfg)
	if err == nil {
		err = db.AutoMigrate(&model.Account{}, &model.Session{}, &model.SshKey{}, &model.Credential{}, &model.Preference{}, &model.TermxState{})
	}
	if err != nil {
		log.Printf("数据库初始化失败: %v", err)
		if config.RestoreBackupIfChanged() {
			log.Printf("已自动回滚 config.yaml.bak，正在以原配置重启…")
			restart.Self(0)
			select {} // 等待 exec 替换进程
		}
	}
	return db, err
}

// hardenFilePerms 启动加固：把敏感文件权限收紧为 0600（尽力而为，不存在即跳过）。
// 覆盖：配置（含备份，DSN 可能含外部库密码）、SQLite 数据文件（含明文元数据）、
// 服务日志（播种时会写入一次性恢复密钥）、TermX 接入密钥（加密落盘）、切库冷却时间戳。
func hardenFilePerms(cfg *config.Config) {
	targets := []string{config.File, config.BackupFile, "data/server.log", "data/last_restart", "data/termx-key.bin"}
	if cfg.Database.Driver == "sqlite" {
		targets = append(targets, cfg.Database.DSN)
	}
	for _, f := range targets {
		if err := os.Chmod(f, 0o600); err == nil {
			log.Printf("已加固文件权限: %s (0600)", f)
		}
	}
}

// ensurePort 启动前端口体检：已有本程序实例在运行则拒绝启动（防双开写坏数据）；
// 配置端口被其他程序占用则自动切换到 10000 以上的空闲端口并写回配置
// （横幅会展示实际生效地址）。
func ensurePort(cfg *config.Config) {
	// 单实例守卫：pidfile 指向存活进程且不是自己 → 拒绝启动
	if data, err := os.ReadFile("data/termx-server.pid"); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 && pid != os.Getpid() {
			if err := syscall.Kill(pid, 0); err == nil {
				log.Fatalf("检测到另一个实例正在运行（PID %d）。请先 ./termx-server --stop 停止后再启动。", pid)
			}
			_ = os.Remove("data/termx-server.pid") // 陈旧记录，清理
		}
	}
	if portprobe.Free(cfg.Server.Host, cfg.Server.Port) {
		return
	}
	start := cfg.Server.Port + 1
	if start < 10000 {
		start = 10000
	}
	for p := start; p <= 65535; p++ {
		if portprobe.Free(cfg.Server.Host, p) {
			log.Printf("⚠ 端口 %d 已被其他程序占用，自动切换到 %d（已写回配置）", cfg.Server.Port, p)
			cfg.Server.Port = p
			if err := cfg.Save(); err != nil {
				log.Printf("端口切换写回配置失败: %v", err)
			}
			return
		}
	}
	log.Fatalf("配置端口 %d 被占用，且 10000-65535 范围内无空闲端口可用", cfg.Server.Port)
}
