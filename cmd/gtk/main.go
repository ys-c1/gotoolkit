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
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N] [--mask N]")
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	// -1 为缺省：不启用掩码，输出与无掩码版本逐字节一致；0..255 表示以该灰度为透明掩码。
	mask := fs.Int("mask", -1, "透明掩码灰度值，0..255 启用，-1 关闭")
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
	// 输入 PGM 不带掩码尾段，读取时固定掩码关闭；掩码只作用于本次输出编码。
	img, err := api.Decode(raw, api.Options{Format: "pgm", Mask: -1})
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}
	body, err := api.Encode(img, api.Options{Format: *format, Gain: *gain, Mask: *mask})
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	if *mask >= 0 {
		fmt.Printf("encoded %dx%d -> %s (%d bytes), mask=%d, transparent=%d\n",
			img.W, img.H, *format, len(body), *mask, countTransparent(img.Pix, uint8(*mask)))
	} else {
		fmt.Printf("encoded %dx%d -> %s (%d bytes)\n", img.W, img.H, *format, len(body))
	}
	return 0
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输入格式")
	in := fs.String("in", "", "输入文件")
	out := fs.String("out", "", "输出 PGM")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	// 与 encode 对称：-1 关闭（缺省）；0..255 时要求输入流带同一掩码值的尾段。
	mask := fs.Int("mask", -1, "透明掩码灰度值，0..255 启用，-1 关闭")
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
	img, err := api.Decode(raw, api.Options{Format: *format, Gain: *gain, Mask: *mask})
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}
	// 重新编码为 PGM 时透传同一掩码，透明尾段随输出保留；掩码关闭则输出纯 P5。
	body, err := api.Encode(img, api.Options{Format: "pgm", Mask: *mask})
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	if *mask >= 0 {
		fmt.Printf("decoded %s -> %dx%d pgm (%d bytes), mask=%d, transparent=%d\n",
			*format, img.W, img.H, len(body), *mask, countAlphaZero(img.Alpha))
	} else {
		fmt.Printf("decoded %s -> %dx%d pgm (%d bytes)\n", *format, img.W, img.H, len(body))
	}
	return 0
}

// countTransparent 统计原始像素中等于掩码值的个数（编码侧透明判定）。
func countTransparent(pix []uint8, maskValue uint8) int {
	n := 0
	for _, v := range pix {
		if v == maskValue {
			n++
		}
	}
	return n
}

// countAlphaZero 统计解码后 alpha 中透明（0）的个数。
func countAlphaZero(alpha []uint8) int {
	n := 0
	for _, a := range alpha {
		if a == 0 {
			n++
		}
	}
	return n
}
