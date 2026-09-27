// 查看 TermX 接入密钥（./termx-server --show-key）：
// 启动过程不再展示任何密钥（以安装引导中的密钥为准），
// TermX 桌面客户端配对时按需用本命令查看。
package main

import (
	"os"

	"ky/internal/config"
	"ky/internal/pkg/console"
	"ky/internal/pkg/keyring"
	"ky/internal/pkg/termxkey"
)

// runShowKey 显示 TermX 接入密钥，返回进程退出码。
func runShowKey() int {
	console.Title("TermX 接入密钥")
	cfg, err := config.Read()
	if err != nil {
		console.Bad("读取配置失败: " + err.Error())
		return 1
	}
	var kr *keyring.Keyring
	if _, rerr := os.ReadFile(cfg.Security.KeyFile); rerr == nil {
		if k, kerr := keyring.Load(cfg.Security.KeyFile); kerr == nil {
			kr = k
		}
	}
	key := termxkey.Peek(cfg.Security.TermxApiKey, kr, "data/termx-key.bin")
	if key == "" {
		console.Warn("尚未生成（系统未初始化或未启用 TermX 同步）")
		return 1
	}
	console.Secret("TermX 接入密钥 · 粘贴到 TermX 桌面客户端设置", key)
	console.Info("仅 TermX 桌面客户端需要；不使用客户端可忽略。重新生成：删除 data/termx-key.bin 后重启")
	return 0
}
