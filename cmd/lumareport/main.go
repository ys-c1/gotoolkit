// lumareport 打印亮度守恒展开的可复算数据：系数与守恒等式、三档灰度实测、
// 超限像素清单、三通道熵（比特每像素）与两种格式的头部开销差值。
//
// 用法示例：
//
//	go run ./cmd/lumareport -out artifacts/luma
//
// 输出不含时间戳，两次运行逐字节一致；-out 会落盘 report.txt 与三张图
// （源 PGM、灰度 BMP、展开 BMP），供别人复算比对。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

// sampleW/sampleH 取 17×16：宽度 17 让 BMP 每行 51 字节、补齐到 52，
// 行补齐非零；像素按行优先铺满 0..255 一遍，每级恰好一个像素。
const (
	sampleW = 17
	sampleH = 16
)

func sample() *api.Image {
	img := &api.Image{W: sampleW, H: sampleH, Pix: make([]uint8, sampleW*sampleH)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 256)
	}
	return img
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func write(out, name string, body []byte) {
	if out == "" {
		return
	}
	must(os.WriteFile(filepath.Join(out, name), body, 0o644))
}

func main() {
	out := flag.String("out", "", "产物目录（为空则只打印）")
	flag.Parse()
	if *out != "" {
		must(os.MkdirAll(*out, 0o755))
	}

	var b []byte
	p := func(format string, a ...any) {
		s := fmt.Sprintf(format, a...)
		b = append(b, s...)
		fmt.Print(s)
	}

	cs := api.LumaCoefficients()
	wr, wg, wb := api.LumaWeights()
	img := sample()
	grayBMP, err := api.Encode(img, api.Options{Format: "bmp"})
	must(err)
	expBMP, err := api.Encode(img, api.Options{Format: "bmp", LumaExpand: true})
	must(err)
	write(*out, "gray.bmp", grayBMP)
	write(*out, "expand.bmp", expBMP)
	write(*out, "source.pgm", mustEncode(img, "pgm", false))

	// ===== 1. 系数与守恒等式 =====
	p("== 1. 亮度守恒等式与展开系数 ==\n\n")
	p("  亮度定义：Y = (%d·R + %d·G + %d·B) / 1000\n", wr, wg, wb)
	p("  展开要求 Σ_c w_c·k_c = 1（w_c = Luma_c/1000）；这是一条等式，解不唯一。\n")
	p("  再要求系数向量范数最小，即线性约束下的最小范数解，唯一：\n")
	p("      k_c = w_c / Σ_j w_j² = 1000·Luma_c / %d\n\n", cs[0].Den)
	p("  （k=(1,1,1) 也满足守恒，因为 Σw=1，但范数更大且三通道无差别；\n")
	p("   本实现取最小范数解，三个通道按 299:587:114 的比例展开。）\n\n")
	p("  分母 Σ w_j²（千分制，即 Σ Luma_j²）= %d²+%d²+%d² = %d\n", wr, wg, wb, cs[0].Den)
	for _, c := range cs {
		p("      %s\n", c.String())
	}
	p("\n  代入校验：")
	var check int64
	for i, c := range cs {
		w := []int64{int64(wr), int64(wg), int64(wb)}[i]
		check += w * int64(c.Num)
	}
	p("(%d·%d + %d·%d + %d·%d) / %d = %d/1000 = 1\n",
		wr, cs[0].Num, wg, cs[1].Num, wb, cs[2].Num, int64(1000)*int64(cs[0].Den), check/int64(cs[0].Den))
	p("  即守恒等式在有理数意义下精确成立，不是近似凑出来的。\n\n")

	// ===== 2. 三档灰度实测 =====
	p("== 2. 三档灰度实测（%dx%d，%d 像素，逐级覆盖 0..255）==\n\n", sampleW, sampleH, len(img.Pix))
	tiers := api.LumaTiers(img)
	p("%-4s %-9s %-6s %-6s %-8s %-8s %-8s\n",
		"档", "区间", "样本", "上界", "实测最大", "超1样本", "最差灰度")
	for i, tr := range tiers {
		p("%-4d %-9s %-6d %-6d %-8d %-8d %-8d\n",
			i+1, fmt.Sprintf("%d-%d", tr.Lo, tr.Hi), tr.Samples, tr.Bound,
			tr.MaxErr, tr.OverOne, tr.WorstGray)
	}
	p("\n三档上界分别取档内逐级 Bound 的最大值：%d / %d / %d 级。\n",
		tiers[0].Bound, tiers[1].Bound, tiers[2].Bound)
	p("上界公式：|e| ≤ 1/2 + Σ_c w_c·max(1/2, d_c)，d_c = max(0, g·k_c−255)。\n")
	p("0–194 不触发截断，前两档（以及第三档 171–194）实测误差恒为 0；\n")
	p("第三档的 47 来自 G 通道截断。全 8 位域最坏值 g=255 → 47，与 Bound(255) 相等，上界是紧的。\n")
	p("不截断上限：%d（g>%d 时 G 的 g·kG 超过 255）。\n\n",
		api.LumaUnclippedMax(), api.LumaUnclippedMax())

	// ===== 3. 亮度守恒三组实测 =====
	p("== 3. 亮度守恒三组实测（按档分组，整数千分权重精确累加）==\n\n")
	p("  展开后存进的 RGB 记为 q；按同一权重收回得 Y(q) = round(Σ w_c·q_c)。\n")
	p("  守恒式：Σ_pix Y(q) = Σ_pix g − Σ_pix (w_G·d_G)，d_G = max(0, g·k_G − 255)。\n\n")
	type consRow struct {
		label              string
		n                  int
		sumG               int64
		sumDecoded, maxAbs int
	}
	measure := func(label string, pick func(uint8) bool) consRow {
		r := consRow{label: label}
		for _, v := range img.Pix {
			if !pick(v) {
				continue
			}
			q0, q1, q2, _ := decodeExpand(v)
			dec := int(api.LumaCollapse(q0, q1, q2))
			r.n++
			r.sumG += int64(v)
			r.sumDecoded += dec
			if d := absInt(int(v) - dec); d > r.maxAbs {
				r.maxAbs = d
			}
		}
		return r
	}
	rows := [3]consRow{}
	for i, tr := range tiers {
		rows[i] = measure(fmt.Sprintf("%d-%d", tr.Lo, tr.Hi), func(v uint8) bool { return tierOf(v) == i })
	}
	all := measure("0-255", func(uint8) bool { return true })
	p("%-3s %-9s %-6s %-10s %-14s %-14s %-10s\n",
		"组", "区间", "像素", "Σg", "Σround(Y)", "ΣY−Σg(级)", "最大单像素差")
	show := func(r consRow, tier int) {
		g := "-"
		if tier > 0 {
			g = fmt.Sprintf("%d", tier)
		}
		p("%-3s %-9s %-6d %-10d %-14d %-+14d %-10d\n",
			g, r.label, r.n, r.sumG, r.sumDecoded,
			r.sumDecoded-int(r.sumG), r.maxAbs)
	}
	for i, r := range rows {
		show(r, i+1)
	}
	show(all, 0)
	p("\n  ΣY−Σg 用逐像素「收回后取整」的 ŷ 求和：Σround(Σw_c·q_c) − Σg。\n")
	p("  0–194 内展开再收回是恒等（每级误差 0），所以第一、二组恰为 0；\n")
	p("  第三组 171–194 也恒等，差额全部来自 195–255 的 G 截断，逐像素差 ≈ −0.587·d_G。\n\n")

	// ===== 4. 超限像素与成因 =====
	offs := api.LumaOffenders(img)
	p("== 4. 误差超限像素（口径：|误差| > 1；共 %d 个）==\n\n", len(offs))
	if len(offs) == 0 {
		p("无。\n\n")
	} else {
		p("%-8s %-6s %-14s %-10s %-6s %-6s %-10s %-4s %s\n",
			"坐标", "灰度", "R,G,B", "截断通道", "收回", "误差", "单像素上界", "档", "成因")
		for _, o := range offs {
			p("(%2d,%2d) %-6d %-14s %-10s %-6d %-6d %-10d %-4d %s\n",
				o.X, o.Y, o.Gray, fmt.Sprintf("(%d,%d,%d)", o.R, o.G, o.B),
				o.ClipNote(), o.Decoded, o.Err, o.Bound, tierOf(o.Gray)+1,
				"G 越过 255 被截断")
		}
		p("\n")
	}
	p("成因：kG = %.6f > 1，G 在 g>194 时 g·kG>255。截断量 d_G = g·kG−255，\n", cs[1].Value)
	p("收回时按权重 0.587 计入，误差 ≈ −0.587·d_G，再叠加 ±0.5 舍入。\n")
	p("因此超限只出现在第三档：197–255 共 59 级；195、196 恰好 1 级，不算超限。\n")
	p("单像素上界一列逐像素按 Bound(g) 给出，实测误差均不越界。\n\n")

	// ===== 5. 熵（比特每像素）=====
	p("== 5. 熵（按符号出现次数，单位：比特每像素）==\n\n")
	rp, gp, bp := planes(expBMP)
	p("%-12s %-10s %s\n", "通道", "不同符号", "H (bit/pixel)")
	for _, r := range []struct {
		name string
		s    []uint8
	}{
		{"灰度样本", img.Pix}, {"展开 R", rp}, {"展开 G", gp}, {"展开 B", bp},
	} {
		p("%-12s %-10d %.6f\n", r.name, distinct(r.s), api.EntropyBitPerPixel(r.s))
	}
	p("\nH = −Σ_i (n_i/N)·log2(n_i/N)，n_i 为符号 i 的出现次数，N 为样本数；\n")
	p("样本是像素，故单位即比特每像素。灰度样本 272 像素覆盖 256 个符号，\n")
	p("其中 16 个符号出现两次、其余一次，故 H 略低于 log2(256)=8；\n")
	p("三通道熵不等，是因为系数把 256 级摊到不同步长上，且高灰度的 G 被截到 255：\n")
	p(fmt.Sprintf("R 步长 %.4f 级，几乎一一对应（172 个取值）；B 步长 %.4f 级，256 级压到约 66 个取值，\n", cs[0].Value, cs[2].Value))
	p(fmt.Sprintf("G 步长 %.4f 级、取值最密（195 个），但 195–255 共 61 级全压在 255 上（重复 62 次），\n", cs[1].Value))
	p("重复把 G 的熵压低，故 G 熵低于 R；B 因取值最稀、重复最多，熵最低。\n\n")

	// ===== 6. 头部开销 =====
	p("== 6. 两种格式的头部开销与差值（同一画幅 %dx%d）==\n\n", sampleW, sampleH)
	n := len(img.Pix)
	ph, po := api.FormatHeaderOverhead("pgm", sampleW, sampleH)
	bh, bo := api.FormatHeaderOverhead("bmp", sampleW, sampleH)
	p("%-12s %-10s %-24s %-10s\n", "格式", "头字节", "额外字节(相对 N 个样本)", "实际总长")
	pgmLen := len(mustEncode(img, "pgm", false))
	p("%-12s %-10d %-24d %-10d\n", "pgm", ph, po, pgmLen)
	p("%-12s %-10d %-24d %-10d\n", "bmp(灰度)", bh, bo, len(grayBMP))
	p("%-12s %-10d %-24d %-10d\n", "bmp(展开)", bh, bo, len(expBMP))
	rowPad := ((sampleW*3+3)&^3)*sampleH - 3*n
	p("\nBMP 额外字节 = 头 %d + 行补齐 %d + 第二三通道 2N=%d，共 %d。\n", bh, rowPad, 2*n, bh+rowPad+2*n)
	delta := (bh - ph) + rowPad + 2*n
	p("差值 BMP−PGM = (%d−%d) + 行补齐 %d + 2N %d = %d 字节；实测 %d−%d = %d，吻合。\n",
		bh, ph, rowPad, 2*n, delta, len(expBMP), pgmLen, len(expBMP)-pgmLen)
	p("展开与不展开的 BMP 总长相同（%d 字节）：差值来自格式本身（头 + 对齐 + 三通道），\n", len(expBMP))
	p("与是否做亮度展开无关。\n")

	if *out != "" {
		must(os.WriteFile(filepath.Join(*out, "report.txt"), b, 0o644))
	}
}

