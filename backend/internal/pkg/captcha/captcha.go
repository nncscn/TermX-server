package captcha

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	ImgWidth  = 300
	ImgHeight = 120
	// Tolerance 点击命中半径：距汉字中心 ≤ 该值视为点中
	// 字形 21-30px 且带 ±22° 旋转，视觉中心与锚点存在几像素偏差，
	// 叠加用户点击误差,22px 偏紧会误判"点对了却报错";放宽到 30px
	Tolerance = 30
	ttl       = 60 * time.Second
)

var idioms = []string{
	"一帆风顺", "马到成功", "心想事成", "万事如意", "花好月圆",
	"海阔天空", "风和日丽", "春暖花开", "山清水秀", "水到渠成",
	"火树银花", "雪中送炭", "雷厉风行", "云淡风轻", "日新月异",
	"星火燎原", "金玉满堂", "木已成舟", "天长地久", "龙飞凤舞",
	"虎头蛇尾", "鸟语花香", "山高水长", "风平浪静", "柳暗花明",
	"水落石出", "春华秋实", "安居乐业", "光明磊落", "温故知新",
}

// 干扰字池：字形差异大、不易与成语字混淆的常用字。
var charset = []rune("山水火木金土风云雷电冰雪日月星天地江河湖海松竹梅兰菊枫")

type Challenge struct {
	ID     string
	SVG    string // 完整 SVG 文本
	Prompt string // 提示语，如"请按顺序点击：一帆风顺"
	answer []Point
	expire time.Time
}

// Point 原图画布坐标
type Point struct{ X, Y float64 }

// Store 内存挑战存储（互斥锁保护，懒过期清理）。
type Store struct {
	mu   sync.Mutex
	data map[string]*Challenge
}

func NewStore() *Store {
	s := &Store{data: map[string]*Challenge{}}
	go s.cleanup()
	return s
}

// cleanup 定时清理过期挑战（每 60 秒一次，防止无人消费的挑战无限累积）。
func (s *Store) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, c := range s.data {
			if now.After(c.expire) {
				delete(s.data, id)
			}
		}
		s.mu.Unlock()
	}
}

// Generate 生成新挑战并登记入存储。
func (s *Store) Generate() *Challenge {
	c := generate()
	s.mu.Lock()
	s.cleanup()
	s.data[c.ID] = c
	s.mu.Unlock()
	return c
}

func (s *Store) Consume(id string, clicks []Point) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.data[id]
	if !ok || time.Now().After(c.expire) {
		delete(s.data, id)
		return false
	}
	delete(s.data, id) // 无论对错一次作废，防重放与反复尝试
	if len(clicks) != len(c.answer) {
		return false
	}
	for i, cl := range clicks {
		if math.Hypot(cl.X-c.answer[i].X, cl.Y-c.answer[i].Y) > Tolerance {
			return false
		}
	}
	return true
}

// cleanup 清理过期挑战（调用方需持锁）。

func generate() *Challenge {
	idiom := idioms[int(randFloat(float64(len(idioms))))]
	targets := []rune(idiom)

	pool := make([]rune, len(charset))
	copy(pool, charset)
	shuffleRune(pool)
	used := map[rune]bool{}
	for _, t := range targets {
		used[t] = true
	}
	distractors := make([]rune, 0, 2)
	for _, c := range pool {
		if len(distractors) >= 2 {
			break
		}
		if !used[c] {
			distractors = append(distractors, c)
			used[c] = true
		}
	}
	all := append(append([]rune{}, targets...), distractors...)
	shuffleRune(all) // 打散成语与干扰字的图上位置

	// 极端情况下退回 2×3 网格均布兜底——保证落点数与字符数严格一致，
	var centers []Point
	placed := false
	for round := 0; round < 8 && !placed; round++ {
		for minDist := 56.0; minDist >= 46; minDist -= 2 {
			centers = centers[:0]
			for attempt := 0; attempt < 400 && len(centers) < len(all); attempt++ {
				p := Point{
					X: 30 + randFloat(ImgWidth-60),
					Y: 34 + randFloat(ImgHeight-68),
				}
				ok := true
				for _, c := range centers {
					if math.Hypot(p.X-c.X, p.Y-c.Y) < minDist {
						ok = false
						break
					}
				}
				if ok {
					centers = append(centers, p)
				}
			}
			if len(centers) == len(all) {
				placed = true
				break
			}
		}
	}
	if !placed {
		centers = centers[:0]
		const cols, rows = 3, 2
		for k := 0; k < len(all); k++ {
			centers = append(centers, Point{
				X: 30 + (float64(k%cols)+0.5)*(float64(ImgWidth-60)/cols),
				Y: 34 + (float64(k/cols)+0.5)*(float64(ImgHeight-68)/rows),
			})
		}
	}

	//绘制 SVG：噪点 → 干扰线 → 汉字（旋转/字号/深浅抖动）
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, ImgWidth, ImgHeight, ImgWidth, ImgHeight)
	b.WriteString(`<rect width="100%" height="100%" fill="#f4f8fd"/>`)
	for i := 0; i < 26; i++ { // 噪点
		fmt.Fprintf(&b, `<circle cx="%.0f" cy="%.0f" r="%.1f" fill="#c9d8ec" opacity="%.2f"/>`,
			randFloat(ImgWidth), randFloat(ImgHeight), 1+randFloat(2), 0.35+randFloat(0.4))
	}
	for i := 0; i < 3; i++ { // 干扰弧线
		fmt.Fprintf(&b, `<path d="M %.0f %.0f Q %.0f %.0f %.0f %.0f" stroke="#bcd0e8" stroke-width="1.2" fill="none" opacity="0.55"/>`,
			randFloat(ImgWidth), randFloat(ImgHeight), randFloat(ImgWidth), randFloat(ImgHeight), randFloat(ImgWidth), randFloat(ImgHeight))
	}
	for i, ch := range all {
		size := 21 + randFloat(9)
		angle := -22 + randFloat(44)
		gray := 40 + int(randFloat(60)) // #28~#64 深灰蓝，浏览器字体渲染
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="%.0f" fill="#%02x%02x%02x" text-anchor="middle" dominant-baseline="central" transform="rotate(%.1f %.1f %.1f)" font-family="'Noto Sans CJK SC','PingFang SC','Microsoft YaHei',sans-serif">%s</text>`,
			centers[i].X, centers[i].Y, size, gray, gray+4, gray+8, angle, centers[i].X, centers[i].Y, string(ch))
	}
	b.WriteString(`</svg>`)

	answer := make([]Point, 0, len(targets))
	for _, t := range targets {
		for i, ch := range all {
			if ch == t {
				answer = append(answer, centers[i])
				break
			}
		}
	}
	return &Challenge{
		ID:     randomID(),
		SVG:    b.String(),
		Prompt: "请按顺序点击：" + idiom,
		answer: answer,
		expire: time.Now().Add(ttl),
	}
}

func (c *Challenge) SVGDataURI() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(c.SVG))
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func randFloat(max float64) float64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return float64(v%1e6) / 1e6 * max
}

func shuffleRune(runes []rune) {
	for i := len(runes) - 1; i > 0; i-- {
		j := int(randFloat(float64(i + 1)))
		runes[i], runes[j] = runes[j], runes[i]
	}
}
