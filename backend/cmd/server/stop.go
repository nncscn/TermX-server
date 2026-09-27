// 停止后台服务（./termx-server --stop）：读取 data/termx-server.pid 精准停止，
// 避免 pkill 误伤其他同名进程；前台运行的服务直接 Ctrl+C 即可。
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ky/internal/pkg/console"
)

// runStop 停止后台守护进程，返回进程退出码。
func runStop() int {
	console.Title("停止服务")
	data, err := os.ReadFile("data/termx-server.pid")
	if err != nil {
		console.Warn("未找到 PID 记录（服务未在后台运行；前台运行请直接 Ctrl+C）")
		return 1
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		console.Bad("PID 记录无效: " + strings.TrimSpace(string(data)))
		return 1
	}
	if err := syscall.Kill(pid, 0); err != nil {
		_ = os.Remove("data/termx-server.pid")
		console.OK(fmt.Sprintf("服务（PID %d）本就未在运行，已清理残留记录", pid))
		return 0
	}
	// 防 PID 复用误杀：确认目标进程确属 ky-server 再发信号
	if !pidIsTermxServer(pid) {
		_ = os.Remove("data/termx-server.pid")
		console.Bad(fmt.Sprintf("PID %d 已被其他进程复用（不是 ky-server），已清理残留记录且不执行停止", pid))
		return 1
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		console.Bad(fmt.Sprintf("停止失败: %v", err))
		return 1
	}
	for i := 0; i < 50; i++ { // 最多等 5 秒优雅退出
		if err := syscall.Kill(pid, 0); err != nil {
			_ = os.Remove("data/termx-server.pid")
			console.OK(fmt.Sprintf("服务已停止（PID %d）", pid))
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	_ = os.Remove("data/termx-server.pid")
	console.OK(fmt.Sprintf("服务已强制停止（PID %d）", pid))
	return 0
}

// pidIsTermxServer 校验 /proc/<pid>/cmdline 是否为 ky-server（防 PID 复用误杀）。
func pidIsTermxServer(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(raw), "\x00", " "), "termx-server")
}