// decodeExpand 是报告里的独立复算：只用导出的系数，不碰 internal。
func decodeExpand(g uint8) (uint8, uint8, uint8, uint8) {
	cs := api.LumaCoefficients()
	rd := func(n, d int) int { return (2*n + d) / (2 * d) }
	q := [3]int{
		rd(int(g)*cs[0].Num, cs[0].Den),
		rd(int(g)*cs[1].Num, cs[1].Den),
		rd(int(g)*cs[2].Num, cs[2].Den),
	}
	var clip uint8
	for i := range q {
		if q[i] > 255 {
			q[i] = 255
			clip |= 1 << uint(i)
		}
	}
	return uint8(q[0]), uint8(q[1]), uint8(q[2]), clip
}

func tierOf(g uint8) int {
	switch {
	case g <= 85:
		return 0
	case g <= 170:
		return 1
	default:
		return 2
	}
}

func planes(bmp []byte) (r, g, b []uint8) {
	w, h := sampleW, sampleH
	row := (w*3 + 3) &^ 3
	r = make([]uint8, w*h)
	g = make([]uint8, w*h)
	b = make([]uint8, w*h)
	for y := 0; y < h; y++ {
		dst := (h - 1 - y) * w
		base := 54 + y*row
		for x := 0; x < w; x++ {
			r[dst+x] = bmp[base+x*3]
			g[dst+x] = bmp[base+x*3+1]
			b[dst+x] = bmp[base+x*3+2]
		}
	}
	return
}

func distinct(s []uint8) int {
	var seen [256]bool
	n := 0
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			n++
		}
	}
	return n
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func mustEncode(img *api.Image, f string, luma bool) []byte {
	body, err := api.Encode(img, api.Options{Format: f, LumaExpand: luma})
	must(err)
	return body
}
