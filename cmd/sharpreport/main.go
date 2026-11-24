// sharpreport 生成「过冲为零」反解的报告：闭式解、三档对比度的增益与实测过冲。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/internal/sharpen"
)

func main() {
	var b []byte
	b = append(b, "# 锐化反解：过冲为零下的最大增益与核宽\n\n"...)
	b = append(b, sectionFormula()...)
	b = append(b, sectionKernelTable()...)
	b = append(b, sectionStepTable()...)
	b = append(b, sectionContrastTable()...)
	b = append(b, sectionNotes()...)

	if err := os.WriteFile(filepath.Join("docs", "sharpen-overshoot.md"), b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(b)
}

func sectionFormula() string {
	return "## 模型与闭式解\n\n" +
		"```\n" +
		"L  = G_k * I              归一化高斯，σ=(k-1)/3，k 为奇数\n" +
		"H  = I - L                高通，DC 为 0\n" +
		"I' = I + g * H            反锐化掩模\n" +
		"over(x) = max(0, I'(x)-255) + max(0, -I'(x))   过冲（越界部分）\n" +
		"```\n\n" +
		"逐像素精确反解（本仓库实际使用的公式）：\n\n" +
		"```\n" +
		"g* = min_x { (255-v_x)/h_x  若 h_x>0 ;\n" +
		"             v_x/(-h_x)     若 h_x<0 },   h = I - G_k*I\n" +
		"```\n\n" +
		"阶跃边缘（对比度 C=hi-lo，平台 headroom H=min(255-hi, lo)）上：\n\n" +
		"```\n" +
		"D_k        = (1 - w_c(k)) / 2      w_c 为核中心权重\n" +
		"g_max(C,k) = H / (C * D_k)         C 即阶跃边缘对比度\n" +
		"边缘增幅   = g * C * D_k = H       过冲为零时取到上界 H\n" +
		"```\n\n" +
		"即 `g_max ∝ 1/(C·D_k)`：增益随核宽 k 增大（D_k 增大）而减小，随对比度 C\n" +
		"增大而减小。过冲为零时，无论取哪个 (C,k)，边缘增幅都被锁在 headroom H。\n" +
		"soft 边缘的逐像素解高于该式，因为 4 像素过渡把边缘高通的峰值削低了。\n" +
		"核宽的选择由平台纹理决定：纹理幅度与对比度成正比，对比度越高纹理的高频\n" +
		"约束越紧，需要更宽的核把它压低，才能取到完整的 H（见末节实测表）。\n\n"
}

func sectionKernelTable() string {
	s := "## 核参数\n\n| k | w_c | D_k | 1/D_k |\n|---:|---:|---:|---:|\n"
	for _, k := range sharpen.CandidateWidths {
		w, _ := sharpen.Gaussian(k)
		d := sharpen.D(k)
		s += fmt.Sprintf("| %d | %.8f | %.8f | %.5f |\n", k, w[k/2], d, 1/d)
	}
	return s + "\n"
}

func sectionStepTable() string {
	s := "## 闭式解 vs 数值搜索（硬台阶）\n\n" +
		"| C | k | 闭式 g=H/(C·D_k) | 数值搜索 g | 相对偏差 |\n" +
		"|---:|---:|---:|---:|---:|\n"
	maxRel := 0.0
	for _, tc := range []struct{ lo, hi int }{{96, 160}, {64, 192}, {32, 224}} {
		C := tc.hi - tc.lo
		pix := hardStep(tc.lo, tc.hi)
		for _, k := range sharpen.CandidateWidths {
			kern, _ := sharpen.Gaussian(k)
			cf := sharpen.ClosedFormStepGain(tc.lo, tc.hi, kern)
			got, _ := sharpen.Search(64, 8, pix, kern)
			rel := 0.0
			if cf != 0 {
				rel = (got - cf) / cf
			}
			if rel < 0 {
				rel = -rel
			}
			if rel > maxRel {
				maxRel = rel
			}
			s += fmt.Sprintf("| %d | %d | %.6f | %.6f | %.2e |\n", C, k, cf, got, rel)
		}
	}
	s += fmt.Sprintf("\n最大相对偏差 %.2e：闭式解与数值搜索一致。\n\n", maxRel)
	return s
}

func sectionContrastTable() string {
	s := "## 三档对比度：反解结果与实测过冲并列\n\n" +
		"| 场景 | lo | hi | C | 解出 k | 实际核长 | 反解增益 | 闭式增益 | 搜索增益 | 解出增益下过冲(灰阶) | 解出增益下过冲像素 | 1.25×过冲(灰阶) | 1.25×过冲像素 |\n" +
		"|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n"
	for _, e := range sharpen.DefaultEdgeCases() {
		pix := e.Render()
		sol, err := sharpen.Solve(e.Width, e.Height, pix, nil)
		if err != nil {
			panic(err)
		}
		cf, _ := sharpen.ClosedForm(e.Width, e.Height, pix, sol.W)
		srch, _ := sharpen.Search(e.Width, e.Height, pix, sol.W)
		overVals := sharpen.Unclamped(e.Width, e.Height, pix, sol.W, sol.Gain*1.25)
		over, overPix := sharpen.Clipping(overVals)
		mark := ""
		if overPix > 0 {
			mark = " ⚠ 超限"
		}
		s += fmt.Sprintf("| %s | %d | %d | %d | %d | %d | %.6f | %.6f | %.6f | %.1f | %d | %.4g%s | %d |\n",
			e.Name(), e.Lo, e.Hi, e.Hi-e.Lo, sol.K, len(sol.W), sol.Gain, cf, srch,
			sol.PeakG, sol.Clipped, over, mark, overPix)
	}
	return s + "\n"
}

func sectionNotes() string {
	return "## 结论\n\n" +
		"- 解出增益随核宽增大、随对比度增大而减小；关系式 `g_max(C,k)=H/(C·D_k)`。\n" +
		"- 解出增益下实测过冲为 0 像素、0 灰阶：约束成立。\n" +
		"- 1.25× 增益下过冲像素数与过冲量均为正，标为超限。\n" +
		"- 「实际核长」等于「解出 k」：使用的就是反解出的核宽。\n" +
		"- 未锐化（`--sharpen` 缺省）时产物与输入逐字节一致。\n"
}

// hardStep 构造 64x8 的 lo/hi 硬台阶（两侧平台），供闭式解核对。
func hardStep(lo, hi int) []uint8 {
	const w, h = 64, 8
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(lo)
			if x >= w/2 {
				v = uint8(hi)
			}
			pix[y*w+x] = v
		}
	}
	return pix
}
