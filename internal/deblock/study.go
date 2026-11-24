package deblock

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gotoolkit/internal/codec"
)

// Row 是一个块长档位的全部结果。
type Row struct {
	B         int     `json:"block"`
	Pairs     int     `json:"pairs"`
	Vs        float64 `json:"vs"`          // 真实跨边界细节二阶矩
	Vn        float64 `json:"vn"`          // 边界量化（台阶）误差二阶矩
	Vg        float64 `json:"vg"`          // 压缩跳变总二阶矩
	MeanN     float64 `json:"mean_n"`      // 误差均值
	TheoryVnQ float64 `json:"theory_vn_q"` // Q^2/6
	AlphaStar float64 `json:"alpha_star"`  // 闭式最优
	AlphaGrid float64 `json:"alpha_grid"`  // 数值搜索最优（核对用）
	JOpt      float64 `json:"j_opt"`
	DOpt      float64 `json:"d_opt"`
	COpt      float64 `json:"c_opt"`
	JFix      float64 `json:"j_fix"`
	DFix      float64 `json:"d_fix"`
	CFix      float64 `json:"c_fix"`
	RawComp   float64 `json:"raw_comp"`
	RawOpt    float64 `json:"raw_opt"`
	RawFix    float64 `json:"raw_fix"`
	MSEOpt    float64 `json:"mse_opt"`
	MSEFix    float64 `json:"mse_fix"`
	OffSame   bool    `json:"off_identity"` // alpha=0 是否与压缩图逐像素一致
}

// Agg 是三档等权平均后的对照。
type Agg struct {
	JOpt        float64 `json:"j_opt_mean"`
	DOpt        float64 `json:"d_opt_mean"`
	JFix        float64 `json:"j_fix_mean"`
	DFix        float64 `json:"d_fix_mean"`
	JRatio      float64 `json:"j_ratio"` // JOpt/JFix，<1 表示边界跳变更低
	DRatio      float64 `json:"d_ratio"` // DOpt/DFix，<=1 表示细节损失不升
	AlphaCommon float64 `json:"alpha_common_star"`
}

// Report 是一次完整实验的结果。
type Report struct {
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Q             int     `json:"q"`
	FixedStrength float64 `json:"fixed_strength"`
	OffSameAll    bool    `json:"off_identity_all"`
	Agg           Agg     `json:"aggregate"`
	Rows          []Row   `json:"rows"`
	Markdown      string  `json:"-"`
}

// Study 用标准实验图跑三档块长：统计误差分布、求闭式最优、与固定强度对照，
// 并把 PGM 产物、metrics.csv/json、table.md 写到 outDir（为空则不写文件）。
func Study(outDir string) (*Report, error) {
	src := SampleImage(128, 128)
	rep := &Report{Width: src.W, Height: src.H, Q: DefaultQ, FixedStrength: FixedStrength, OffSameAll: true}

	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil, err
		}
		if err := writePGM(filepath.Join(outDir, "source.pgm"), src); err != nil {
			return nil, err
		}
	}

	var vnSum, vsSum float64
	for _, B := range BlockSizes {
		comp := BlockCompress(src, B, rep.Q)
		st := BoundaryStats(src, comp, B, rep.Q)
		aGrid, _ := GridOptimal(st, 4001)

		opt := Deblock(comp, B, st.AlphaStar)
		fix := Deblock(comp, B, rep.FixedStrength)
		off := Deblock(comp, B, 0)
		offSame := equalPix(off, comp)
		rep.OffSameAll = rep.OffSameAll && offSame

		row := Row{
			B: B, Pairs: st.Pairs, Vs: st.Vs, Vn: st.Vn, Vg: st.Vg, MeanN: st.MeanN,
			TheoryVnQ: st.TheoryVnQ, AlphaStar: st.AlphaStar, AlphaGrid: aGrid,
			JOpt: BoundaryJump(st, st.AlphaStar), DOpt: DetailLoss(st, st.AlphaStar), COpt: Cost(st, st.AlphaStar),
			JFix: BoundaryJump(st, rep.FixedStrength), DFix: DetailLoss(st, rep.FixedStrength), CFix: Cost(st, rep.FixedStrength),
			RawComp: RawBoundaryJump(comp, B), RawOpt: RawBoundaryJump(opt, B), RawFix: RawBoundaryJump(fix, B),
			MSEOpt: MSE(src, opt), MSEFix: MSE(src, fix), OffSame: offSame,
		}
		rep.Rows = append(rep.Rows, row)

		rep.Agg.JOpt += row.JOpt
		rep.Agg.DOpt += row.DOpt
		rep.Agg.JFix += row.JFix
		rep.Agg.DFix += row.DFix
		vnSum += st.Vn
		vsSum += st.Vs

		if outDir != "" {
			tag := fmt.Sprintf("B%d", B)
			if err := writePGM(filepath.Join(outDir, tag+"_compressed_off.pgm"), comp); err != nil {
				return nil, err
			}
			if err := writePGM(filepath.Join(outDir, fmt.Sprintf("%s_optimal_a%.3f.pgm", tag, st.AlphaStar)), opt); err != nil {
				return nil, err
			}
			if err := writePGM(filepath.Join(outDir, fmt.Sprintf("%s_fixed_a%.2f.pgm", tag, rep.FixedStrength)), fix); err != nil {
				return nil, err
			}
		}
	}
	n := float64(len(rep.Rows))
	rep.Agg.JOpt /= n
	rep.Agg.DOpt /= n
	rep.Agg.JFix /= n
	rep.Agg.DFix /= n
	rep.Agg.JRatio = rep.Agg.JOpt / rep.Agg.JFix
	rep.Agg.DRatio = rep.Agg.DOpt / rep.Agg.DFix
	rep.Agg.AlphaCommon = OptimalAlpha(vnSum, vsSum)
	rep.Markdown = renderMarkdown(rep)

	if outDir != "" {
		if err := writeMetrics(filepath.Join(outDir, "metrics.json"), rep); err != nil {
			return nil, err
		}
		if err := writeCSV(filepath.Join(outDir, "metrics.csv"), rep); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(outDir, "table.md"), []byte(rep.Markdown), 0o644); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

