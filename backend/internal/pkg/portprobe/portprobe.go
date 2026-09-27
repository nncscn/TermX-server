package portprobe

import (
	"fmt"
	"net"
)

// Free 探测 host:port 是否可绑定（空闲）。绑定失败即视为被占用。
func Free(host string, port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
