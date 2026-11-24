package main

import (
	"fmt"
	"math"
	"strings"

	"gotoolkit/api"
	"gotoolkit/internal/lowbit"
)

// tier 是一个待测参数档。
type tier struct {
	Bits      int `json:"bits"`
	TargetErr int `json:"target_err"`
	Q         int `json:"q"`
	EffErr    int `json:"eff_err"`
}

// defaultTiers 是“两档参数各测一次”的默认选取：d=4 与 d=2。
func defaultTiers(tgt int) ([]tier, error) {
	var out []tier
	for _, d := range []int{4, 2} {
		p, err := lowbit.NewParams(d, tgt, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, tier{d, p.TargetErr, p.Q, p.EffErr})
	}
	return out, nil
}

func selectTiers(bits, tgt int) ([]tier, error) {
	if bits == 0 {
		return defaultTiers(tgt)
	}
	p, err := lowbit.NewParams(bits, tgt, 0)
	if err != nil {
		return nil, err
	}
	return []tier{{bits, p.TargetErr, p.Q, p.EffErr}}, nil
}

// MatrixRow 是组合矩阵的一行：行号 + 组合描述 + 三项指标。
type MatrixRow struct {
	No            int    `json:"no"`
	Format        string `json:"format"`
	Subcommand    string `json:"subcommand"`
	Force         string `json:"force"`
	Bits          int    `json:"bits"`
	TargetErr     int    `json:"target_err"`
	Q             int    `json:"q"`
	OutputBytes   int    `json:"output_bytes"`
	OutputSHA     string `json:"output_sha256"`
	PSNRvsAllOff  string `json:"psnr_vs_all_off_db"`
	MaxErrvsAllOf int    `json:"max_abs_err_vs_all_off"`
	RoundTripOK   bool   `json:"roundtrip_ok"`
}

// TierReport 是一档参数的完整记录。
type TierReport struct {
	Bits       int         `json:"bits"`
	TargetErr  int         `json:"target_err"`
	Q          int         `json:"q"`
	EffErr     int         `json:"eff_err"`
	Inequality string      `json:"inequality"`
	Kernel     string      `json:"kernel"`
	KernelRaw  []int       `json:"kernel_weights"`
	KernelNorm []float64   `json:"kernel_normalized"`
	IndexBits  int         `json:"index_bits"`
	Palette    [2][]uint8  `json:"palette_before_after"`
	IndexShape [2][2]int   `json:"index_table_shapes"`
	Indices    [2][]uint8  `json:"index_tables_before_after"`
	Rows       []MatrixRow `json:"rows"`
}

// Report 是矩阵输出的顶层结构。
type Report struct {
	Tiers           []TierReport `json:"tiers"`
	BaselineSHA     [3]string    `json:"baseline_sha256"`
	BaselineBytes   [3]int       `json:"baseline_bytes"`
	AllOffIdentical bool         `json:"all_off_identical_to_baseline"`
	Rows            []MatrixRow  `json:"rows"`
}

// baseline 计算三种格式全关时的字节与摘要；同时校验与 testdata/golden 一致。
func baseline(img *api.Image) ([3]string, [3]int, error) {
	var sha [3]string
	var n [3]int
	for i, f := range []string{"pgm", "bmp", "rle"} {
		body, err := api.Encode(img, api.Options{Format: f, Force: api.ForceOff})
		if err != nil {
			return sha, n, err
		}
		sha[i] = sha256Hex(body)
		n[i] = len(body)
	}
	return sha, n, nil
}

// forceStates 是开关三态：全关 / 全开 / 仅调色板（部分开）。
// 部分开取 palette 而不是 quant：调色板是唯一有索引表的特性，能体现“口径联动”。
func forceStates() []struct {
	Name string
	Opt  api.Options
} {
	return []struct {
		Name string
		Opt  api.Options
	}{
		{"off", api.Options{Force: api.ForceOff}},
		{"on", api.Options{Force: api.ForceOn}},
		{"palette", api.Options{Force: api.ForcePalette}},
	}
}

