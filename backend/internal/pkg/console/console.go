// 重定向或后台日志（data/server.log）自动降级为纯文本，不留颜色乱码
package console

// FIXME: banner字符画硬编码的,以后看要不要换成figlet库
import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Version 版本号（与前端页脚一致）
const Version = "v0.1.0"

var (
	isTTY  = detectTTY()
	colors = isTTY && os.Getenv("NO_COLOR") == ""
)

func IsTTY() bool { return isTTY }

func detectTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

const (
	cReset   = "\033[0m"
	cDim     = "\033[2m"
	cCyan    = "\033[36m"
	cGreen   = "\033[32m"
	cYellow  = "\033[33m"
	cRed     = "\033[31m"
	cHiWhite = "\033[97m"
	cHiBg    = "\033[7m" // 反白高亮（地址/密钥）
)

func paint(code, s string) string {
	if !colors {
		return s
	}
	return code + s + cReset
}

var reANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visible 去掉 ANSI 码后的显示宽度（按等宽字符计，中文按 2 列宽估算）。
func visible(s string) int {
	n := 0
	for _, r := range reANSI.ReplaceAllString(s, "") {
		if r > 0x2E7F { // CJK 与全角
			n += 2
		} else {
			n++
		}
	}
	return n
}

var banner = []string{
	`████████╗███████╗██████╗ ███╗   ███╗██╗  ██╗`,
	`╚══██╔══╝██╔════╝██╔══██╗████╗ ████║╚██╗██╔╝`,
	`   ██║   █████╗  ██████╔╝██╔████╔██║ ╚███╔╝ `,
	`   ██║   ██╔══╝  ██╔══██╗██║╚██╔╝██║ ██╔██╗ `,
	`   ██║   ███████╗██║  ██║██║ ╚═╝ ██║██╔╝ ██╗`,
	`   ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚═╝  ╚═╝`,
}

// （监听 0.0.0.0 时为探测到的局域网 IP,回环监听时为 127.0.0.1）。
func Startup(listen, access, termxPrefix string, configured bool) {
	if isTTY {
		fmt.Println()
		for _, l := range banner {
			fmt.Println(paint(cCyan, l))
		}
		fmt.Println(paint(cDim, "          TermX · 自托管凭据管理服务 · "+Version))
		fmt.Println()
	}

	rows := [][2]string{
		{"服务状态", paint(cGreen, "✔ 已启动")},
		{"监听地址", listen},
		{"访问地址", paint(cHiBg, " "+access+" ")},
	}
	//同步接口在安装引导中才设置——未初始化前不展示，避免误导
	if configured {
		rows = append(rows, [2]string{"同步接口", termxPrefix})
	}
	var status string
	if configured {
		status = paint(cGreen, "✔ 系统已初始化，可直接登录使用")
	} else {
		status = paint(cYellow, "⚠ 系统尚未初始化：请用浏览器打开上方访问地址完成安装引导")
	}
	rows = append(rows, [2]string{"初始化", status})

	if isTTY {
		printBox(rows)
	} else {
		for _, r := range rows {
			fmt.Printf("  %-8s %s\n", r[0]+":", r[1])
		}
	}

	fmt.Println(paint(cDim, " 常用命令:"))
	fmt.Println("   停止服务   " + paint(cHiWhite, "./termx-server --stop"))
	fmt.Println("   一键卸载   " + paint(cHiWhite, "./termx-server --uninstall"))
	fmt.Println("   查看密钥   " + paint(cHiWhite, "./termx-server --show-key") + paint(cDim, "   （仅 TermX 客户端需要）"))
	fmt.Println("   命令帮助   " + paint(cHiWhite, "./termx-server --help"))
	fmt.Println()
}

// printBox 终端模式渲染带边框的信息面板：内容超宽自动换行缩进，不溢出边框。
func printBox(rows [][2]string) {
	const w = 54
	fmt.Println(paint(cDim, " ╭"+strings.Repeat("─", w)+"╮"))
	for _, r := range rows {
		head := " " + r[0] + ": "
		if visible(head)+visible(r[1]) <= w-1 {
			pad := w - visible(head) - visible(r[1]) - 1
			fmt.Println(paint(cDim, "│") + head + r[1] + strings.Repeat(" ", max(1, pad)) + paint(cDim, "│"))
			continue
		}
		//超宽：标签独行，内容折行缩进（保留 ANSI 颜色段不切断）
		fmt.Println(paint(cDim, "│") + head + strings.Repeat(" ", max(1, w-visible(head)-1)) + paint(cDim, "│"))
		for _, seg := range wrapText(r[1], w-6) {
			line := "   " + seg
			pad := w - visible(line) - 1
			fmt.Println(paint(cDim, "│") + line + strings.Repeat(" ", max(1, pad)) + paint(cDim, "│"))
		}
	}
	fmt.Println(paint(cDim, " ╰"+strings.Repeat("─", w)+"╯"))
}

func wrapText(s string, width int) []string {
	var out []string
	var cur strings.Builder
	curW := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b { // ANSI 转义段
			j := i
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++
			}
			cur.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		rw := 1
		if r > 0x2E7F {
			rw = 2
		}
		if curW+rw > width && curW > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteString(s[i : i+size])
		curW += rw
		i += size
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

func Secret(title, value string) {
	if isTTY {
		const w = 52
		fmt.Println(paint(cDim, " ╭"+strings.Repeat("─", w)+"╮"))
		tl := " " + title + " "
		fmt.Println(paint(cDim, "│") + paint(cYellow, tl) + strings.Repeat(" ", max(1, w-visible(tl))) + paint(cDim, "│"))
		vl := " " + paint(cHiBg, " "+value+" ") + " "
		fmt.Println(paint(cDim, "│") + vl + strings.Repeat(" ", max(1, w-visible(vl))) + paint(cDim, "│"))
		fmt.Println(paint(cDim, " ╰"+strings.Repeat("─", w)+"╯"))
		return
	}
	fmt.Printf("[%s] %s\n", title, value)
}

func Phase(msg string) { fmt.Println("  " + paint(cGreen, "✔ ") + msg) }

func OK(msg string) { fmt.Println(paint(cGreen, "✔ ") + msg) }

func Bad(msg string) { fmt.Println(paint(cRed, "✗ ") + msg) }

// Info 普通说明行（暗色前缀）
func Info(msg string) { fmt.Println(paint(cDim, "· ") + msg) }

func Warn(msg string) { fmt.Println(paint(cYellow, "⚠ ") + msg) }

// Title 子命令标题（卸载/停止等 CLI 入口）
func Title(s string) {
	if isTTY {
		fmt.Println()
		fmt.Println(paint(cCyan, "── ") + s + paint(cCyan, " ──"))
		fmt.Println(paint(cDim, strings.Repeat("-", visible(s)+6)))
		return
	}
	fmt.Println("== " + s + " ==")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
