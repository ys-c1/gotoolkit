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
	case "kernels":
		os.Exit(runKernels())
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N] [--dither-strength X --dither-kernel NAME --dither-levels N]")
	fmt.Fprintln(os.Stderr, "       gtk kernels            # 打印内置扩散核的原始/归一化权重表")
}

// addDitherFlags 给 encode / decode 加同一组抖动旗标，保证两条子命令
// 都能设置公开参数结构体里的字段（不靠内部开关）。
func addDitherFlags(fs *flag.FlagSet, strength *float64, kernel *string, levels *int, printKernel *bool) {
	fs.Float64Var(strength, "dither-strength", 0, "误差扩散强度，0 关闭（默认）；负数报错")
	fs.StringVar(kernel, "dither-kernel", "floyd-steinberg", "扩散核：floyd-steinberg | atkinson")
	fs.IntVar(levels, "dither-levels", 0, "目标色阶数，0 表示缺省 8；显式给值须在 [2,256]")
	if printKernel != nil {
		fs.BoolVar(printKernel, "print-kernel", false, "编码前把归一化后的权重表打印到 stdout")
	}
}

// resolveDither 把命令行两个旗标解析成 api.Options 里的字段。
// 强度为 0 时核留空即可；强度为正时核必须能解析。
func resolveDither(strength float64, kernel string, levels int) (float64, api.DitherKernel, int, error) {
	if strength == 0 {
		return 0, api.DitherKernel{}, levels, nil
	}
	k, err := api.LookupDitherKernel(kernel)
	if err != nil {
		return 0, api.DitherKernel{}, 0, err
	}
	if k.IsZero() {
		return 0, api.DitherKernel{}, 0, fmt.Errorf("强度 %v > 0 但核为空", strength)
	}
	return strength, k, levels, nil
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	var strength float64
	var kernel string
	var levels int
	var printKernel bool
	addDitherFlags(fs, &strength, &kernel, &levels, &printKernel)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	ds, dk, dl, err := resolveDither(strength, kernel, levels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "抖动参数非法:", err)
		return 2
	}
	if printKernel && ds > 0 {
		fmt.Print(dk.WeightTable())
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
	body, err := api.Encode(img, api.Options{Format: *format, Gain: *gain, DitherStrength: ds, DitherKernel: dk, DitherLevels: dl})
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

// runKernels 打印内置核的权重表；报告里的归一化权重表就是它的输出。
func runKernels() int {
	for i, k := range api.BuiltinDitherKernels() {
		if i > 0 {
			fmt.Println()
		}
		fmt.Print(k.WeightTable())
	}
	return 0
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输入格式")
	in := fs.String("in", "", "输入文件")
	out := fs.String("out", "", "输出 PGM")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	var strength float64
	var kernel string
	var levels int
	addDitherFlags(fs, &strength, &kernel, &levels, nil)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	ds, dk, dl, err := resolveDither(strength, kernel, levels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "抖动参数非法:", err)
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	img, err := api.Decode(raw, api.Options{Format: *format, Gain: *gain, DitherStrength: ds, DitherKernel: dk, DitherLevels: dl})
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
