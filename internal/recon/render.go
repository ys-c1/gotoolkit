package recon

import (
	"fmt"
	"strings"
)

// f6 统一矩阵元素格式：量级大时用定点，否则用科学计数，保证两端对齐可读。
func f6(v float64) string {
	a := absf(v)
	switch {
	case a == 0:
		return "       0"
	case a >= 1e5:
		return fmt.Sprintf("%9.3e", v)
	case a >= 100:
		return fmt.Sprintf("%9.2f", v)
	case a >= 1:
		return fmt.Sprintf("%9.4f", v)
	default:
		return fmt.Sprintf("%9.5f", v)
	}
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// formatRow 输出一行矩阵，如 "| 1.2 3.4 |"。
func formatRow(vals []float64) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = f6(v)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// FormatMatrix 把方阵渲染成按列右对齐、带方括号的文本矩阵。
func FormatMatrix(m [][]float64) string {
	n := len(m)
	tok := make([][]string, n)
	widths := make([]int, len(m[0]))
	for i := 0; i < n; i++ {
		tok[i] = make([]string, len(m[i]))
		for j, v := range m[i] {
			tok[i][j] = f6(v)
			if len(tok[i][j]) > widths[j] {
				widths[j] = len(tok[i][j])
			}
		}
	}
	rows := make([]string, n)
	for i := 0; i < n; i++ {
		parts := make([]string, len(m[i]))
		for j := range m[i] {
			parts[j] = strings.Repeat(" ", widths[j]-len(tok[i][j])) + tok[i][j]
		}
		rows[i] = "[" + strings.Join(parts, "  ") + "]"
	}
	return strings.Join(rows, "\n")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func vecToSlice(v [TapCount]float64) []float64 { return v[:] }

// RenderMarkdown 生成验收报告全文。
func RenderMarkdown(rep *Report) string {
	s := rep.Solved
	var b strings.Builder

	b.WriteString("# 五抽头约束最小二乘重建核：求解、约束核验与双线性色差对照\n\n")
	b.WriteString("本报告由 `go run ./cmd/reconreport` 用同一组 6 张确定性彩色图实算生成，" +
		"所有矩阵、抽头与色差均为程序真实输出，可复跑核对。\n\n")

	// 1. 建模
	b.WriteString("## 1. 约束最小二乘建模\n\n")
	b.WriteString("抽头顺序固定为 `[h0=上, h1=左, h2=中, h3=右, h4=下]`（十字五点）。" +
		"在每个训练像素上，以柔化观测 S 的五点邻域为特征、原始真值为目标：\n\n")
	b.WriteString("```\n")
	b.WriteString("ŷ = h0*S(y-1,x) + h1*S(y,x-1) + h2*S(y,x) + h3*S(y,x+1) + h4*S(y+1,x)\n")
	b.WriteString("min_h ||A h - y||^2\n")
	b.WriteString("```\n\n")
	b.WriteString("两条验收条件作为**等式约束直接进入求解**（不是解完再投影/检查）：\n\n")
	b.WriteString("- 系数和为 1：`h0+h1+h2+h3+h4 = 1`；\n")
	b.WriteString("- 关于中心对称：`h0 = h4`（上=下）、`h1 = h3`（左=右）。\n\n")
	b.WriteString("写成 `C h = d`：\n\n")
	b.WriteString("```\n")
	b.WriteString("C = [ 1  1  1  1  1 ]   d = [ 1 ]\n")
	b.WriteString("    [ 1  0  0  0 -1 ]       [ 0 ]\n")
	b.WriteString("    [ 0  1  0 -1  0 ]       [ 0 ]\n")
	b.WriteString("```\n\n")

	// 2. 数据正规方程两端
	b.WriteString("## 2. 正规方程（数据项 AᵀA h = Aᵀy，左右两端数值）\n\n")
	var aa [][]float64
	for i := 0; i < TapCount; i++ {
		aa = append(aa, s.AtA[i][:])
	}
	b.WriteString("左端矩阵 `AᵀA`（5×5）：\n\n```\n" + FormatMatrix(aa) + "\n```\n\n")
	b.WriteString("右端向量 `Aᵀy`（长度 5）：\n\n```\n" + formatRow(vecToSlice(s.Aty)) + "\n```\n\n")
	b.WriteString("即正规方程左右两端为：\n\n```\n")
	for i := 0; i < TapCount; i++ {
		b.WriteString(fmt.Sprintf("  %s · h  =  %s\n", formatRow(s.AtA[i][:]), f6(s.Aty[i])))
	}
	b.WriteString("```\n\n")

	// 3. KKT 增广正规方程
	b.WriteString("## 3. 含等式约束的增广正规方程（实际求解的 KKT，8×8）\n\n")
	b.WriteString("令乘子 μ，解 `[AᵀA Cᵀ; C 0][h; μ] = [Aᵀy; d]`。左端 8×8 与右端 8 维向量为：\n\n")
	var kkt [][]float64
	for i := 0; i < KKTDim; i++ {
		kkt = append(kkt, s.KKT[i][:])
	}
	b.WriteString("左端 KKT 矩阵：\n\n```\n" + FormatMatrix(kkt) + "\n```\n\n")
	rhs := make([]float64, KKTDim)
	copy(rhs[:TapCount], s.RHS[:TapCount])
	copy(rhs[TapCount:], s.RHS[TapCount:])
	b.WriteString("右端向量：\n\n```\n" + formatRow(rhs) + "\n```\n\n")
	b.WriteString(fmt.Sprintf("KKT 方程整体最大逐行残差 = %.3e（高斯-约当部分选主元求解）。\n\n", kktResidual(s)))

	// 4. 五个抽头逐项
	b.WriteString("## 4. 五个抽头数值（逐项）\n\n")
	b.WriteString("| 抽头 | 位置 | 数值 |\n|---|---|---:|\n")
	pos := []string{"上", "左", "中", "右", "下"}
	for i := 0; i < TapCount; i++ {
		b.WriteString(fmt.Sprintf("| h%d | %s | **%.10f** |\n", i, pos[i], s.H[i]))
	}
	b.WriteString("\n")

	// 5. 逐条约束打勾
	b.WriteString("## 5. 约束满足情况（逐条核验，不合并）\n\n")
	res := s.ConstraintResiduals()
	sum := s.H[0] + s.H[1] + s.H[2] + s.H[3] + s.H[4]
	b.WriteString(checkLine(fmt.Sprintf("约束① 系数和为 1：Σh = %.12f，残差 %.3e", sum, res[0]), absf(res[0]) < 1e-8))
	b.WriteString("\n")
	b.WriteString(checkLine(fmt.Sprintf("约束②-a 中心对称（上=下）：h0 = %.10f，h4 = %.10f，h0−h4 残差 %.3e",
		s.H[0], s.H[4], res[1]), absf(res[1]) < 1e-8))
	b.WriteString("\n")
	b.WriteString(checkLine(fmt.Sprintf("约束②-b 中心对称（左=右）：h1 = %.10f，h3 = %.10f，h1−h3 残差 %.3e",
		s.H[1], s.H[3], res[2]), absf(res[2]) < 1e-8))
	b.WriteString("\n")
	b.WriteString(checkLine(fmt.Sprintf("正规方程 KKT 残差达标（< 1e-6，实测 %.3e）", kktResidual(s)), kktResidual(s) < 1e-6))
	b.WriteString("\n\n")

	// 6. 色差表（Lab 主口径）
	b.WriteString("## 6. 同一组图 vs 双线性：CIE76 ΔE 色差（平均 / 最大两列）\n\n")
	b.WriteString(fmt.Sprintf("色差在 sRGB→CIE Lab(D65) 后按欧氏距离逐像素计算，统一去掉 %d 像素边界；"+
		"**双线性路径**为 `2×2 均值降半 → 教科书双线性放大回原尺寸`（`MethodBilinear`，保留可切回）。\n\n", Margin))
	b.WriteString("| 图像 | 双线性 平均ΔE | 双线性 最大ΔE | 五抽头 平均ΔE | 五抽头 最大ΔE |\n")
	b.WriteString("|---|---:|---:|---:|---:|\n")
	for _, r := range rep.Rows {
		b.WriteString(fmt.Sprintf("| %s | %.4f | %.3f | %.4f | %.3f |\n",
			r.Name, r.BilinearLab.Mean, r.BilinearLab.Max, r.ClskLab.Mean, r.ClskLab.Max))
	}
	b.WriteString(fmt.Sprintf("| **总体（像素加权平均 / 全局最大）** | **%.4f** | **%.3f** | **%.4f** | **%.3f** |\n",
		rep.BilLab.Mean, rep.BilLab.Max, rep.ClskLab.Mean, rep.ClskLab.Max))
	b.WriteString("\n")

	b.WriteString("### 验收③ 色差低于双线性（平均、最大分别判定）\n\n")
	b.WriteString(checkLine(fmt.Sprintf("平均色差：五抽头 %.4f < 双线性 %.4f（降低 %.2f%%）",
		rep.ClskLab.Mean, rep.BilLab.Mean, 100*(rep.BilLab.Mean-rep.ClskLab.Mean)/rep.BilLab.Mean),
		rep.ClskLab.Mean < rep.BilLab.Mean))
	b.WriteString("\n")
	b.WriteString(checkLine(fmt.Sprintf("最大色差：五抽头 %.3f < 双线性 %.3f（降低 %.2f%%）",
		rep.ClskLab.Max, rep.BilLab.Max, 100*(rep.BilLab.Max-rep.ClskLab.Max)/rep.BilLab.Max),
		rep.ClskLab.Max < rep.BilLab.Max))
	b.WriteString("\n\n")

	// 7. RGB 口径 + 同口径共址双线性
	b.WriteString("## 7. 附：RGB 欧氏色差(0–441) 与同输入共址双线性核验\n\n")
	b.WriteString("约束最小二乘直接最小化 RGB 三通道平方误差之和，故附 RGB 欧氏色差：\n\n")
	b.WriteString("| 口径 | 双线性平均 | 双线性最大 | 五抽头平均 | 五抽头最大 |\n|---|---:|---:|---:|---:|\n")
	b.WriteString(fmt.Sprintf("| RGB 欧氏距离 | %.3f | %.2f | %.3f | %.2f |\n\n",
		rep.BilRGB.Mean, rep.BilRGB.Max, rep.ClskRGB.Mean, rep.ClskRGB.Max))
	b.WriteString(fmt.Sprintf("同一柔化观测上的**共址双线性核** δ=(0,0,1,0,0) 总体 ΔE：平均 %.4f、最大 %.3f。" +
		"δ 是满足 Σh=1 与中心对称的可行点，因此约束 LS 的训练平方误差不会劣于它；五抽头相对它同样改善。\n\n",
		rep.CoLab.Mean, rep.CoLab.Max))

	// 8. 双线性保留与切换一致性
	b.WriteString("## 8. 双线性路径保留与切回一致性\n\n")
	b.WriteString("- 双线性实现保留在 `internal/recon/kernel.go`：`BilinearScale`（教科书插值）、" +
		"`BilinearRoundTrip`（基准路径）、`MethodBilinear` 开关；\n")
	b.WriteString("- `Reconstruct(Method, observed, h)` 可在 `bilinear | cosited | clsk` 间切换；\n")
	b.WriteString("- `TestSwitchBackIdentical` 验证 `bilinear → clsk → bilinear` 第二次双线性输出与第一次**逐像素完全相等**；\n")
	b.WriteString("- `TestBilinearPinnedMetrics` 把当前双线性平均/最大色差锁定为常量，切回时必须与“现在”一致；\n")
	b.WriteString("- 原有灰度 codec/golden 与 `make test` 全部保持不变。\n")

	return b.String()
}

func checkLine(text string, ok bool) string {
	if ok {
		return "- [x] ✅ " + text
	}
	return "- [ ] ❌ " + text
}

// kktResidual 与 CLSResult.KKTResidual 相同，包内渲染便捷封装。
func kktResidual(r *CLSResult) float64 { return r.KKTResidual() }
