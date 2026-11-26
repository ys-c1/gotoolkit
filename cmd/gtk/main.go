package main

import (
	"flag"
	"fmt"
	"os"

	"gotoolkit/api"
	"gotoolkit/internal/codec"
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
	case "analyze":
		os.Exit(runAnalyze(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle|pred --in F --out F [--gain N] [--pred-order K] [--auto-order]")
	fmt.Fprintln(os.Stderr, "       gtk analyze --in F.pgm [--max-order K]")
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	predOrder := fs.Int("pred-order", 4, "pred 格式预测阶数 0..4")
	autoOrder := fs.Bool("auto-order", false, "阶数开关：仅在残差方差比 <= 1 时加阶")
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
	body, err := api.Encode(img, api.Options{Format: *format, Gain: *gain, PredOrder: *predOrder, AutoOrder: *autoOrder})
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

// runAnalyze 打印各阶残差的方差、方差比、熵与开关判定，
// 并给出开关开/关两种状态下的熵差。
func runAnalyze(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM")
	maxOrder := fs.Int("max-order", codec.MaxOrder, "考察的最大阶数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in")
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
	stats, chosen := codec.ChooseOrder(img.W, img.H, img.Pix, *maxOrder)
	fmt.Printf("%s %dx%d, max-order=%d\n", *in, img.W, img.H, *maxOrder)
	fmt.Printf("%-5s %-12s %-10s %-14s %s\n", "order", "variance", "varRatio", "entropy(bit/px)", "switch")
	for _, st := range stats {
		sw := "accept"
		if !st.Accepted {
			sw = fmt.Sprintf("reject (varRatio %.3f > 1)", st.Ratio)
		}
		fmt.Printf("%-5d %-12.2f %-10.3f %-14.4f %s\n", st.Order, st.Variance, st.Ratio, st.Entropy, sw)
	}
	hOn := stats[chosen].Entropy
	hOff := stats[len(stats)-1].Entropy
	fmt.Printf("开关开:  选阶 %d, 熵 %.4f bit/px\n", chosen, hOn)
	fmt.Printf("开关关:  固定 %d 阶, 熵 %.4f bit/px\n", stats[len(stats)-1].Order, hOff)
	fmt.Printf("熵差 ΔH = %.4f bit/px\n", hOff-hOn)
	return 0
}