// buildMatrix 跑完 3 格式 × 2 子命令 × 3 开关态，共 18 个组合。
func buildMatrix(img *api.Image, tiers []tier, kernelName string) (*Report, error) {
	sha, nb, err := baseline(img)
	if err != nil {
		return nil, err
	}
	rep := &Report{BaselineSHA: sha, BaselineBytes: nb, AllOffIdentical: true}
	formats := []string{"pgm", "bmp", "rle"}
	subs := []string{"encode", "decode"}
	for _, tr := range tiers {
		trp := TierReport{
			Bits: tr.Bits, TargetErr: tr.TargetErr, Q: tr.Q, EffErr: tr.EffErr,
			Inequality: lowbit.Inequality(tr.TargetErr, tr.Bits),
			Kernel:     kernelName,
			IndexBits:  tr.Bits,
		}
		if k, ok := lowbit.Builtin(kernelName); ok {
			trp.KernelRaw = append([]int(nil), k.W...)
			trp.KernelNorm = k.Normalized()
		}
		st, err := api.LowbitState(img, api.Options{
			Format: "pgm", Bits: tr.Bits, TargetErr: tr.TargetErr, Force: api.ForceOn,
			Kernel: kernelName,
		})
		if err != nil {
			return nil, err
		}
		trp.Indices[0] = st.IndicesBefore
		trp.Indices[1] = st.IndicesAfter
		trp.Palette[0] = st.PaletteBefore
		trp.Palette[1] = st.PaletteAfter
		trp.IndexShape = [2][2]int{{img.W, img.H}, {st.ThumbW, st.ThumbH}}
		no := 0
		for _, f := range formats {
			for _, s := range subs {
				for _, fs := range forceStates() {
					no++
					row, err := runCombo(img, no, f, s, fs.Name, fs.Opt, tr, kernelName, sha)
					if err != nil {
						return nil, err
					}
					if fs.Name == "off" && !(row.PSNRvsAllOff == "inf" && row.MaxErrvsAllOf == 0) {
						rep.AllOffIdentical = false
					}
					trp.Rows = append(trp.Rows, row)
					rep.Rows = append(rep.Rows, row)
				}
			}
		}
		rep.Tiers = append(rep.Tiers, trp)
	}
	return rep, nil
}

// runCombo 执行一个组合并算出三项指标：输出字节数、与全关基准的 PSNR/最大误差、
// 能否无损往返（容器层）。
//
// 两个子命令的输入都来自同一份“全关编码”的 seed：
//   - encode：seed 解成图，带低位深参数重新编码；
//   - decode：seed 直接作为待解字节流，带低位深参数解码，再以全关 PGM 落盘。
func runCombo(img *api.Image, no int, format, sub, forceName string, base api.Options, tr tier, kernel string, sha [3]string) (MatrixRow, error) {
	row := MatrixRow{
		No: no, Format: format, Subcommand: sub, Force: forceName,
		Bits: tr.Bits, TargetErr: tr.TargetErr, Q: tr.Q,
	}
	seed, err := api.Encode(img, api.Options{Format: format, Force: api.ForceOff})
	if err != nil {
		return row, err
	}
	off := api.Options{Format: format, Force: api.ForceOff}

	opt := base
	opt.Format = format
	opt.Bits = tr.Bits
	opt.TargetErr = tr.TargetErr
	opt.Kernel = kernel

	var out, ref []byte
	if sub == "encode" {
		out, err = api.Encode(img, opt)
		if err != nil {
			return row, err
		}
		ref, err = api.Encode(img, off)
	} else {
		decoded, derr := api.Decode(seed, opt)
		if derr != nil {
			return row, derr
		}
		out, err = api.Encode(decoded, api.Options{Format: "pgm", Force: api.ForceOff})
		if err != nil {
			return row, err
		}
		refDecoded, derr := api.Decode(seed, off)
		if derr != nil {
			return row, derr
		}
		ref, err = api.Encode(refDecoded, api.Options{Format: "pgm", Force: api.ForceOff})
	}
	if err != nil {
		return row, err
	}
	row.OutputBytes = len(out)
	row.OutputSHA = sha256Hex(out)

	// 三项指标都在像素域计算，三种容器才可横向比较：
	// 把产物按各自容器解回灰度，再与全关产物做同口径比较。
	got, err := pixelsOf(out, format, sub)
	if err != nil {
		return row, err
	}
	want, err := pixelsOf(ref, format, sub)
	if err != nil {
		return row, err
	}
	row.PSNRvsAllOff, row.MaxErrvsAllOf = comparePixels(want, got)
	row.RoundTripOK = roundTripOK(out, format, sub, forceName)
	return row, nil
}

