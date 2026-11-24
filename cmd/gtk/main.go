package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
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
	default:
		usage()
		os.Exit(2)
	}
}

// 处理链顺序是固定语义，不提供命令行开关：掩码先行、伽马其次、抖动最后。
const chainLine = "处理链顺序（固定）：[1] 掩码 --mask -> [2] 伽马 --gamma -> [3] 抖动 --dither"

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N] [--mask V[,V...]] [--gamma V] [--dither N]")
	fmt.Fprintln(os.Stderr, chainLine)
}

// chainFlags 汇总三段链加既有增益的公共参数。
type chainFlags struct {
	gain   *int
	mask   *string
	gamma  *float64
	dither *int
	order  *string
}

func addChainFlags(fs *flag.FlagSet) chainFlags {
	// 把固定顺序写进 --help：encode 与 decode 都能直接看到。
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "用法: gtk %s --format pgm|bmp|rle --in F --out F [--gain N] [--mask V[,V...]] [--gamma V] [--dither N] [--order A>B>C]\n", fs.Name())
		fmt.Fprintln(fs.Output(), chainLine)
		fmt.Fprintln(fs.Output(), "参数:")
		fs.PrintDefaults()
	}
	return chainFlags{
		gain:   fs.Int("gain", 0, "亮度增益百分比 [-100,100]，默认 0"),
		mask:   fs.String("mask", "", "透明掩码灰度值，逗号分隔（如 0,255），默认关闭"),
		gamma:  fs.Float64("gamma", 0, "伽马指数，0 关闭；开启须为正的有限值"),
		dither: fs.Int("dither", 0, "4x4 有序抖动幅度 [0,255]，默认 0（关闭）"),
		order:  fs.String("order", strings.Join(api.CanonicalOrder, ">"), "处理链顺序，只允许三步的一个排列；默认即记录顺序"),
	}
}

// options 把命令行参数转成 api.Options，并把解析错误变成带错误码的报错。
func (c chainFlags) options(format string) (api.Options, error) {
	vals, err := parseMaskList(*c.mask)
	if err != nil {
		return api.Options{}, err
	}
	return api.Options{
		Format:     format,
		Gain:       *c.gain,
		MaskValues: vals,
		Gamma:      *c.gamma,
		Dither:     *c.dither,
	}, nil
}

func parseMaskList(s string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		v, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("%w: --mask 含非整数 %q", api.ErrBadOption, p)
		}
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("%w: --mask 值 %d 超出 [0,255]", api.ErrBadOption, v)
		}
		out = append(out, v)
	}
	return out, nil
}

// fail 打印错误与稳定错误码并返回退出码。
func fail(prefix string, err error) int {
	code := api.CodeOf(err)
	fmt.Fprintf(os.Stderr, "%s: %v (错误码 %d)\n", prefix, err, code)
	if code == api.CodeNone {
		return 1
	}
	return int(code)
}

// parseOrderList 把 "mask>gamma>dither" 拆成三步名字序列。
func parseOrderList(s string) ([]string, error) {
	names := strings.Split(s, ">")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return names, nil
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	cf := addChainFlags(fs)
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
		return fail("解码失败", err)
	}
	opt, err := cf.options(*format)
	if err != nil {
		return fail("参数非法", err)
	}
	order, err := parseOrderList(*cf.order)
	if err != nil {
		return fail("参数非法", err)
	}
	body, err := api.EncodeWithOrder(img, opt, order)
	if err != nil {
		return fail("编码失败", err)
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("encoded %dx%d -> %s (%d bytes) chain=%s\n",
		img.W, img.H, *format, len(body), strings.Join(order, ">"))
	return 0
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输入格式")
	in := fs.String("in", "", "输入文件")
	out := fs.String("out", "", "输出 PGM")
	cf := addChainFlags(fs)
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
	opt, err := cf.options(*format)
	if err != nil {
		return fail("参数非法", err)
	}
	img, err := api.Decode(raw, opt)
	if err != nil {
		return fail("解码失败", err)
	}
	body, err := api.Encode(img, api.Options{Format: "pgm"})
	if err != nil {
		return fail("编码失败", err)
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("decoded %s -> %dx%d pgm (%d bytes) chain=%s\n",
		*format, img.W, img.H, len(body), strings.Join(api.CanonicalOrder, ">"))
	return 0
}
