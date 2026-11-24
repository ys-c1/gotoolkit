package dither

import "fmt"

// ReportRow 是一种矩阵在报告中的一行结果。
type ReportRow struct {
	Name    string
	Matrix  [Size * Size]int
	Metrics Metrics
}

// BuildReport 计算三种矩阵的全部指标，按固定顺序返回。
func BuildReport() []ReportRow {
	suite, w, h := BuildTestSuite()
	names := []struct {
		key, label string
	}{
		{NameBayer, "bayer（原矩阵，保留可选）"},
		{NameVoidCluster, "void-cluster（蓝噪声）"},
		{NameSpectral, "spectral（频谱优化）"},
	}
	rows := make([]ReportRow, 0, len(names))
	for _, n := range names {
		m, _ := Matrix(n.key)
		rows = append(rows, ReportRow{
			Name:    n.label,
			Matrix:  m,
			Metrics: Evaluate(m, suite, w, h),
		})
	}
	return rows
}

// FormatReport 把报告渲染成对齐的纯文本表格。
func FormatReport(rows []ReportRow) string {
	out := "矩阵                         低频泄漏(r<=2,12项)   宽泄漏(r<=sqrt5)      PAR峰均比   各向异性    二值低频      MSE        MAE\n"
	for _, r := range rows {
		mt := r.Metrics
		out += fmt.Sprintf(
			"%-28s %18.3f %16.3f %11.3f %10.4f %10.3f %9.3f %9.3f\n",
			r.Name, mt.LowFreqLeak, mt.LowFreqWide, mt.PAR, mt.Anisotropy,
			mt.BinaryLF, mt.MSE, mt.MAE)
	}
	return out
}
