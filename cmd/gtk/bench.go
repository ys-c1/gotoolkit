package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gotoolkit/api"
)

// encodeGrayPNG 把灰度像素编码成 PNG，方便直接看图（PGM/BMP 原件也同时保留）。
func encodeGrayPNG(w, h int, pix []uint8) ([]byte, error) {
	img := image.NewGray(image.Rect(0, 0, w, h))
	copy(img.Pix, pix)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// runBench 实现 `gtk bench`（别名 compare / quality）。
//
// 不带 --threshold 时按 --thresholds（默认 4,7）把五档尺寸各跑两遍并给出
// 逐档一致性结论；带 --threshold T 时只跑一个阈值，便于“各跑一遍”式复跑。
func runBench(args []string) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	in := fs.String("in", "", "外部 PGM 原图；不给则用内置确定性五档样本")
	one := fs.Int("threshold", -1, "只跑单个画质阈值 τ（最大逐像素误差 0..255）")
	pair := fs.String("thresholds", "4,7", "成对运行的两个阈值，逗号分隔，默认 4,7（同一离散误差档，结论严格一致）")
	out := fs.String("out", filepath.Join("artifacts", "bench"), "报告与出图目录")
	amp := fs.Int("diff-amp", 16, "差异图放大倍数（线性拉伸后截断到 255）")
	noImages := fs.Bool("no-images", false, "只出文字/JSON 报告，不写出图像与码流")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var images []api.BenchImage
	if *in != "" {
		raw, err := os.ReadFile(*in)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取失败:", err)
			return 1
		}
		img, err := api.Decode(raw, api.Options{Format: "pgm"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "PGM 解码失败:", err)
			return 1
		}
		images = []api.BenchImage{{Label: "外部样本", W: img.W, H: img.H, Pix: img.Pix}}
	} else {
		images = api.CanonicalImages(api.DefaultSizes)
	}

	var thresholds []int
	single := *one >= 0
	if single {
		if *one < 0 || *one > api.MaxThreshold {
			fmt.Fprintf(os.Stderr, "阈值越界: %d 不在 [0,%d]\n", *one, api.MaxThreshold)
			return 2
		}
		thresholds = []int{*one}
	} else {
		var err error
		thresholds, err = parseThresholds(*pair)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}

	runs := make([]*api.ThresholdRun, 0, len(thresholds))
	for _, tau := range thresholds {
		r, err := api.RunThresholdOn(images, tau)
		if err != nil {
			fmt.Fprintln(os.Stderr, "分析失败:", err)
			return 1
		}
		runs = append(runs, r)
	}

	var cons *api.Consistency
	if len(runs) == 2 {
		cons = api.CompareRuns(runs[0], runs[1])
	}

	// 文字报告同时打到 stdout；成对运行时落一份 report.txt。
	var buf strings.Builder
	renderReport(&buf, runs, cons, renderOpts{external: *in != "", amp: *amp})
	fmt.Print(buf.String())

	if *out != "" {
		if err := os.MkdirAll(*out, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "建目录失败:", err)
			return 1
		}
		if len(runs) == 2 {
			if err := os.WriteFile(filepath.Join(*out, "report.txt"), []byte(buf.String()), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "写 report.txt 失败:", err)
				return 1
			}
			rep := api.BenchReport{Pattern: "ramp-shapes-v1(builtin)"}
			if *in != "" {
				rep.Pattern = "external:" + *in
			}
			for _, im := range images {
				rep.Sizes = append(rep.Sizes, [2]int{im.W, im.H})
			}
			rep.Runs = runs
			rep.Consistency = cons
			if jb, err := json.MarshalIndent(rep, "", "  "); err == nil {
				_ = os.WriteFile(filepath.Join(*out, "report.json"), jb, 0o644)
			}
		}
		if !*noImages {
			n, err := writeArtifacts(*out, images, runs, *amp)
			if err != nil {
				fmt.Fprintln(os.Stderr, "出图失败:", err)
				return 1
			}
			fmt.Printf("已写出 %d 个图像/码流文件到 %s\n", n, *out)
		}
	}
	return 0
}

func parseThresholds(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var v int
		if _, err := fmt.Sscanf(p, "%d", &v); err != nil {
			return nil, fmt.Errorf("无法解析阈值 %q", p)
		}
		if v < 0 || v > api.MaxThreshold {
			return nil, fmt.Errorf("阈值 %d 超出 [0,%d]", v, api.MaxThreshold)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--thresholds 至少给一个阈值")
	}
	return out, nil
}

type renderOpts struct {
	external bool
	amp      int
}