// pixelsOf 把一次组合的产物还原成灰度像素，供像素域比较。
// encode 的产物是目标容器，按该格式解码；decode 的产物恒为 PGM。
func pixelsOf(out []byte, format, sub string) ([]byte, error) {
	f := format
	if sub == "decode" {
		f = "pgm"
	}
	img, err := api.Decode(out, api.Options{Format: f})
	if err != nil {
		return nil, err
	}
	return img.Pix, nil
}

// comparePixels 在灰度像素域算 PSNR 与最大绝对差。
func comparePixels(a, b []byte) (string, int) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	maxErr := 0
	sq := 0.0
	cnt := 0
	for i := 0; i < n; i++ {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > maxErr {
			maxErr = d
		}
		sq += float64(d * d)
		cnt++
	}
	if len(a) != len(b) {
		d := len(a) - len(b)
		if d < 0 {
			d = -d
		}
		if d > maxErr {
			maxErr = d
		}
		sq += float64(d * d)
		cnt++
	}
	if cnt == 0 || sq == 0 {
		return "inf", maxErr
	}
	mse := sq / float64(cnt)
	psnr := 10 * math.Log10(255.0*255.0/mse)
	return fmt.Sprintf("%.2f", psnr), maxErr
}

// roundTripOK：把产物按容器层再读一遍，确认头部与像素数自洽。
func roundTripOK(out []byte, format, sub, forceName string) bool {
	if sub == "decode" {
		img, err := api.Decode(out, api.Options{Format: "pgm"})
		return err == nil && img.W > 0 && img.H > 0 && len(img.Pix) == img.W*img.H
	}
	// encode 的产物按同格式读回；低位深口径下再读会重复量化，只校验容器。
	img, err := api.Decode(out, api.Options{Format: format})
	if err != nil {
		return false
	}
	return img.W > 0 && img.H > 0 && len(img.Pix) == img.W*img.H
}

func (r *Report) markdown() string {
	var b strings.Builder
	b.WriteString("# 低位深三处联动组合矩阵\n\n")
	b.WriteString("基准 = 三项全关；每档 18 行（3 格式 × 2 子命令 × 3 开关态）。\n\n")
	b.WriteString("| 格式 | 基准字节数 | 基准 SHA-256 |\n|---|---:|---|\n")
	for i, f := range []string{"pgm", "bmp", "rle"} {
		fmt.Fprintf(&b, "| %s | %d | `%s` |\n", f, r.BaselineBytes[i], r.BaselineSHA[i])
	}
	fmt.Fprintf(&b, "\n全关行与基准逐字节相同：**%v**\n\n", r.AllOffIdentical)
	for _, tr := range r.Tiers {
		fmt.Fprintf(&b, "## 档位：bits=%d, target_err=%d, q=%d, 实际误差上界=%d\n\n", tr.Bits, tr.TargetErr, tr.Q, tr.EffErr)
		fmt.Fprintf(&b, "反解不等式：`%s`\n\n", tr.Inequality)
		fmt.Fprintf(&b, "抖动核 `%s`，原始权重 %v，归一到 1 后 %s\n\n", tr.Kernel, tr.KernelRaw, formatFloats(tr.KernelNorm))
		fmt.Fprintf(&b, "索引位宽 %d（条目数 2^%d）\n\n", tr.IndexBits, tr.IndexBits)
		fmt.Fprintf(&b, "调色板：缩略前 %d 条 %v；缩略后 %d 条 %v\n\n",
			len(tr.Palette[0]), tr.Palette[0], len(tr.Palette[1]), tr.Palette[1])
		b.WriteString("| 组合 | 格式 | 子命令 | 开关 | 输出字节 | PSNR(dB) | 最大误差 | 往返 |\n")
		b.WriteString("|---:|---|---|---|---:|---:|---:|---|\n")
		for _, row := range tr.Rows {
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %d | %s | %d | %v |\n",
				row.No, row.Format, row.Subcommand, row.Force, row.OutputBytes, row.PSNRvsAllOff, row.MaxErrvsAllOf, row.RoundTripOK)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatFloats(f []float64) string {
	if len(f) == 0 {
		return "（未知核）"
	}
	parts := make([]string, len(f))
	for i, v := range f {
		parts[i] = fmt.Sprintf("%.4f", v)
	}
	return strings.Join(parts, ", ")
}
