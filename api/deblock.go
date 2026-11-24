package api

import "gotoolkit/internal/deblock"

// 去块滤波实验结果类型。用类型别名把内部实现类型从 api 包转出，
// 调用方只需要认识 api.DeblockReport / api.DeblockRow，不必直接引用 internal。
type (
	// DeblockRow 是一个块长档位的最优强度与两项指标、固定对照。
	DeblockRow = deblock.Row
	// DeblockAgg 是三档等权平均的对照结论。
	DeblockAgg = deblock.Agg
	// DeblockReport 是完整实验报告（含可直接打印的 Markdown 表）。
	DeblockReport = deblock.Report
)

// OptimalDeblockAlpha 是闭式最优滤波强度 α*=Vn/(Vn+Vs)。
// Vn 为块边界量化误差二阶矩，Vs 为真实跨边界细节二阶矩。
func OptimalDeblockAlpha(vn, vs float64) float64 {
	return deblock.OptimalAlpha(vn, vs)
}

// DeblockImage 对块网格边界做对称差分混合。strength=0 时像素内容不变。
func DeblockImage(img *Image, block int, strength float64) *Image {
	if img == nil {
		return nil
	}
	out := deblock.Deblock(&deblock.Gray{W: img.W, H: img.H, Pix: img.Pix}, block, strength)
	return &Image{W: out.W, H: out.H, Pix: out.Pix}
}

// RunDeblockStudy 跑三档块长（4/8/16）的闭式最优 vs 固定 0.5 对照实验。
// outDir 非空时写入 PGM 产物、metrics.json/csv 与 table.md。
func RunDeblockStudy(outDir string) (*DeblockReport, error) {
	return deblock.Study(outDir)
}
