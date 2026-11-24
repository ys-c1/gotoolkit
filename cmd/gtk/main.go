package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"gotoolkit/api"
)

// 退出码：除 1（IO）与 2（用法）外，两组亮度链路错误各有独立码，
// 与 api 包里的哨兵错误一一对应，错误文本同时点名出问题的字段。
const (
	exitIO        = 1
	exitUsage     = 2
	exitGammaZero = 3 // api.ErrGammaZero，字段 Gamma
	exitMaskRange = 4 // api.ErrMaskRange，字段 Mask
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitUsage)
	}
	switch os.Args[1] {
	case "encode":
		os.Exit(runEncode(os.Args[2:]))
	case "decode":
		os.Exit(runDecode(os.Args[2:]))
	default:
		usage()
		os.Exit(exitUsage)
	}
}

// 亮度链路顺序说明：与 api.Options 的字段注释、README 的“亮度链路顺序”一节一致。
const chainHelp = `亮度链路固定顺序（编码与解码两侧一致）：
  [1/3] --gain N    线性增益百分比 [-100,100]；整数截断，逐像素钳到 [0,255]，不产生溢出
  [2/3] --gamma V   伽马目标平均亮度 (0,255]；系数由 --gain 之后的均值反解
  [3/3] --mask F    透明掩码 PGM（8 位，尺寸须与图相同）；0 全透明，255 原值
掩码与增益同时开启时：先 --gain，再 --gamma，最后 --mask；掩码作用在增益与伽马之后的像素上。
退出码：2 用法/参数越界，3 伽马为零（ErrGammaZero，字段 Gamma），4 掩码超范围（ErrMaskRange，字段 Mask）`

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N] [--gamma V] [--mask F]")
	fmt.Fprintln(os.Stderr, chainHelp)
}

type common struct {
	format string
	in     string
	out    string
	gain   int
	gamma  float64
	mask   []uint8
}

func parse(fs *flag.FlagSet, args []string) (*common, int) {
	c := &common{}
	fs.StringVar(&c.format, "format", "pgm", "pgm|bmp|rle")
	fs.StringVar(&c.in, "in", "", "输入文件")
	fs.StringVar(&c.out, "out", "", "输出文件")
	fs.IntVar(&c.gain, "gain", 0, "[1/3] 线性增益百分比 [-100,100]")
	fs.Float64Var(&c.gamma, "gamma", 0, "[2/3] 伽马目标平均亮度 (0,255]")
	maskPath := fs.String("mask", "", "[3/3] 透明掩码 PGM")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: gtk %s --format pgm|bmp|rle --in F --out F [--gain N] [--gamma V] [--mask F]\n", fs.Name())
		fmt.Fprintln(os.Stderr, chainHelp)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return nil, exitUsage
	}
	if c.in == "" || c.out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return nil, exitUsage
	}
	if *maskPath != "" {
		m, err := readMask(*maskPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取掩码失败:", err)
			return nil, exitIO
		}
		c.mask = m
	}
	return c, 0
}

// readMask 把 P5 灰度图读成掩码像素。
func readMask(path string) ([]uint8, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := api.Decode(raw, api.Options{Format: "pgm"})
	if err != nil {
		return nil, err
	}
	return img.Pix, nil
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	c, code := parse(fs, args)
	if c == nil {
		return code
	}
	raw, err := os.ReadFile(c.in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return exitIO
	}
	img, err := api.Decode(raw, api.Options{Format: "pgm"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return exitIO
	}
	opt := api.Options{Format: c.format, Gain: c.gain, Gamma: c.gamma, Mask: c.mask}
	body, err := api.Encode(img, opt)
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(c.out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return exitIO
	}
	fmt.Printf("encoded %dx%d -> %s (%d bytes) order=gain>gamma>mask\n", img.W, img.H, c.format, len(body))
	return 0
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	c, code := parse(fs, args)
	if c == nil {
		return code
	}
	raw, err := os.ReadFile(c.in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return exitIO
	}
	opt := api.Options{Format: c.format, Gain: c.gain, Gamma: c.gamma, Mask: c.mask}
	img, err := api.Decode(raw, opt)
	if err != nil {
		return fail(err)
	}
	body, err := api.Encode(img, api.Options{Format: "pgm"})
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(c.out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return exitIO
	}
	fmt.Printf("decoded %s -> %dx%d pgm (%d bytes) order=gain>gamma>mask\n", c.format, img.W, img.H, len(body))
	return 0
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "失败:", err)
	switch {
	case errors.Is(err, api.ErrGammaZero):
		return exitGammaZero
	case errors.Is(err, api.ErrMaskRange):
		return exitMaskRange
	case errors.Is(err, api.ErrBadOption), errors.Is(err, api.ErrUnknownFormat):
		return exitUsage
	default:
		return exitIO
	}
}
