package main

import (
	"flag"
	"fmt"
	"os"

	"gotoolkit/api"
	"gotoolkit/internal/quant"
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
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N] [--levels L] [--dither P|--auto-dither]")
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	levels := fs.Int("levels", 0, "调色板桶数 2..256，0 表示不量化")
	dither := fs.Int("dither", 0, "抖动强度百分比 0..100")
	auto := fs.Bool("auto-dither", false, "按桶数自动选用加权误差最小的抖动强度")
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
	opt := api.Options{Format: *format, Gain: *gain, Levels: *levels, Dither: *dither, AutoDither: *auto}
	body, err := api.Encode(img, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("encoded %dx%d -> %s (%d bytes)\n", img.W, img.H, *format, len(body))
	if *levels != 0 {
		tau := float64(*dither) / 100
		if *auto {
			plan, err := api.OptimalDither(*levels)
			if err != nil {
				fmt.Fprintln(os.Stderr, "计算配比失败:", err)
				return 1
			}
			tau = plan.Tau
		}
		if err := reportParts(img, *levels, tau); err != nil {
			fmt.Fprintln(os.Stderr, "统计失败:", err)
			return 1
		}
	}
	return 0
}

// reportParts 打印量化后的色块/噪声分解与加权误差。
func reportParts(img *api.Image, levels int, tau float64) error {
	parts := quant.Predict(api.Histogram(img), levels, tau)
	fmt.Printf("levels=%d dither=%.0f%% banding=%.3f noise=%.3f weighted=%.3f\n",
		levels, tau*100, parts.Banding, parts.Noise, parts.Weighted())
	return nil
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
