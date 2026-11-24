package main

import (
	"flag"
	"fmt"
	"os"

	"gotoolkit/api"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "encode":
		os.Exit(runEncode(os.Args[2:]))
	case "decode":
		os.Exit(runDecode(os.Args[2:]))
	case "scale":
		os.Exit(runScale(os.Args[2:]))
	case "gamma-lab":
		os.Exit(runGammaLab(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N]")
	fmt.Fprintln(os.Stderr, "       gtk scale --in F.pgm --out F.pgm --ratio 0.5|0.25|0.125 [--space encoded|gamma-dual|compensated|linear] [--gamma 2.2]")
	fmt.Fprintln(os.Stderr, "       gtk gamma-lab [--gamma 2.2] [--tolerance 1]")
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	img, err := api.Decode(raw, api.Options{Format: "pgm"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}
	body, err := api.Encode(img, api.Options{Format: *format, Gain: *gain})
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("encoded %dx%d -> %s (%d bytes)\n", img.W, img.H, *format, len(body))
	return 0
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输入格式")
	in := fs.String("in", "", "输入文件")
	out := fs.String("out", "", "输出 PGM")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	img, err := api.Decode(raw, api.Options{Format: *format, Gain: *gain})
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}
	body, err := api.Encode(img, api.Options{Format: "pgm"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("decoded %s -> %dx%d pgm (%d bytes)\n", *format, img.W, img.H, len(body))
	return 0
}

func runScale(args []string) int {
	fs := flag.NewFlagSet("scale", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出 PGM")
	ratio := fs.Float64("ratio", 0.5, "缩小比，必须为 1/N")
	space := fs.String("space", "encoded", "encoded|gamma-dual|compensated|linear")
	gamma := fs.Float64("gamma", 2.2, "伽马指数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	img, err := api.Decode(raw, api.Options{Format: "pgm"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}
	scaled, err := api.Scale(img, api.Options{
		Ratio:      *ratio,
		Gamma:      *gamma,
		ScaleSpace: *space,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "缩放失败:", err)
		return 1
	}
	body, err := api.Encode(scaled, api.Options{Format: "pgm"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("scaled %dx%d -> %dx%d ratio=%.6g space=%s gamma=%.2f (%d bytes)\n",
		img.W, img.H, scaled.W, scaled.H, *ratio, *space, *gamma, len(body))
	return 0
}

func runGammaLab(args []string) int {
	fs := flag.NewFlagSet("gamma-lab", flag.ContinueOnError)
	gamma := fs.Float64("gamma", 2.2, "伽马指数")
	tolerance := fs.Float64("tolerance", 1, "8-bit 码值容差")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rows, err := api.GammaLab(api.Options{Gamma: *gamma, Tolerance: *tolerance})
	if err != nil {
		fmt.Fprintln(os.Stderr, "复算失败:", err)
		return 1
	}

	worst := 0
	fmt.Printf("gamma=%.2f tolerance=%.3g code; dark reference C_d=32\n", *gamma, *tolerance)
	for _, r := range rows {
		frameText := ""
		for _, f := range r.Frames {
			frameText += fmt.Sprintf("%s->%dx%d Bmax=%d Rmax=%d; ",
				f.Name, f.OutW, f.OutH, f.MaxEncodedBias, f.MaxResidual)
			if f.MaxResidual > worst {
				worst = f.MaxResidual
			}
		}
		fmt.Printf("rho=1/%d (%.3f): K=%.6f; B(32)=%+.3f code; R(32)=%+d code; %spass=%v\n",
			r.Denom, r.Ratio, r.Compensation, r.BiasAtDark32, r.ResidualAtDark32, frameText, r.WithinTolerance)
	}
	verdict := "PASS"
	if float64(worst) > *tolerance {
		verdict = "FAIL"
	}
	fmt.Printf("tolerance check: worst residual=%d code %s tau=%.3g -> %s\n",
		worst, comparisonLE(float64(worst), *tolerance), *tolerance, verdict)
	if verdict == "FAIL" {
		return 1
	}
	return 0
}

func comparisonLE(got, tol float64) string {
	if got <= tol {
		return "<="
	}
	return ">"
}