// renderReport 按题面要求的行结构渲染：
// 每档有独立的“画幅行”“实际体积行”“阈值结论行”，格式与体积始终成对。
func renderReport(w io.Writer, runs []*api.ThresholdRun, cons *api.Consistency, opt renderOpts) {
	fmt.Fprintln(w, "画质阈值选格式基准（gtk bench）")
	fmt.Fprintln(w, "画质指标：τ = 允许的最大逐像素绝对灰度误差 Emax（范围 0..255）")
	fmt.Fprintln(w, "量化模型：s 位右移 + 中点反量化，有效位深 b=8-s；最坏误差上界 E(s)=0(s=0) 或 2^(s-1)(s>=1)")
	fmt.Fprintln(w, "反解公式：s*(τ)=0（τ=0）或 min(7, floor(log2 τ)+1)（τ>=1）；b*=8-s*")
	if opt.external {
		fmt.Fprintln(w, "样本：外部 --in 指定 PGM")
	} else {
		fmt.Fprintln(w, "样本：内置确定性图样 ramp-shapes-v1（无随机源，含纯 0 像素，实测 Emax 严格等于上界）")
	}
	if len(runs[0].Tiers) > 0 {
		var dims []string
		for _, t := range runs[0].Tiers {
			dims = append(dims, fmt.Sprintf("%s=%s(%dpx)", t.Label, t.Dims(), t.N))
		}
		fmt.Fprintln(w, "五档画幅总览："+strings.Join(dims, "  "))
	}
	fmt.Fprintln(w, strings.Repeat("=", 72))

	for ri, r := range runs {
		tag := fmt.Sprintf("阈值 %d", r.Threshold)
		if len(runs) == 2 {
			tag = fmt.Sprintf("阈值档 %s（τ=%d）", string(rune('A'+ri)), r.Threshold)
		}
		fmt.Fprintf(w, "【%s】反解：τ=%d -> s*=%d，位深 b=%d，误差上界 E=%d\n",
			tag, r.Threshold, r.PlanShift, r.PlanBitDepth, r.PlanBound)

		for _, t := range r.Tiers {
			// 1) 画幅独占一行。
			fmt.Fprintf(w, "  %s·画幅：%s（%d 像素）\n", t.Label, t.Dims(), t.N)

			// 2) 三种格式的实际体积，全部 “格式=体积B” 成对出现。
			pairs := make([]string, 0, len(t.Formats))
			for _, fr := range t.Formats {
				pairs = append(pairs, fmt.Sprintf("%s=%dB", fr.Format, fr.Chosen.Bytes))
			}
			fmt.Fprintf(w, "  %s·实际体积（格式=体积，成对）：%s\n", t.Label, strings.Join(pairs, "  "))

			// 3) 推导公式与实际字节对账 + 逐像素核对（合并成一行明细）。
			checks := make([]string, 0, len(t.Formats))
			for _, fr := range t.Formats {
				mark := "公式精确"
				if !fr.Planned.FormulaExact {
					mark = fmt.Sprintf("公式偏差(公式%d/实际%d)", fr.Planned.FormulaBytes, fr.Planned.Bytes)
				}
				checks = append(checks, fmt.Sprintf("%s[%s,Emax实测=%d,MAE=%.3f,差异像素=%.1f%%,≤τ:%v,最小确认:%v]",
					fr.Format, mark, fr.Chosen.Measured.MaxAbs, fr.Chosen.Measured.MAE,
					100*fr.Chosen.Measured.DiffRatio(), fr.Chosen.Eligible, fr.MinBytesConfirmed))
			}
			fmt.Fprintf(w, "  %s·逐像素核对：%s；三格式重建一致=%v；误差上界全部成立=%v\n",
				t.Label, strings.Join(checks, " "), t.ReconIdentical, t.BoundHolds)

			// 4) 题面核心：阈值与选中格式的结论，每档恰好一行，选中格式与体积成对。
			fmt.Fprintf(w, "  【阈值结论 τ=%d · %s · %s】选中 %s，%s=%dB（实测 Emax=%d≤%d，三格式中体积最小）\n",
				t.Threshold, t.Label, t.Dims(), t.SelectedFormat,
				t.SelectedFormat, t.SelectedBytes, selectedMax(t), t.Threshold)
		}
		fmt.Fprintln(w, strings.Repeat("-", 72))
	}

	if cons != nil {
		renderConsistency(w, cons)
	}
}

func selectedMax(t *api.TierReport) int {
	for _, fr := range t.Formats {
		if fr.Format == t.SelectedFormat {
			return fr.Chosen.Measured.MaxAbs
		}
	}
	return -1
}

