package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"gotoolkit/api"
)

// blockReport 只在一处定义体积口径，报告与测试共用。
type blockReport struct {
	text      string
	tsv       string
	blocksTSV string
	json      string
}

func mb(n int) string {
	return fmt.Sprintf("%.3f KiB", float64(n)/1024)
}

// regionJSON 统一两类区域的字段，三张图的表格直接从同一结构取数。
type regionJSON struct {
	Blocks   int     `json:"blocks"`
	Pixels   int     `json:"pixels"`
	PixelPct float64 `json:"pixel_pct"`
	MSE      float64 `json:"mse"`
	MaxBlock float64 `json:"max_block_mse"`
	MaxAbs   int     `json:"max_abs_err"`
	Patches  int     `json:"patches"`
	Colors   int     `json:"colors"`
	MaskPct  float64 `json:"mask_pct"`
}

func analyzeReport(name string, img *api.Image, a *api.HybridAnalysis) blockReport {
	// 体积：三种单一格式的实际编码长度，与混合码流长度。
	single := map[string]int{}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := api.Encode(img, api.Options{Format: f})
		if err != nil {
			continue
		}
		single[f] = len(body)
	}
	body, err := api.Encode(img, api.Options{Format: "hybrid"})
	hyb := 0
	if err == nil {
		hyb = len(body)
	}
	// 混合模式有体积约束：取 GTH1 与最小单一格式中较小者，报告里如实标注实际形态。
	used := "GTH1"
	if hyb > 0 && minSingleOf(single) < hyb {
		used = "回退 " + minNameOf(single)
	}
	minSingle, minName := minSingleOf(single), minNameOf(single)
	flatShare := float64(a.Flat.Pixels) / float64(a.W*a.H)

	var b strings.Builder
	fmt.Fprintf(&b, "== %s  %dx%d τ=%.5f (τ²=%.5f)\n", name, a.W, a.H, a.Tau, a.TauSq)
	fmt.Fprintf(&b, "分区    平坦块 %d / %d（%.1f%% 像素）  纹理块 %d\n",
		a.Flat.Blocks, len(a.Blocks), flatShare*100, a.Tex.Blocks)
	fmt.Fprintf(&b, "平坦区  MSE %.3f  块 MSE 最大 %.3f  最大绝对误差 %d  色块 %d  灰度级 %d\n",
		a.Flat.MSE, a.Flat.MaxBlock, a.Flat.MaxAbs, a.Flat.Patches, a.Flat.Colors)
	fmt.Fprintf(&b, "纹理区  MSE %.3f  块 MSE 最大 %.3f  最大绝对误差 %d\n",
		a.Tex.MSE, a.Tex.MaxBlock, a.Tex.MaxAbs)
	fmt.Fprintf(&b, "整幅    MSE %.4f（模型 %.4f）  扫描最小点 k=%d/%d  τ* 落点 k=%d  平台 (%.5f, %.5f]\n",
		a.Total(), a.TotalModel(), a.MinK, len(a.Blocks), a.ModelK, a.Lo, a.Hi)
	fmt.Fprintf(&b, "体积    hybrid %d B (%s, %s)  pgm %d  bmp %d  rle %d  最小单一 %s=%d  比值 %.3f\n",
		hyb, mb(hyb), used, single["pgm"], single["bmp"], single["rle"], minName, minSingle,
		float64(hyb)/float64(minSingle))
	fmt.Fprintf(&b, "掩码    平坦 %.1f%% / 纹理 %.1f%%\n", flatShare*100, (1-flatShare)*100)

	var tsv strings.Builder
	tsv.WriteString("tau\tflat_blocks\tflat_share\tmse_flat\tmse_tex\ttotal\ttotal_model\n")
	for _, p := range a.Sweep {
		fmt.Fprintf(&tsv, "%.6f\t%d\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\n",
			p.Tau, p.FlatBlocks, p.FlatShare, p.MSEFlat, p.MSETex, p.Total, p.TotalModel)
	}

	var blocks strings.Builder
	blocks.WriteString("bx\tby\tsigma\tflat\n")
	for _, st := range a.Blocks {
		f := 0
		if st.Flat {
			f = 1
		}
		fmt.Fprintf(&blocks, "%d\t%d\t%.9f\t%d\n", st.BX, st.BY, st.Sigma, f)
	}

	summary := map[string]any{
		"image":        name,
		"width":        a.W,
		"height":       a.H,
		"block":        16,
		"tau":          a.Tau,
		"tau_sq":       a.TauSq,
		"block_count":  len(a.Blocks),
		"total_pixels": a.W * a.H,
		"flat": regionJSON{
			Blocks: a.Flat.Blocks, Pixels: a.Flat.Pixels, PixelPct: flatShare * 100,
			MSE: a.Flat.MSE, MaxBlock: a.Flat.MaxBlock, MaxAbs: a.Flat.MaxAbs,
			Patches: a.Flat.Patches, Colors: a.Flat.Colors, MaskPct: flatShare * 100,
		},
		"tex": regionJSON{
			Blocks: a.Tex.Blocks, Pixels: a.Tex.Pixels, PixelPct: (1 - flatShare) * 100,
			MSE: a.Tex.MSE, MaxBlock: a.Tex.MaxBlock, MaxAbs: a.Tex.MaxAbs,
			Patches: 0, Colors: 0, MaskPct: (1 - flatShare) * 100,
		},
		"total_mse":     a.Total(),
		"total_model":   a.TotalModel(),
		"sweep_min_k":   a.MinK,
		"sweep_min_tau": a.MinTau,
		"model_k":       a.ModelK,
		"plateau_lo":    a.Lo,
		"plateau_hi":    a.Hi,
		"volume": map[string]any{
			"hybrid": hyb, "pgm single": single["pgm"], "bmp single": single["bmp"], "rle single": single["rle"],
			"min_single": minSingle, "min_name": minName,
		},
	}
	js, _ := json.MarshalIndent(summary, "", "  ")
	return blockReport{text: b.String(), tsv: tsv.String(), blocksTSV: blocks.String(), json: string(js) + "\n"}
}

func minSingleOf(single map[string]int) int {
	min := 0
	for _, n := range single {
		if min == 0 || n < min {
			min = n
		}
	}
	return min
}

func minNameOf(single map[string]int) string {
	name, min := "", 0
	for f, n := range single {
		if min == 0 || n < min {
			name, min = strings.ToUpper(f), n
		}
	}
	return name
}
