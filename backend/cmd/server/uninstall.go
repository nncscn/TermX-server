// 一键卸载向导（./termx-server --uninstall [-y]）：交互式清理外部业务库、
// 本地数据目录与程序本身。所有删除动作默认拒绝（输 y 才执行），服务运行中拒绝卸载。
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"ky/internal/config"
	"ky/internal/database"
	"ky/internal/service"
)

// runUninstall 卸载主流程，返回进程退出码。autoYes=true（-y）时全部按是处理。
func runUninstall(autoYes bool) int {
	reader := bufio.NewReader(os.Stdin)
	ask := func(format string, a ...any) bool {
		if autoYes {
			return true
		}
		fmt.Printf(format+" [y/N]: ", a...)
		line, _ := reader.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true
		}
		return false
	}

	fmt.Println("TermX · 一键卸载")
	fmt.Println("----------------------------------------")

	cfg, err := config.Read()
	if err != nil {
		cfg = config.Default()
	}

	// 服务运行中拒绝卸载（避免删掉正在使用的库文件造成状态错乱）
	if conn, derr := net.DialTimeout("tcp", cfg.Server.Addr(), time.Second); derr == nil {
		_ = conn.Close()
		fmt.Println("✗ 检测到服务正在运行：请先停止（前台 Ctrl+C，或 pkill -f termx-server）再执行卸载")
		return 1
	}
	if wd, _ := os.Getwd(); wd != "" {
		fmt.Println("工作目录:", wd)
	}

	// ① 外部业务库（sqlite 模式无此项）
	if cfg.Database.Driver != "" && cfg.Database.Driver != "sqlite" {
		host, port, user, pass, name := service.ParseDSN(cfg.Database.Driver, cfg.Database.DSN)
		if name != "" {
			fmt.Printf("当前使用外部 %s 业务库「%s」（%s:%d）\n", cfg.Database.Driver, name, host, port)
			if ask("是否一并删除该业务库？") {
				if derr := database.DropDatabase(cfg.Database.Driver, host, port, user, pass, name); derr != nil {
					fmt.Println("✗ 删除业务库失败:", derr)
				} else {
					fmt.Println("✓ 业务库已删除")
				}
			} else {
				fmt.Println("- 业务库保留（如需手动删除可用数据库管理工具）")
			}
		}
	}

	// ② 本地数据目录（配置/SQLite 库/密钥，不可恢复）
	if ask("是否删除本地数据目录 data/（全部配置与数据，不可恢复）？") {
		if rerr := os.RemoveAll(config.Dir); rerr != nil {
			fmt.Println("✗ 删除 data/ 失败:", rerr)
		} else {
			fmt.Println("✓ data/ 目录已删除")
		}
	} else {
		fmt.Println("- data/ 保留（重装/重置引导时可直接删除）")
	}

	// ③ 程序本身
	if ask("是否删除程序本身（termx-server）？") {
		exe, eerr := os.Executable()
		if eerr != nil {
			fmt.Println("✗ 定位程序失败:", eerr)
			return 1
		}
		if rerr := os.Remove(exe); rerr != nil {
			fmt.Println("✗ 删除程序失败:", rerr)
			return 1
		}
		fmt.Println("✓ 程序已删除")
	} else {
		fmt.Println("- 程序保留")
	}

	fmt.Println("----------------------------------------")
	fmt.Println("卸载完成，感谢使用 TermX。")
	return 0
}

// printUsage 命令行用法（./ky-server -h）。
func printUsage() {
	fmt.Println("TermX 服务程序")
	fmt.Println()
	fmt.Println("用法:")
	fmt.Println("  termx-server                正常启动服务（完成安装引导后自动转入后台）")
	fmt.Println("  termx-server --stop         停止后台运行的服务")
	fmt.Println("  termx-server --show-key     查看 TermX 接入密钥（仅 TermX 桌面客户端需要）")
	fmt.Println("  termx-server --uninstall    一键卸载向导（交互确认，可分别清理业务库/数据/程序）")
	fmt.Println("  termx-server --uninstall -y 卸载且全部默认确认（脚本用，慎用）")
	fmt.Println("  termx-server -h | --help    显示本帮助")
}
