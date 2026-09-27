package restart

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"time"
)

func Self(delay time.Duration) {
	go func() {
		time.Sleep(delay)
		exe, err := os.Executable()
		if err != nil {
			log.Printf("自重启失败（解析可执行路径）: %v", err)
			return
		}
		log.Printf("应用新配置，自动重启进程（PID %d → 原地替换）…", os.Getpid())
		if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
			log.Printf("自重启失败: %v，请手动重启后端", err)
		}
	}()
}

// SelfDaemonNow 启动即后台（同步）：横幅打印完成后调用。成功则父进程直接退出；
func SelfDaemonNow() error {
	pid, err := forkDaemon()
	if err != nil {
		return err
	}
	_ = pid
	fmt.Println("   ✔ 服务已在后台运行 · 实时日志: tail -f data/server.log")
	fmt.Println()
	os.Exit(0)
	return nil // 不可达
}

// SelfDaemon 延迟 delay 后转入后台（异步）：前台模式下安装引导完成时调用
func SelfDaemon(delay time.Duration) {
	go func() {
		time.Sleep(delay)
		pid, err := forkDaemon()
		if err != nil {
			log.Printf("后台化失败: %v，改为原地重启", err)
			Self(0)
			return
		}
		_ = pid
		fmt.Println()
		fmt.Println("  ╭────────────────────────────────────────────────╮")
		fmt.Println("  │    ✔ 安装完成，服务已转入后台运行               │")
		fmt.Println("  ╰────────────────────────────────────────────────╯")
		fmt.Println()
		fmt.Println("    查看日志   tail -f data/server.log")
		fmt.Println("    停止服务   ./termx-server --stop")
		fmt.Println("    一键卸载   ./termx-server --uninstall")
		fmt.Println("    查看密钥   ./termx-server --show-key   （仅 TermX 客户端）")
		fmt.Println()
		fmt.Println("  本窗口已完成使命，可直接关闭。")
		fmt.Println()
		os.Exit(0)
	}()
}

// KY_DAEMON=1 环境标记），记录 PID，返回子进程 PID
// TODO: Windows下这套不work,要另做
func forkDaemon() (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("解析可执行路径: %w", err)
	}
	_ = os.MkdirAll("data", 0o755)
	logf, err := os.OpenFile("data/server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("打开日志文件: %w", err)
	}
	defer logf.Close()
	null, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		null = logf // 极端情况：stdin 也指向日志（只读打开无副作用）
	} else {
		defer null.Close()
	}
	pid, err := syscall.ForkExec(exe, os.Args, &syscall.ProcAttr{
		Env:   append(os.Environ(), "KY_DAEMON=1"),
		Files: []uintptr{null.Fd(), logf.Fd(), logf.Fd()},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("分裂守护进程: %w", err)
	}
	_ = os.WriteFile("data/termx-server.pid", []byte(fmt.Sprintf("%d", pid)), 0o600)
	return pid, nil
}

func IsDaemon() bool {
	return os.Getenv("KY_DAEMON") == "1"
}
