package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"gotoolkit/api"
	"gotoolkit/internal/batch"
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
	case "batch-encode":
		os.Exit(runBatch(batch.Encode, os.Args[2:]))
	case "batch-decode":
		os.Exit(runBatch(batch.Decode, os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N]")
	fmt.Fprintln(os.Stderr, "       gtk batch-encode|batch-decode --format pgm|bmp|rle --in DIR --out DIR [--gain N]")
}

// 批量子命令退出码约定：
//
//	0 全部文件成功
//	1 有文件失败（失败隔离，其余已处理）
//	2 用法/参数错误
//	3 输入目录为空（无候选文件）
//	4 权限不足（无法遍历目录或读写文件）
func runBatch(mode batch.Mode, args []string) int {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	format := fs.String("format", "pgm", "目标/输入格式")
	in := fs.String("in", "", "输入目录")
	out := fs.String("out", "", "输出目录")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	rep, err := batch.Run(mode, *format, *gain, *in, *out)
	if err != nil {
		switch {
		case errors.Is(err, batch.ErrPermission):
			fmt.Fprintln(os.Stderr, err)
			return 4
		case errors.Is(err, batch.ErrDirEmpty):
			fmt.Fprintln(os.Stderr, err)
			return 3
		default:
			fmt.Fprintln(os.Stderr, "批量处理失败:", err)
			return 2
		}
	}
	if err := batch.RenderReport(os.Stdout, rep); err != nil {
		fmt.Fprintln(os.Stderr, "输出汇总失败:", err)
		return 1
	}
	if rep.Failed > 0 {
		return 1
	}
	return 0
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