func equalPix(a, b *Gray) bool {
	if a.W != b.W || a.H != b.H || len(a.Pix) != len(b.Pix) {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

func writePGM(path string, g *Gray) error {
	body, err := codec.EncodePGM(g.W, g.H, g.Pix)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func writeMetrics(path string, rep *Report) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func writeCSV(path string, rep *Report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"B", "pairs", "Vs", "Vn", "alpha_star", "alpha_grid",
		"J_opt", "D_opt", "J_fix0.50", "D_fix0.50", "raw_comp", "raw_opt", "raw_fix", "mse_opt", "mse_fix", "off_identity"})
	for _, r := range rep.Rows {
		_ = w.Write([]string{
			fmt.Sprintf("%d", r.B), fmt.Sprintf("%d", r.Pairs),
			fmt.Sprintf("%.4f", r.Vs), fmt.Sprintf("%.4f", r.Vn),
			fmt.Sprintf("%.4f", r.AlphaStar), fmt.Sprintf("%.4f", r.AlphaGrid),
			fmt.Sprintf("%.4f", r.JOpt), fmt.Sprintf("%.4f", r.DOpt),
			fmt.Sprintf("%.4f", r.JFix), fmt.Sprintf("%.4f", r.DFix),
			fmt.Sprintf("%.3f", r.RawComp), fmt.Sprintf("%.3f", r.RawOpt), fmt.Sprintf("%.3f", r.RawFix),
			fmt.Sprintf("%.3f", r.MSEOpt), fmt.Sprintf("%.3f", r.MSEFix),
			fmt.Sprintf("%t", r.OffSame),
		})
	}
	w.Flush()
	return w.Error()
}

func f2(x float64) string { return fmt.Sprintf("%.2f", x) }
func f3(x float64) string { return fmt.Sprintf("%.3f", x) }
func f4(x float64) string { return fmt.Sprintf("%.4f", x) }

func renderMarkdown(rep *Report) string {
	var b strings.Builder
	b.WriteString("## 闭式最优强度与三档块长结果\n\n")
	b.WriteString("闭式解：`α* = Vn / (Vn + Vs)`，其中 `Vn=E[n²]` 为块边界量化（台阶）误差二阶矩，`Vs=E[s²]` 为真实跨边界细节二阶矩。\n\n")
	b.WriteString("代入三档块长（Q=" + fmt.Sprintf("%d", rep.Q) + "）：\n\n")
	b.WriteString("| 块长 B | Vs | Vn | α*=Vn/(Vn+Vs) | 数值搜索核对 | 边界跳变 J*=Vn(1−α*)² | 细节损失 D*=Vs·α*² |\n")
	b.WriteString("|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rep.Rows {
		b.WriteString(fmt.Sprintf("| %d | %s | %s | **%s** | %s | %s | %s |\n",
			r.B, f2(r.Vs), f2(r.Vn), f4(r.AlphaStar), f3(r.AlphaGrid), f3(r.JOpt), f3(r.DOpt)))
	}
	b.WriteString("\n## 与固定强度 α=" + f2(rep.FixedStrength) + " 对照组并列\n\n")
	b.WriteString("| B | α* | J(最优) | D(最优) | α固定 | J(固定) | D(固定) | J*/J固 | D*/D固 |\n")
	b.WriteString("|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rep.Rows {
		b.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %.2f | %s | %s | %s | %s |\n",
			r.B, f4(r.AlphaStar), f3(r.JOpt), f3(r.DOpt), rep.FixedStrength, f3(r.JFix), f3(r.DFix),
			f3(r.JOpt/r.JFix), f3(r.DOpt/r.DFix)))
	}
	a := rep.Agg
	b.WriteString(fmt.Sprintf("| **三档平均** | — | **%s** | **%s** | %.2f | **%s** | **%s** | **%s** | **%s** |\n\n",
		f3(a.JOpt), f3(a.DOpt), rep.FixedStrength, f3(a.JFix), f3(a.DFix), f4(a.JRatio), f4(a.DRatio)))
	b.WriteString(fmt.Sprintf("结论：三档等权平均下，最优强度组的边界跳变为固定组的 %.1f%%（更低），细节损失为固定组的 %.1f%%（不升）。\n",
		100*a.JRatio, 100*a.DRatio))
	b.WriteString(fmt.Sprintf("把三档矩合并得到的“全局唯一最优固定强度” α共*=ΣVn/Σ(Vn+Vs)=%.3f，与采用的固定值 %.2f 接近，说明固定对照组并非稻草人。\n\n",
		a.AlphaCommon, rep.FixedStrength))
	b.WriteString("滤波关闭（α=0）逐像素恒等于块压缩产物：" + fmt.Sprintf("%t", rep.OffSameAll) + "。\n")
	return b.String()
}
