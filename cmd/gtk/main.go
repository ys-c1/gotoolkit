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
	case "targets":
		os.Exit(runTargets(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode|targets --format pgm|bmp|rle|blk --in F --out F [--gain N] [--quality Q] [--block-size B] [--target-min B --target-max B | --target BYTES]")
	fmt.Fprintln(os.Stderr, "       gtk targets [--in a.pgm] [--block-size 8]  # 四档目标体积反解对账")
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	quality := fs.Int("quality", 0, "BLK 质量参数 1..100；0=最高(位深8)")
	blockSize := fs.Int("block-size", 0, "BLK 分块边长 B；0=默认 8")
	target := fs.Int("target", 0, "点目标体积(字节)，按 [T,T] 反解质量，通常走最接近兜底")
	targetMin := fs.Int("target-min", 0, "目标体积区间下界(字节)")
	targetMax := fs.Int("target-max", 0, "目标体积区间上界(字节)")
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

	opt := api.Options{
		Format:         *format,
		Gain:           *gain,
		Quality:        *quality,
		BlockSize:      *blockSize,
		TargetBytes:    *target,
		TargetMinBytes: *targetMin,
		TargetMaxBytes: *targetMax,
	}

	// 指定了目标体积/区间：二分反解质量参数，落盘采用参数下的真实编码。
	if *target > 0 || *targetMin > 0 || *targetMax > 0 {
		rep, serr := api.SolveQuality(img, opt)
		if serr != nil {
			fmt.Fprintln(os.Stderr, "按目标体积反解质量失败:", serr)
			return 1
		}
		if werr := os.WriteFile(*out, rep.Body, 0o644); werr != nil {
			fmt.Fprintln(os.Stderr, "写出失败:", werr)
			return 1
		}
		verdict := "落入目标区间"
		if !rep.InRange {
			verdict = fmt.Sprintf("区间外，已取最接近 Q=%d，差距 %d B (%.2f%%)",
				rep.ClosestQuality, rep.GapBytes, rep.GapPercent)
		}
		// 解出的参数与实测体积放在同一行。
		fmt.Printf("encoded %dx%d -> blk Q=%d d=%d 实测体积=%d bytes 目标区间=[%d,%d] %s -> %s\n",
			img.W, img.H, rep.Quality, rep.Depth, rep.Bytes, rep.MinBytes, rep.MaxBytes, verdict, *out)
		return 0
	}

	// 未指定目标：与现在完全一致的直接编码路径。
	body, err := api.Encode(img, opt)
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
