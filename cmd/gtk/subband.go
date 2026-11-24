package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

	"gotoolkit/api"
	"gotoolkit/internal/subband"
)

// runSubband 跑子带注水/平分对比分析，输出三档预算的逐带分配与误差。
// 用法：gtk subband --in F [--levels N] [--out DIR] [--bpp 1,2,3]
func runSubband(args []string) int {
	fs := flag.NewFlagSet("subband", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM（留空则用内置 64×64 合成图）")
	levels := fs.Int("levels", 2, "Haar 分解层数")
	outDir := fs.String("out", "", "可选：把重构 PGM 与 Markdown 报告写到该目录")
	bppFlag := fs.String("bpp", "1,2,3", "三档每像素比特预算，逗号分隔")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	w, h, pix, err := loadAnalysisImage(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	budgets, err := parseBPP(*bppFlag, w*h)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	rep, err := subband.Analyze(pix, w, h, *levels, budgets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "分析失败:", err)
		return 1
	}

	var sb strings.Builder
	writeTextReport(&sb, rep)
	fmt.Print(sb.String())

	if *outDir != "" {
		if err := writeArtifacts(*outDir, rep, pix); err != nil {
			fmt.Fprintln(os.Stderr, "写产物失败:", err)
			return 1
		}
		fmt.Printf("\n产物已写入 %s/\n", *outDir)
	}
	return 0
}

func loadAnalysisImage(path string) (int, int, []uint8, error) {
	if path == "" {
		w, h, pix := subband.SyntheticImage(64)
		return w, h, pix, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, nil, err
	}
	img, err := api.Decode(raw, api.Options{Format: "pgm"})
	if err != nil {
		return 0, 0, nil, err
	}
	return img.W, img.H, img.Pix, nil
}

func parseBPP(s string, pixels int) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		var bpp float64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%g", &bpp); err != nil {
			return nil, fmt.Errorf("bpp 解析失败: %q", p)
		}
		if bpp <= 0 {
			return nil, fmt.Errorf("bpp 必须为正: %g", bpp)
		}
		out = append(out, int(math.Round(bpp*float64(pixels))))
	}
	return out, nil
}

func writeTextReport(sb *strings.Builder, rep *subband.Report) {
	fmt.Fprintf(sb, "子带比特分配分析  图 %dx%d  分解层数 %d  子带数 %d\n",
		rep.W, rep.H, rep.Levels, rep.NBands)
	fmt.Fprintln(sb, strings.Repeat("=", 92))
	fmt.Fprintln(sb, "子带统计（按自然频率顺序）：")
	fmt.Fprintf(sb, "  %-6s %-5s %8s %9s %12s %12s\n", "带", "层", "系数N", "权重w", "方差σ²", "动态范围")
	for _, st := range rep.Stats {
		fmt.Fprintf(sb, "  %-6s L%-4d %8d %9.4f %12.4f %12.4f\n",
			st.Name, st.Level, st.N, st.Weight, st.Var, st.Range)
	}

	for _, br := range rep.Budgets {
		fmt.Fprintln(sb, strings.Repeat("-", 92))
		fmt.Fprintf(sb, "预算 B=%d 比特（%.2f bpp）\n", br.Budget, br.BitsPerPixel)
		wr := br.Water
		fmt.Fprintf(sb, "反向注水：迭代 %d 次 | 水位 θ=%.6g | 连续总比特 %.4f（预算残差 %.2e）| KKT 残差 %.2e\n",
			wr.Iterations, wr.Lambda, wr.ContinuousTotal, wr.BudgetResidual, wr.KKTResidual)
		fmt.Fprintf(sb, "活动带 %d 个：", len(wr.Active))
		for _, i := range wr.Active {
			fmt.Fprintf(sb, "%s ", wr.Details[i].Name)
		}
		fmt.Fprintln(sb)
		if wr.TruncatedBits > 0 {
			fmt.Fprintf(sb, "整数化超预算：截断 %d 比特（涉及 %d 带），截断后用量 %d ≤ B\n",
				wr.TruncatedBits, wr.TruncatedBands, wr.Used)
		} else {
			fmt.Fprintf(sb, "整数化未超预算，无需截断；实际用量 %d，回填/填充至预算上限\n", wr.Used)
		}

		fmt.Fprintln(sb, "逐带分配与实测误差：")
		fmt.Fprintf(sb, "  %-6s %-4s %8s %10s %8s %8s %12s %12s\n",
			"带", "层", "σ²", "连续b*", "注水b", "平分b", "注水MSE", "平分MSE")
		for _, r := range br.Rows {
			fmt.Fprintf(sb, "  %-6s L%-3d %8.3f %10.4f %8d %8d %12.5f %12.5f\n",
				r.Name, r.Level, r.Variance, r.Continuous, r.WaterBits, r.EqualBits,
				r.WaterMSE, r.EqualMSE)
		}
		fmt.Fprintf(sb, "整图加权 MSE：注水 %.5f | 平分 %.5f | 注水相对降幅 %.2f%%\n",
			br.WaterMSE, br.EqualMSE, 100*(1-br.WaterMSE/br.EqualMSE))
		fmt.Fprintf(sb, "PSNR(dB)：注水 %.2f | 平分 %.2f\n", br.WaterPSNR, br.EqualPSNR)
		ic := br.Inverse
		fmt.Fprintf(sb, "误差-比特验证（n=%d）：指数律 log(D/σ²)=a+sb 斜率 %.4f（理论 -ln4=-1.386）R²=%.4f；"+
			"字面反比 D/σ²~1/b R²=%.4f；Spearman(b, D/σ²)=%.4f（期望为负）\n",
			ic.SampleBands, ic.LogSlope, ic.LogR2, ic.InvR2, ic.Spearman)
	}
	fmt.Fprintln(sb, strings.Repeat("=", 92))
}

