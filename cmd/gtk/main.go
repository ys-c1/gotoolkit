package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gotoolkit/api"
	"gotoolkit/internal/quant"
	"gotoolkit/internal/quant/fixture"
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
	case "sample":
		os.Exit(runSample(os.Args[2:]))
	case "recon":
		os.Exit(runRecon(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N]")
	fmt.Fprintln(os.Stderr, "       gtk sample --out F")
	fmt.Fprintln(os.Stderr, "       gtk recon --in F [--bits B]")
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

// runSample 写出一张确定性合成灰度图（P5），供 recon 演示使用。
func runSample(args []string) int {
	fs := flag.NewFlagSet("sample", flag.ContinueOnError)
	out := fs.String("out", "", "输出 PGM")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --out")
		return 2
	}
	w, hh, pix := fixture.Image()
	body, err := api.Encode(&api.Image{W: w, H: hh, Pix: pix}, api.Options{Format: "pgm"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("sample %dx%d -> %s\n", w, hh, *out)
	return 0
}

// runRecon 对输入 PGM 并排跑中点重建与条件期望重建，打印迭代次数与误差。
// 任一档位迭代未收敛都会报错退出，不打印半成品结果。
func runRecon(args []string) int {
	fs := flag.NewFlagSet("recon", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM")
	bitsArg := fs.String("bits", "3,4,5", "逗号分隔的位深列表")
	tol := fs.Float64("tol", quant.DefaultTol, "不动点迭代收敛阈值")
	maxIter := fs.Int("maxiter", quant.DefaultMaxIter, "不动点迭代上限")
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
	h := quant.FromPixels(img.Pix)
	var bitsList []int
	for _, s := range strings.Split(*bitsArg, ",") {
		b, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			fmt.Fprintln(os.Stderr, "位深非法:", s)
			return 2
		}
		bitsList = append(bitsList, b)
	}

	fmt.Printf("样本 %dx%d，非零灰度 %d 档\n", img.W, img.H, nonzeroLevels(h))
	// 先全部算完再打印：任一档失败就整张表都不输出，避免看上去像有结果。
	type row struct {
		bits               int
		midIters, expIters int
		midMSE, expMSE     float64
	}
	rows := make([]row, 0, len(bitsList))
	for _, b := range bitsList {
		mid, err := quant.Midpoint(b)
		if err != nil {
			fmt.Fprintln(os.Stderr, "中点重建失败:", err)
			return 1
		}
		opt, err := quant.Optimal(h, b, *tol, *maxIter)
		if err != nil {
			fmt.Fprintln(os.Stderr, "条件期望重建失败:", err)
			return 1
		}
		rows = append(rows, row{
			bits:     b,
			midIters: mid.Iterations,
			expIters: opt.Iterations,
			midMSE:   quant.DistortionRounded(h, mid.Levels),
			expMSE:   quant.DistortionRounded(h, opt.Levels),
		})
	}
	fmt.Printf("%-4s %-12s %-5s %-14s %-12s %-5s %-14s\n",
		"位深", "重建", "迭代", "MSE", "重建", "迭代", "MSE")
	for _, r := range rows {
		fmt.Printf("%-4d %-12s %-5d %-14.6f %-12s %-5d %-14.6f\n",
			r.bits, "midpoint", r.midIters, r.midMSE,
			"expectation", r.expIters, r.expMSE)
	}
	return 0
}

func nonzeroLevels(h quant.Histogram) int {
	n := 0
	for _, c := range h {
		if c != 0 {
			n++
		}
	}
	return n
}
