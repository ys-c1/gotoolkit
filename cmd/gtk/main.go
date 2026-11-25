package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
	case "analyze":
		os.Exit(runAnalyze(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle|hybrid --in F --out F [--gain N]")
	fmt.Fprintln(os.Stderr, "       gtk analyze --in F --mask F [--tsv F] [--blocks F] [--json F]")
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式（pgm|bmp|rle|hybrid）")
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
	var body []byte
	var used string
	if *format == "hybrid" {
		// 混合模式带体积约束：GTH1 不占优时回退为最小的单一格式。
		body, used, err = api.EncodeHybrid(img, api.Options{Format: "hybrid", Gain: *gain})
	} else {
		body, err = api.Encode(img, api.Options{Format: *format, Gain: *gain})
		used = *format
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("encoded %dx%d -> %s (%s, %d bytes)\n", img.W, img.H, *format, used, len(body))
	return 0
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输入格式（pgm|bmp|rle|hybrid）")
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

// runAnalyze 对输入 PGM 做混合分区分析：打印分区、两类失真与体积，并写出掩码 PGM。
// 掩码图：0 表示平坦块、255 表示纹理块；用 P5 写出，便于与 PGM 工具链一致处理。
func runAnalyze(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM")
	maskOut := fs.String("mask", "", "输出分区掩码 PGM（0 平坦 / 255 纹理）")
	tsvOut := fs.String("tsv", "", "可选：输出阈值扫描 TSV")
	blocksOut := fs.String("blocks", "", "可选：输出逐块 σ TSV（绘图用）")
	jsonOut := fs.String("json", "", "可选：输出汇总 JSON")
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
	a, err := api.AnalyzeHybrid(img)
	if err != nil {
		fmt.Fprintln(os.Stderr, "分析失败:", err)
		return 1
	}
	report := analyzeReport(filepath.Base(*in), img, a)
	fmt.Print(report.text)
	if *maskOut != "" {
		body, err := api.Encode(&api.Image{W: img.W, H: img.H, Pix: a.Mask}, api.Options{Format: "pgm"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "掩码写出失败:", err)
			return 1
		}
		if err := os.WriteFile(*maskOut, body, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "掩码写出失败:", err)
			return 1
		}
		fmt.Printf("mask -> %s (%d bytes)\n", *maskOut, len(body))
	}
	if *tsvOut != "" {
		if err := os.WriteFile(*tsvOut, []byte(report.tsv), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "TSV 写出失败:", err)
			return 1
		}
		fmt.Printf("sweep -> %s\n", *tsvOut)
	}
	if *blocksOut != "" {
		if err := os.WriteFile(*blocksOut, []byte(report.blocksTSV), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "逐块 TSV 写出失败:", err)
			return 1
		}
		fmt.Printf("blocks -> %s\n", *blocksOut)
	}
	if *jsonOut != "" {
		if err := os.WriteFile(*jsonOut, []byte(report.json), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "JSON 写出失败:", err)
			return 1
		}
		fmt.Printf("summary -> %s\n", *jsonOut)
	}
	return 0
}