// writeArtifacts 落 Markdown 报告与每档预算两条路径的重构 PGM。
func writeArtifacts(dir string, rep *subband.Report, orig []uint8) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	md := buildMarkdown(rep)
	if err := os.WriteFile(dir+"/report.md", []byte(md), 0o644); err != nil {
		return err
	}
	origBody, err := api.Encode(&api.Image{W: rep.W, H: rep.H, Pix: orig},
		api.Options{Format: "pgm"})
	if err != nil {
		return err
	}
	if err := os.WriteFile(dir+"/original.pgm", origBody, 0o644); err != nil {
		return err
	}
	for i, br := range rep.Budgets {
		tag := fmt.Sprintf("budget%d", i+1)
		wBody, e1 := api.Encode(&api.Image{W: rep.W, H: rep.H, Pix: br.WaterRecon},
			api.Options{Format: "pgm"})
		eBody, e2 := api.Encode(&api.Image{W: rep.W, H: rep.H, Pix: br.EqualRecon},
			api.Options{Format: "pgm"})
		if e1 != nil || e2 != nil {
			return fmt.Errorf("重构 PGM 编码失败")
		}
		if err := os.WriteFile(fmt.Sprintf("%s/%s_waterfill.pgm", dir, tag), wBody, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(fmt.Sprintf("%s/%s_equal.pgm", dir, tag), eBody, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func buildMarkdown(rep *subband.Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# 子带反向注水比特分配报告\n\n")
	fmt.Fprintf(&sb, "图 %dx%d，%d 级 Haar 分解，共 %d 个子带。\n\n", rep.W, rep.H, rep.Levels, rep.NBands)
	for _, br := range rep.Budgets {
		wr := br.Water
		fmt.Fprintf(&sb, "## 预算 B=%d（%.2f bpp）\n\n", br.Budget, br.BitsPerPixel)
		fmt.Fprintf(&sb, "- 注水迭代 **%d** 次，水位 θ=%.6g，连续总比特 %.4f，预算残差 %.2e，KKT 残差 %.2e\n",
			wr.Iterations, wr.Lambda, wr.ContinuousTotal, wr.BudgetResidual, wr.KKTResidual)
		fmt.Fprintf(&sb, "- 整数化截断 %d 比特（%d 带），实际用量 %d / %d\n",
			wr.TruncatedBits, wr.TruncatedBands, wr.Used, br.Budget)
		fmt.Fprintf(&sb, "- 整图加权 MSE：注水 **%.5f** vs 平分 %.5f（降幅 %.2f%%）；PSNR %.2f vs %.2f dB\n\n",
			br.WaterMSE, br.EqualMSE, 100*(1-br.WaterMSE/br.EqualMSE), br.WaterPSNR, br.EqualPSNR)
		sb.WriteString("| 带 | 层 | 系数N | 权重 | 方差σ² | 连续b* | 注水b | 平分b | 注水MSE | 平分MSE |\n")
		sb.WriteString("|---|--|--|--|--|--|--|--|--|--|\n")
		for _, r := range br.Rows {
			fmt.Fprintf(&sb, "| %s | %d | %d | %.4f | %.4f | %.4f | %d | %d | %.5f | %.5f |\n",
				r.Name, r.Level, r.N, r.Weight, r.Variance, r.Continuous,
				r.WaterBits, r.EqualBits, r.WaterMSE, r.EqualMSE)
		}
		ic := br.Inverse
		fmt.Fprintf(&sb, "\n误差-比特：指数律斜率 %.4f（理论 -1.386）R²=%.4f；字面反比 R²=%.4f；Spearman=%.4f。\n\n",
			ic.LogSlope, ic.LogR2, ic.InvR2, ic.Spearman)
	}
	return sb.String()
}
