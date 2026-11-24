package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"runtime"

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
	// 大图编码时不再需要原始输入缓冲，先释放再编码。
	raw = nil
	runtime.GC()
	dst, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建失败:", err)
		return 1
	}
	// 64 KiB 缓冲吸收 RLE 的逐段小写入，不改变峰值量级。
	bw := bufio.NewWriterSize(dst, 64*1024)
	if err := api.EncodeTo(bw, img, api.Options{Format: *format, Gain: *gain}); err != nil {
		dst.Close()
		os.Remove(*out)
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := bw.Flush(); err != nil {
		dst.Close()
		os.Remove(*out)
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	info, err := dst.Stat()
	if err != nil {
		dst.Close()
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	if err := dst.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	n := info.Size()
	fmt.Printf("encoded %dx%d -> %s (%d bytes)\n", img.W, img.H, *format, n)
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
