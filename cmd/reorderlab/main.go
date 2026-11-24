// reorderlab 生成三张测试图，对每张图执行“重排前 GTR1 / 重排后 GTR2”
// 全流程对照：整图游程数、体积、解码逐像素一致性、重排前后索引序列，
// 并把结果汇总成 report.md。给定下降比例 ρ 由 --target 指定。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gotoolkit/api"
)

const (
	W = 64 // 测试图画幅
	H = 64
)

// xorshift 确定性伪随机源，保证产物可复现。
type prng struct{ s uint64 }

func (p *prng) next() uint64 {
	p.s ^= p.s << 13
	p.s ^= p.s >> 7
	p.s ^= p.s << 17
	return p.s
}

func genNoisy() []uint8 {
	p := &prng{s: 0x9e3779b97f4a7c15}
	pix := make([]uint8, W*H)
	for i := range pix {
		pix[i] = uint8(p.next() % 12) // 12 个灰度级
	}
	return pix
}

func genGradient() []uint8 {
	pix := make([]uint8, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			pix[y*W+x] = uint8(x * 31 / (W - 1)) // 32 级水平渐变
		}
	}
	return pix
}

func genBlocks() []uint8 {
	pix := make([]uint8, W*H)
	rect := func(x0, y0, w, h int, v uint8) {
		for y := y0; y < y0+h; y++ {
			for x := x0; x < x0+w; x++ {
				pix[y*W+x] = v
			}
		}
	}
	rect(4, 4, 24, 18, 3)
	rect(36, 8, 22, 14, 5)
	rect(8, 34, 18, 24, 2)
	rect(32, 30, 28, 26, 7)
	for i := 0; i < W; i++ { // 一条对角细线，制造零散游程
		pix[i*W+i] = 9
	}
	return pix
}

type result struct {
	name      string
	k         int // 不同索引个数
	runs0     int // 重排前游程数（整图）
	runs1     int // 重排后游程数（整图）
	size0     int // GTR1 字节数
	size1     int // GTR2 字节数
	identical bool // 解码逐像素一致
	greedy    int // 分窗贪心游程数
	greedyW   int
	greedyMax int // 上界 ⌈n/W⌉·k
}

func distinct(pix []uint8) int {
	var seen [256]bool
	k := 0
	for _, v := range pix {
		if !seen[v] {
			seen[v] = true
			k++
		}
	}
	return k
}

func writeFile(dir, name string, body []byte) error {
	return os.WriteFile(filepath.Join(dir, name), body, 0o644)
}