func renderConsistency(w io.Writer, c *api.Consistency) {
	fmt.Fprintln(w, "【两档阈值的对应关系】")
	for _, ta := range c.Items {
		// Items 只含对照摘要；完整映射另从调用处拿不到 run，故这里用摘要双列。
		fmt.Fprintf(w, "  %s %s：τ=%d -> %s=%dB ｜ τ=%d -> %s=%dB\n",
			ta.Label, ta.Dims, c.ThresholdA, ta.FormatA, ta.BytesA,
			c.ThresholdB, ta.FormatB, ta.BytesB)
	}
	fmt.Fprintln(w, "【逐档一致性核对】")
	samePairs := 0
	for _, it := range c.Items {
		switch {
		case it.PairAgree:
			samePairs++
			fmt.Fprintf(w, "  %s %s：%s=%dB ↔ %s=%dB → 一致（选中格式与体积对完全相同）\n",
				it.Label, it.Dims, it.FormatA, it.BytesA, it.FormatB, it.BytesB)
		case it.FormatAgree:
			fmt.Fprintf(w, "  %s %s：%s=%dB ↔ %s=%dB → 格式一致（均为 %s），体积随阈值变化\n",
				it.Label, it.Dims, it.FormatA, it.BytesA, it.FormatB, it.BytesB, it.FormatA)
		default:
			fmt.Fprintf(w, "  %s %s：%s=%dB ↔ %s=%dB → 格式不一致\n",
				it.Label, it.Dims, it.FormatA, it.BytesA, it.FormatB, it.BytesB)
		}
	}
	fmt.Fprintf(w, "【总结论】格式一致 %d/%d；“格式+体积”对一致 %d/%d。\n",
		countFormatAgree(c), len(c.Items), samePairs, len(c.Items))
	if c.PairsAgree {
		fmt.Fprintf(w, "τ=%d 与 τ=%d 落在同一个离散误差区间，反解到同一量化档，编码产物逐字节相同，故结论严格一致。\n",
			c.ThresholdA, c.ThresholdB)
	}
	fmt.Fprintln(w, strings.Repeat("=", 72))
}

func countFormatAgree(c *api.Consistency) int {
	n := 0
	for _, it := range c.Items {
		if it.FormatAgree {
			n++
		}
	}
	return n
}

// writeArtifacts 把原图、各格式选中码流、解码 PGM、放大差异 PGM 落盘。
func writeArtifacts(dir string, images []api.BenchImage, runs []*api.ThresholdRun, amp int) (int, error) {
	n := 0
	// 原图只写一次。
	origDir := filepath.Join(dir, "orig")
	if err := os.MkdirAll(origDir, 0o755); err != nil {
		return n, err
	}
	for i, im := range images {
		base := fmt.Sprintf("tier%d_%dx%d", i+1, im.W, im.H)
		if im.Label == "外部样本" {
			base = "external"
		}
		pg, err := api.Encode(&api.Image{W: im.W, H: im.H, Pix: im.Pix}, api.Options{Format: "pgm"})
		if err != nil {
			return n, err
		}
		if err := os.WriteFile(filepath.Join(origDir, base+".pgm"), pg, 0o644); err != nil {
			return n, err
		}
		n++
		if pb, err := encodeGrayPNG(im.W, im.H, im.Pix); err == nil {
			if err := os.WriteFile(filepath.Join(origDir, base+".png"), pb, 0o644); err != nil {
				return n, err
			}
			n++
		}
	}

	for _, r := range runs {
		tdir := filepath.Join(dir, fmt.Sprintf("t%d", r.Threshold))
		if err := os.MkdirAll(tdir, 0o755); err != nil {
			return n, err
		}
		for i, t := range r.Tiers {
			base := fmt.Sprintf("tier%d_%dx%d", i+1, t.W, t.H)
			if len(images) == 1 && images[0].Label == "外部样本" {
				base = "external"
			}
			var src []uint8
			if i < len(images) {
				src = images[i].Pix
			}
			for _, fr := range t.Formats {
				cp := fr.Chosen
				ext := map[string]string{"pgm": ".pgm", "bmp": ".bmp", "rle": ".rle"}[fr.Format]
				native := filepath.Join(tdir, base+"_"+fr.Format+ext)
				if err := os.WriteFile(native, cp.Body, 0o644); err != nil {
					return n, err
				}
				n++
				decPGM, err := api.Encode(&api.Image{W: t.W, H: t.H, Pix: cp.Recon}, api.Options{Format: "pgm"})
				if err != nil {
					return n, err
				}
				if err := os.WriteFile(filepath.Join(tdir, base+"_"+fr.Format+".dec.pgm"), decPGM, 0o644); err != nil {
					return n, err
				}
				n++
				if pb, err := encodeGrayPNG(t.W, t.H, cp.Recon); err == nil {
					if err := os.WriteFile(filepath.Join(tdir, base+"_"+fr.Format+".dec.png"), pb, 0o644); err != nil {
						return n, err
					}
					n++
				}
				diff := api.DiffRaster(src, cp.Recon, amp)
				diffPGM, err := api.Encode(&api.Image{W: t.W, H: t.H, Pix: diff}, api.Options{Format: "pgm"})
				if err != nil {
					return n, err
				}
				diffStem := fmt.Sprintf("%s_%s.diffx%d", base, fr.Format, amp)
				if err := os.WriteFile(filepath.Join(tdir, diffStem+".pgm"), diffPGM, 0o644); err != nil {
					return n, err
				}
				n++
				if pb, err := encodeGrayPNG(t.W, t.H, diff); err == nil {
					if err := os.WriteFile(filepath.Join(tdir, diffStem+".png"), pb, 0o644); err != nil {
						return n, err
					}
					n++
				}
			}
		}
	}
	return n, nil
}
