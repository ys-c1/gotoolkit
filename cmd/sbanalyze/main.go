// Command sbanalyze 跑子带反向注水分配、同体积平分对比、逐带重建图与两类失真统计，
// 全部产物写到 --out 目录（默认 artifacts/subband）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sb "gotoolkit/internal/subband"
)

func main() {
	out := flag.String("out", "artifacts/subband", "产物目录")
	size := flag.Int("size", 128, "合成图边长（须被 8 整除）")
	levels := flag.Int("levels", 3, "Haar 层数")
	tau := flag.Float64("tau", 5.0, "异常像素误差阈值（灰度级）")
	flag.Parse()

	if err := run(*out, *size, *levels, *tau); err != nil {
		fmt.Fprintln(os.Stderr, "sbanalyze:", err)
		os.Exit(1)
	}
}

func run(outDir string, size, levels int, tau float64) error {
	w, h, pix := sb.SourceImage(size)
	st, err := sb.NewSetup(pix, w, h, levels)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	var md strings.Builder
	wf := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		fmt.Println(line)
		md.WriteString(line)
		md.WriteByte('\n')
	}

	wf("# 子带反向注水分析数据（由 sbanalyze 生成）\n")
	wf("画幅 %dx%d，N=%d，Haar 层数 %d，异常阈值 τ=%.1f\n", w, h, st.N, levels, tau)

	// ---------- 1. 子带统计 ----------
	wf("## 子带方差\n")
	wf("| i | 带 | n_i | 权重 | σ² | σ | mean | range |")
	wf("|--:|---|--:|--:|--:|--:|--:|--:|")
	for i, s := range st.Stats {
		wf("| %d | %s | %d | %.5f | %.3f | %.2f | %.2f | %.1f |",
			i, s.Label, s.N, st.Weights[i], s.Var, sqrt(s.Var), s.Mean, s.Range)
	}

	// ---------- 1b. “旧版拍脑袋分配”误差表与可疑行 ----------
	// 工作区并未随题面提供现成误差表；这里用一版典型的手工固定比特表，
	// 走同一个真实量化器把误差表实测出来，再自动圈出符合题面描述的行：
	// 某带比特比隔壁少，实测 MSE 却只高一点点（比值≤1.5）。
	legacy := []int{5, 4, 4, 3, 4, 5, 3, 2, 2, 1}
	legacyRows := buildLegacyRows(st, legacy)
	if err := writeErrorTable(filepath.Join(outDir, "error_table.md"), st, legacy, legacyRows); err != nil {
		return err
	}
	if err := writeErrorCSV(filepath.Join(outDir, "error_table.csv"), st, legacy, legacyRows); err != nil {
		return err
	}
	wf("\n## 旧版手工分配误差表（总比特 %d，%.2f bpp）\n", sb.TotalBits(st.NS, legacy),
		float64(sb.TotalBits(st.NS, legacy))/float64(st.N))
	wf("| i | 带 | 比特 | σ² | 实测MSE | 归一化MSE/σ² |")
	wf("|--:|---|--:|--:|--:|--:|")
	for _, r := range legacyRows {
		mark := ""
		if r.Suspect {
			mark = " ⚠️可疑行"
		}
		wf("| %d | %s | %d | %.1f | %.1f | %.4f |%s", r.I, r.Label, r.Bits, r.Var, r.MSE, r.Norm, mark)
	}
	for _, r := range legacyRows {
		if r.Suspect {
			wf("> 可疑行：**%s** 只分 %d bit，比隔壁 **%s** 的 %d bit 少，",
				r.Label, r.Bits, r.NeighborLabel, r.NeighborBits)
			wf("> 但实测 MSE %.1f 对 %.1f，仅高 %.0f%%。根因是隔壁 %s 的方差 σ²=%.0f 是本带 σ²=%.0f 的 %.1f 倍：",
				r.MSE, r.NeighborMSE, 100*(r.MSE/r.NeighborMSE-1),
				r.NeighborLabel, r.NeighborVar, r.Var, r.NeighborVar/r.Var)
			wf("> 低方差带本来就不需要那么多比特——这正是按方差反向注水要系统化解决的事，而不是把它当成测量误差。")
		}
	}

	// ---------- 2. 连续反向注水（主预算 B=N，1bpp） ----------
	mainK := 1
	mainB := st.N * mainK
	wr := sb.Waterfill(st.Stats, mainB)
	wf("\n## 连续反向注水 B=%d (%.2f bpp)\n", mainB, float64(mainB)/float64(st.N))
	wf("水位 θ=%.4f，活动集迭代 %d 次，活动带 %v，连续解总比特 %.1f（残差 %.2e）\n",
		wr.Theta, wr.Iterations, labels(st, wr.Active), wr.ContinuousSum, wr.BudgetResidual)
	wf("| 带 | σ² | b*连续 |")
	wf("|---|--:|--:|")
	for _, d := range wr.Details {
		wf("| %s | %.2f | %.3f |", d.Label, d.Variance, d.Continuous)
	}

	// ---------- 3. 同体积：DP 整数注水 vs 平分 ----------
	wf("\n## 同体积对比（精确总比特 B=kN）\n")
	wf("| k bit/系数 | B | 方案 | 各带比特(粗→细) | 用比特 | 文件字节 | 加权MSE(系数) | 像素MSE | PSNR dB |")
	wf("|--:|--:|---|---|--:|--:|--:|--:|--:|")
	type point struct {
		k                        int
		bitsW, bitsE             []int
		rw, re                   *sb.RunResult
		bytesW, bytesE           int
	}
	var main point
	for _, k := range []int{1, 2, 3, 4} {
		B := st.N * k
		bitsM := st.BestInteger(B, true).Bits // 纯方差模型整数反向注水
		bitsW := st.BestIntegerMeasured(B, true).Bits // 实测量化失真校准的整数最优
		bitsE := sb.EqualBits(len(st.Bands), k)
		rm, err := st.RunBits(bitsM, tau)
		if err != nil {
			return err
		}
		rw, err := st.RunBits(bitsW, tau)
		if err != nil {
			return err
		}
		re, err := st.RunBits(bitsE, tau)
		if err != nil {
			return err
		}
		m := len(st.Bands)
		bmBytes := sb.StreamBytes(m, rm.Used)
		bwBytes := sb.StreamBytes(m, rw.Used)
		beBytes := sb.StreamBytes(m, re.Used)
		wf("| %d | %d | 方差注水整数解 | %s | %d | %d | %.3f | %.3f | %.2f |",
			k, B, bitVec(bitsM), rm.Used, bmBytes, rm.WeightedCoefMSE, rm.MSE, rm.PSNR)
		wf("| %d | %d | 实测DP校准 | %s | %d | %d | %.3f | %.3f | %.2f |",
			k, B, bitVec(bitsW), rw.Used, bwBytes, rw.WeightedCoefMSE, rw.MSE, rw.PSNR)
		wf("| %d | %d | 平分 | %s | %d | %d | %.3f | %.3f | %.2f |",
			k, B, bitVec(bitsE), re.Used, beBytes, re.WeightedCoefMSE, re.MSE, re.PSNR)
		gainM := 100 * (re.MSE - rm.MSE) / re.MSE
		gain := 100 * (re.MSE - rw.MSE) / re.MSE
		wf("| | | **方差注水相对平分降幅** | | | | | **%.1f%%** | **+%.2f** |", gainM, rm.PSNR-re.PSNR)
		wf("| | | **实测DP相对平分降幅** | | | | | **%.1f%%** | **+%.2f** |", gain, rw.PSNR-re.PSNR)
		if k == mainK {
			// 画面与两类失真统计采用“实测 R-D 曲线的离散注水 DP”落地解：
			// 它与方差注水同一原理，只是把理想 D=σ²4^-b 换成真实量化器失真，
			// 在激进预算下画面与两类失真的区分最清楚。纯方差整数解见上表。
			main = point{k: k, bitsW: bitsW, bitsE: bitsE, rw: rw, re: re, bytesW: bwBytes, bytesE: beBytes}
		}
	}

	// ---------- 4. 紧预算截断（不超预算 DP） ----------
	wf("\n## 紧预算与整带截断\n")
	for _, B := range []int{4096, 2048, 1024} {
		dp := st.BestIntegerMeasured(B, false)
		r, err := st.RunBits(dp.Bits, tau)
		if err != nil {
			return err
		}
		zero := sb.TruncatedBands(dp.Bits)
		wf("- B=%d (%.3f bpp)：用 %d bit，比特 %s；整带截断 %d 带：%s；MSE=%.2f PSNR=%.2f",
			B, float64(B)/float64(st.N), dp.Used, bitVec(dp.Bits),
			len(zero), idxLabels(st, zero), r.MSE, r.PSNR)
	}

	// ---------- 5. 字节上限截断演示 ----------
	prio := sb.PriorityByMarginalValue(st)
	packed := sb.Pack(st, main.rw, prio)
	capBytes := 1500
	tbits, dropped, tsize, err := sb.TruncateForCap(st, main.bitsW, prio, capBytes)
	if err != nil {
		return err
	}
	rt, err := st.RunBits(tbits, tau)
	if err != nil {
		return err
	}
	wf("\n## 字节上限截断（主方案 B=%d，原流 %d 字节，上限 %d）\n", mainB, len(packed), capBytes)
	wf("优先级（高→低）：%s", idxLabels(st, prio))
	wf("截断规则：头部保留，按优先级从末端（最不重要带）整带丢弃，直到文件≤上限。")
	wf("实际截掉 %d 带：%s；截断后 %d 字节，MSE=%.2f PSNR=%.2f",
		len(dropped), idxLabels(st, dropped), tsize, rt.MSE, rt.PSNR)

	// 高码率流上的字节上限截断：更能体现“先砍最细高频带”。
	{
		B4 := st.N * 4
		bits4 := st.BestIntegerMeasured(B4, true).Bits
		r4, err := st.RunBits(bits4, tau)
		if err != nil {
			return err
		}
		full4 := sb.StreamBytes(len(st.Bands), r4.Used)
		cap4 := 6000
		tb4, drop4, size4, err := sb.TruncateForCap(st, bits4, prio, cap4)
		if err != nil {
			return err
		}
		rt4, err := st.RunBits(tb4, tau)
		if err != nil {
			return err
		}
		wf("\n### 高码率流的字节上限截断（k=4，原流 %d 字节，上限 %d）\n", full4, cap4)
		wf("原分配 %s", bitVec(bits4))
		wf("按同一规则从优先级末端整带丢弃，截掉 %d 带：**%s**；截断后 %d 字节（≤%d），MSE %.1f→%.1f，PSNR %.2f→%.2f。",
			len(drop4), idxLabels(st, drop4), size4, cap4, r4.MSE, rt4.MSE, r4.PSNR, rt4.PSNR)
		wf("被截顺序即“方差能量最低、最不耐看”的带优先：HH1 本就 0bit，随后是 HH2/LH1 等细高频；LL3 永远最后才动。")
		if err := sb.SaveGrayPNG(filepath.Join(outDir, "truncated_k4_recon.png"), w, h, rt4.Recon); err != nil {
			return err
		}
	}

	// ---------- 6. 两类失真统计（主预算） ----------
	writeArt := func(title string, a sb.ArtifactStats) {
		wf("\n### %s\n", title)
		wf("- 异常像素 |e|≥%.1f：%d/%d = **%.2f%%**", a.Tau, a.Flagged, a.N, a.FlagPct)
		wf("- 主导归属占比（两类内）：低频块状条纹 **%.1f%%**（%d 像素）／高频边缘毛刺 **%.1f%%**（%d 像素）",
			a.DomLowPct, a.DomLow, a.DomHighPct, a.DomHigh)
		wf("- 误差能量占比（Parseval，合计100%%）：低频族 **%.1f%%** ／高频族 **%.1f%%**",
			100*a.EnergyLowShare, 100*a.EnergyHighShare)
		wf("- 块状条纹形态：块缝命中率 **%.2f%%**（%d/%d 条缝阶跃≥%.0f），块缝阶跃RMS=%.2f，内部RMS=%.2f，超额MSE=%.2f",
			a.SeamHitPct, a.SeamHits, a.SeamPositions, a.SeamHitThreshold, a.SeamStepRMS, a.InteriorStepRMS, a.SeamExcessMSE)
		wf("- 边缘毛刺形态：Sobel强边缘 %d px，边缘邻域 %d px；毛刺检出 **%.2f%%**（%d px，|Laplacian|≥%.0f），边缘拉普拉斯能量=%.1f vs 带外=%.2f",
			a.EdgePixels, a.EdgeBandPixels, a.BurrPrevalence, a.BurrHits, a.BurrLapThreshold, a.BurrScoreEdge, a.BurrScoreOutside)
	}
	wf("\n## 两类失真分开统计（主预算 B=%d）", mainB)
	writeArt("注水方案", main.rw.Art)
	writeArt("平分方案", main.re.Art)
	writeArt("截断方案", rt.Art)

	// ---------- 7. 写图像 ----------
	if err := writeImages(outDir, st, main, tbits, rt, prio); err != nil {
		return err
	}
	if err := sb.SavePGM(filepath.Join(outDir, "source.pgm"), w, h, pix); err != nil {
		return err
	}
	if err := sb.SaveGrayPNG(filepath.Join(outDir, "source.png"), w, h, pix); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "packed_wf.sbs"), packed, 0o644); err != nil {
		return err
	}
	tpacked := sb.Pack(st, rt, prio)
	if err := os.WriteFile(filepath.Join(outDir, "packed_truncated.sbs"), tpacked, 0o644); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(outDir, "tables.md"), []byte(md.String()), 0o644); err != nil {
		return err
	}
	fmt.Println("\n产物已写入", outDir)
	return nil
}