// dumpIndices 把索引序列写成文本：首行元信息，之后每行 32 项，便于逐项比对。
func dumpIndices(pix []uint8, runs int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# n=%d runs=%d\n", len(pix), runs)
	for i, v := range pix {
		if i > 0 {
			if i%32 == 0 {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		}
		fmt.Fprintf(&b, "%3d", v)
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

func comparePix(a, b []uint8) (mismatch, maxDiff int) {
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d != 0 {
			mismatch++
		}
		if d > maxDiff {
			maxDiff = d
		}
	}
	return mismatch, maxDiff
}

func runOne(dir, name string, pix []uint8, greedyW int) (*result, error) {
	img := &api.Image{W: W, H: H, Pix: pix}
	r := &result{name: name, k: distinct(pix), greedyW: greedyW}

	pgm, err := api.Encode(img, api.Options{Format: "pgm"})
	if err != nil {
		return nil, err
	}
	before, err := api.Encode(img, api.Options{Format: "rle"})
	if err != nil {
		return nil, err
	}
	after, err := api.Encode(img, api.Options{Format: "rle", Reorder: true})
	if err != nil {
		return nil, err
	}
	r.runs0 = api.CountRuns(pix)
	seq, _ := api.ReorderSequence(pix)
	r.runs1 = api.CountRuns(seq)
	r.size0 = len(before)
	r.size1 = len(after)
	r.greedy = api.BatchWindowRuns(pix, greedyW)
	n := len(pix)
	r.greedyMax = ((n + greedyW - 1) / greedyW) * r.k

	// 解码对照：两条流各自解码，与原图逐像素比
	dec0, err := api.Decode(before, api.Options{Format: "rle"})
	if err != nil {
		return nil, fmt.Errorf("GTR1 解码: %w", err)
	}
	dec1, err := api.Decode(after, api.Options{Format: "rle"})
	if err != nil {
		return nil, fmt.Errorf("GTR2 解码: %w", err)
	}
	mm0, md0 := comparePix(pix, dec0.Pix)
	mm1, md1 := comparePix(pix, dec1.Pix)
	r.identical = mm0 == 0 && mm1 == 0

	pgm0, _ := api.Encode(dec0, api.Options{Format: "pgm"})
	pgm1, _ := api.Encode(dec1, api.Options{Format: "pgm"})
	cmp := fmt.Sprintf("image=%s n=%d\nbefore(GTR1): mismatches=%d maxdiff=%d\nafter(GTR2):  mismatches=%d maxdiff=%d\nverdict: %s\n",
		name, n, mm0, md0, mm1, md1, map[bool]string{true: "IDENTICAL", false: "DIFFER"}[r.identical])

	files := map[string][]byte{
		name + ".pgm":                pgm,
		name + ".before.rle":         before,
		name + ".after.rle2":         after,
		name + ".decoded-before.pgm": pgm0,
		name + ".decoded-after.pgm":  pgm1,
		name + ".compare.txt":        []byte(cmp),
		name + ".indices-before.txt": dumpIndices(pix, r.runs0),
		name + ".indices-after.txt":  dumpIndices(seq, r.runs1),
	}
	for fn, body := range files {
		if err := writeFile(dir, fn, body); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func main() {
	out := flag.String("out", "artifacts/reorder", "产物目录")
	target := flag.Float64("target", 0.5, "给定下降比例 ρ：要求 (R前-R后)/R前 >= ρ，按整图统计")
	greedyW := flag.Int("window", 256, "分窗贪心的窗口大小 W")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "建目录失败:", err)
		os.Exit(1)
	}
	images := []struct {
		name string
		pix  []uint8
	}{
		{"noisy", genNoisy()},
		{"gradient", genGradient()},
		{"blocks", genBlocks()},
	}
	var rs []*result
	fail := false
	for _, im := range images {
		r, err := runOne(*out, im.name, im.pix, *greedyW)
		if err != nil {
			fmt.Fprintln(os.Stderr, im.name, "失败:", err)
			os.Exit(1)
		}
		rs = append(rs, r)
		drop := float64(r.runs0-r.runs1) / float64(r.runs0)
		ok := r.identical && drop >= *target && r.greedy <= r.greedyMax
		if !ok {
			fail = true
		}
		fmt.Printf("%-8s runs %d -> %d (drop %.1f%%, target %.0f%%) size %d -> %d B identical=%v greedy(W=%d)=%d<=%d %s\n",
			r.name, r.runs0, r.runs1, drop*100, *target*100, r.size0, r.size1,
			r.identical, r.greedyW, r.greedy, r.greedyMax, map[bool]string{true: "PASS", false: "FAIL"}[ok])
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# 索引重排对照报告\n\n")
	fmt.Fprintf(&b, "画幅 %dx%d（n=%d），游程数按整图统计（不抽样）。给定下降比例 ρ=%.0f%%。\n\n", W, H, W*H, *target*100)
	fmt.Fprintf(&b, "| 图像 | k(不同索引) | 游程数(前) | 游程数(后) | 下降比例 | 体积前(GTR1,B) | 体积后(GTR2,B) | 解码逐像素一致 |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|\n")
	tot0, tot1 := 0, 0
	for _, r := range rs {
		drop := float64(r.runs0-r.runs1) / float64(r.runs0)
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %.2f%% | %d | %d | %v |\n",
			r.name, r.k, r.runs0, r.runs1, drop*100, r.size0, r.size1, r.identical)
		tot0 += r.runs0
		tot1 += r.runs1
	}
	fmt.Fprintf(&b, "| 合计 | - | %d | %d | %.2f%% | - | - | - |\n\n",
		tot0, tot1, float64(tot0-tot1)/float64(tot0)*100)
	fmt.Fprintf(&b, "近似算法（分窗贪心，W=%d）误差界验证：R <= ⌈n/W⌉·k\n\n", *greedyW)
	fmt.Fprintf(&b, "| 图像 | 贪心游程数 | 上界 ⌈n/W⌉·k | 满足界 |\n|---|---|---|---|\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "| %s | %d | %d | %v |\n", r.name, r.greedy, r.greedyMax, r.greedy <= r.greedyMax)
	}
	fmt.Fprintf(&b, "\n说明：重排后游程数恒等于 k（理论最优 R*=k，见 docs/palette-reorder.md）；\n")
	fmt.Fprintf(&b, "GTR2 体积含逆置换所需的差分变长置换流，解码与原图逐像素相同。\n")
	fmt.Fprintf(&b, "每张图另存：解码对照 PGM 两份、compare.txt、重排前后索引序列各一份。\n")
	if err := writeFile(*out, "report.md", []byte(b.String())); err != nil {
		fmt.Fprintln(os.Stderr, "写报告失败:", err)
		os.Exit(1)
	}
	if fail {
		fmt.Fprintln(os.Stderr, "存在未达标项")
		os.Exit(1)
	}
	fmt.Println("all checks passed ->", filepath.Join(*out, "report.md"))
}
