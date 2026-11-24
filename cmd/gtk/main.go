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
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N] [--sharpen [--k N] [--sharpen-gain G]]")
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	doSharpen := fs.Bool("sharpen", false, "开启锐化（反解过冲为零的最大增益与核宽）")
	kFlag := fs.Int("k", 0, "锐化核宽（0=自动反解）")
	sgFlag := fs.Float64("sharpen-gain", 0, "覆盖反解增益（0=采用反解值）")
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
	opt := api.Options{Format: *format, Gain: *gain}
	if *doSharpen {
		opt.Sharpen, opt.SharpenK, opt.SharpenGain = true, *kFlag, *sgFlag
		sr, err := api.SolveSharpen(img, *kFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "反解失败:", err)
			return 1
		}
		gain := sr.Gain
		if *sgFlag > 0 {
			gain = *sgFlag
		}
		over, overPix, boost := api.MeasureSharpen(img, sr, gain)
		flag := ""
		if overPix > 0 {
			flag = " [超限]"
		}
		fmt.Printf("sharpen k=%d D=%.6f gain=%.6f boost=%.3f over=%.6g overPix=%d%s\n",
			sr.K, sr.D, gain, boost, over, overPix, flag)
	}
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
