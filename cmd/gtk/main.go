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
	case "chroma":
		os.Exit(runChroma(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N]")
	fmt.Fprintln(os.Stderr, "       gtk chroma --in F.ppm --out F.ppm --recon bilinear|adaptive")
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

// runChroma 对 P6 PPM 彩色图做 4:2:0 色度降采样并按所选模式重建，输出重建后的 PPM。
func runChroma(args []string) int {
	fs := flag.NewFlagSet("chroma", flag.ContinueOnError)
	in := fs.String("in", "", "输入 P6 PPM")
	out := fs.String("out", "", "输出重建 PPM")
	recon := fs.String("recon", "", "重建模式 bilinear|adaptive")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" || *recon == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in、--out 或 --recon")
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	img, err := api.DecodePPM(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PPM 解码失败:", err)
		return 1
	}
	rec, err := api.ChromaRoundTrip(img, api.Options{ChromaRecon: *recon})
	if err != nil {
		fmt.Fprintln(os.Stderr, "色度重建失败:", err)
		return 1
	}
	body, err := api.EncodePPM(rec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PPM 编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("chroma 4:2:0 recon=%s %dx%d -> %s (%d bytes)\n", *recon, img.W, img.H, *out, len(body))
	return 0
}
