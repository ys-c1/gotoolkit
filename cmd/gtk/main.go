package main

import (
	"flag"
	"fmt"
	"os"

	"gotoolkit/api"
)

func parseBudget(s string) (int64, error) {
	switch s {
	case "", "0", "none":
		return 0, nil
	case "tight":
		return api.BudgetTight, nil
	case "generous":
		return api.BudgetGenerous, nil
	}
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return 0, fmt.Errorf("预算应为 tight|generous|字节数: %q", s)
	}
	if v < 0 {
		return 0, fmt.Errorf("预算不能为负")
	}
	return v, nil
}

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
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N]")
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
	budgetFlag := fs.String("mem-budget", "0", "内存预算: tight|generous|字节数，0 不限")
	bandRows := fs.Int("band-rows", 0, "分块解码每块行数，0 自动")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	budget, err := parseBudget(*budgetFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "预算非法:", err)
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	opt := api.Options{Format: *format, Gain: *gain, MemBudget: budget, BandRows: *bandRows}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建失败:", err)
		return 1
	}
	defer f.Close()
	stats, err := api.DecodeToPGM(raw, opt, f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}
	fmt.Printf("decoded %s -> %dx%d pgm path=%s bandRows=%d modelPeak=%d budget=%d\n",
		*format, stats.W, stats.H, stats.Path, stats.BandRows, stats.ModelPeak, budget)
	return 0
}
