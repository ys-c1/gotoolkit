package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

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
	case "matrix":
		os.Exit(runMatrix(os.Args[2:]))
	case "dump":
		os.Exit(runDump(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  gtk encode --format pgm|bmp|rle --in F --out F [--gain N]
             [--bits D] [--force off|on|quant|dither|palette] [--target-err E]
             [--kernel NAME|custom:...] [--palette N]
  gtk decode --format pgm|bmp|rle --in F --out F [同上低位深参数]
  gtk matrix --in F --out PREFIX [--bits D] [--target-err E] [--kernel NAME]
  gtk dump   --in F [--bits D] [--target-err E] [--kernel NAME]`)
}

func lowbitFlags(fs *flag.FlagSet) (bits, tgt *int, force, kernel *string, pal *int) {
	bits = fs.Int("bits", 0, "目标位深 1..8，0 表示关闭")
	force = fs.String("force", "", "off|on|quant|dither|palette，默认由 --bits 决定")
	tgt = fs.Int("target-err", 0, "目标最大误差 E，0 表示按位深推导")
	kernel = fs.String("kernel", "", "抖动核：floyd-steinberg|bayer|custom:...")
	pal = fs.Int("palette", 0, "调色板条目数，0 表示 2^bits")
	return
}

func optionsFrom(format string, bits, tgt, pal int, force, kernel string) api.Options {
	return api.Options{
		Format:      format,
		Bits:        bits,
		Force:       api.Force(force),
		TargetErr:   tgt,
		Kernel:      kernel,
		PaletteSize: pal,
	}
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "错误:", err)
	switch {
	case errors.Is(err, api.ErrCodeBitDepth),
		errors.Is(err, api.ErrCodeKernelZero),
		errors.Is(err, api.ErrCodeKernel),
		errors.Is(err, api.ErrCodePalette):
		return 3
	case errors.Is(err, api.ErrBadOption):
		return 2
	default:
		return 1
	}
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	bits, tgt, force, kernel, pal := lowbitFlags(fs)
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
	opt := optionsFrom(*format, *bits, *tgt, *pal, *force, *kernel)
	opt.Gain = *gain
	body, err := api.Encode(img, opt)
	if err != nil {
		return fail(err)
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
	bits, tgt, force, kernel, pal := lowbitFlags(fs)
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
	opt := optionsFrom(*format, *bits, *tgt, *pal, *force, *kernel)
	opt.Gain = *gain
	img, err := api.Decode(raw, opt)
	if err != nil {
		return fail(err)
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

// runDump 打印低位深全部中间结果，重点是缩略前/后两张索引表的逐条列举。
func runDump(args []string) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	bits, tgt, force, kernel, pal := lowbitFlags(fs)
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
	opt := optionsFrom("pgm", *bits, *tgt, *pal, *force, *kernel)
	p, err := api.Params(opt)
	if err != nil {
		return fail(err)
	}
	st, err := api.LowbitState(img, opt)
	if err != nil {
		return fail(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(st); err != nil {
			fmt.Fprintln(os.Stderr, "写出失败:", err)
			return 1
		}
		return 0
	}
	dumpText(img, p, st)
	return 0
}

func runMatrix(args []string) int {
	fs := flag.NewFlagSet("matrix", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出前缀：<out>.md 表格，<out>.json 明细")
	bits := fs.Int("bits", 0, "位深；0 表示两档各测一次")
	tgt := fs.Int("target-err", 0, "目标误差 E；0 表示按位深推导")
	kernel := fs.String("kernel", "floyd-steinberg", "抖动核")
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
	tiers, err := selectTiers(*bits, *tgt)
	if err != nil {
		return fail(err)
	}
	rep, err := buildMatrix(img, tiers, *kernel)
	if err != nil {
		return fail(err)
	}
	md := strings.TrimSuffix(*out, ".md")
	if err := os.WriteFile(md+".md", []byte(rep.markdown()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return 1
	}
	if err := os.WriteFile(md+".json", append(body, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("matrix -> %s.md, %s.json (%d 行)\n", md, md, len(rep.Rows))
	return 0
}