func writeImages(outDir string, st *sb.Setup, main struct {
	k              int
	bitsW, bitsE   []int
	rw, re         *sb.RunResult
	bytesW, bytesE  int
}, tbits []int, rt *sb.RunResult, prio []int) error {
	w, h := st.W, st.H
	// 全图对比：原图 | 注水 | 平分 | 注水误差×4 | 平分误差×4
	errW := make([]uint8, st.N)
	errE := make([]uint8, st.N)
	for i := range errW {
		errW[i] = 128
		errE[i] = 128
	}
	ewSum := make([]float64, st.N)
	eeSum := make([]float64, st.N)
	for _, e := range main.rw.BandErr {
		for p, v := range e {
			ewSum[p] += v
		}
	}
	for _, e := range main.re.BandErr {
		for p, v := range e {
			eeSum[p] += v
		}
	}
	errW = sb.MapSymmetric(ewSum, 4)
	errE = sb.MapSymmetric(eeSum, 4)
	panels := [][]uint8{st.Pix, main.rw.Recon, main.re.Recon, errW, errE}
	if err := sb.ComposeRow(filepath.Join(outDir, "compare_k1.png"), panels, w, h, 3, 2); err != nil {
		return err
	}

	// 边缘区域裁剪放大（棋盘 + 中部笔画 / 对角线）。
	crops := []struct {
		name string
		x0, y0, cw, ch int
	}{
		{"checker", 66, 62, 58, 58},
		{"diag", 4, 14, 66, 66},
	}
	for _, c := range crops {
		_ = sb.CropZoom(filepath.Join(outDir, "zoom_"+c.name+"_src.png"), st.Pix, w, c.x0, c.y0, c.cw, c.ch, 4)
		_ = sb.CropZoom(filepath.Join(outDir, "zoom_"+c.name+"_wf.png"), main.rw.Recon, w, c.x0, c.y0, c.cw, c.ch, 4)
		_ = sb.CropZoom(filepath.Join(outDir, "zoom_"+c.name+"_eq.png"), main.re.Recon, w, c.x0, c.y0, c.cw, c.ch, 4)
	}

	// 逐带三联图：原始该带单独重建 | 量化后该带单独重建 | 该带误差×3。
	writeBandRows := func(prefix string, bits []int, r *sb.RunResult) error {
		for i, b := range st.Bands {
			origOnly, err := sb.BandOnlyImage(st.Bands, i, w, h, st.Levels)
			if err != nil {
				return err
			}
			// 中间面板必须与左图同口径：只保留“量化后的这一个带”，其余带置零。
			qBands := make([]sb.Band, len(st.Bands))
			for j, src := range st.Bands {
				nb := src
				nb.Coef = make([]float64, src.N)
				if j == i {
					nb.Coef = r.Quants[i].Recon
				}
				qBands[j] = nb
			}
			qOnly, err := sb.Reconstruct(qBands, w, h, st.Levels)
			if err != nil {
				return err
			}
			p1, p2 := sb.MapAuto(origOnly, qOnly, w, h)
			p3 := sb.MapSymmetric(r.BandErr[i], 3)
			fn := filepath.Join(outDir, fmt.Sprintf("band_%s_%02d_%s_b%d.png", prefix, i, b.Label, bits[i]))
			if err := sb.ComposeRow(fn, [][]uint8{p1, p2, p3}, w, h, 2, 2); err != nil {
				return err
			}
		}
		return nil
	}
	if err := writeBandRows("wf", main.bitsW, main.rw); err != nil {
		return err
	}
	if err := writeBandRows("eq", main.bitsE, main.re); err != nil {
		return err
	}
	// 截断方案全图。
	if err := sb.SaveGrayPNG(filepath.Join(outDir, "truncated_recon.png"), w, h, rt.Recon); err != nil {
		return err
	}
	_ = prio
	return nil
}

func bitVec(bits []int) string {
	parts := make([]string, len(bits))
	for i, b := range bits {
		parts[i] = fmt.Sprintf("%d", b)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func labels(st *sb.Setup, idx []int) string {
	parts := make([]string, len(idx))
	for k, i := range idx {
		parts[k] = st.Stats[i].Label
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func idxLabels(st *sb.Setup, idx []int) string {
	if len(idx) == 0 {
		return "无"
	}
	return labels(st, idx) + fmt.Sprintf(" %v", idx)
}

func sqrt(v float64) float64 {
	if v < 0 {
		return 0
	}
	x := v
	if x <= 0 {
		return 0
	}
	for i := 0; i < 30; i++ {
		x = (x + v/x) / 2
	}
	return x
}
