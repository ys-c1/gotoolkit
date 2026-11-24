package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

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
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	w := os.Stderr
	fmt.Fprintln(w, "usage: gtk encode|decode --format pgm|bmp|rle|idx --in F --out F [选项]")
	fmt.Fprintln(w, "选项:")
	fmt.Fprintln(w, "  --gain N        亮度增益百分比 [-100,100]，默认 0")
	fmt.Fprintln(w, "缩略图链路（默认全关，全关时产物与旧版逐字节一致）:")
	fmt.Fprintln(w, "  --palette       打开调色板索引（等间距灰度级，索引不去重）")
	fmt.Fprintln(w, "  --dither        索引时做 Floyd-Steinberg 抖动（需配合 --palette）")
	fmt.Fprintln(w, "  --buckets N     调色板桶数，palette 打开时必须 >0 且 <=255，默认 16")
	fmt.Fprintln(w, "  --thumb N       面积平均缩略除数，按 N×N 块平均；0=不缩略，负数报错")
	fmt.Fprintln(w, "链路顺序: 先面积平均缩略，后调色板索引；缩略比例与抖动强度关系: 强度 = 1/thumb（未缩略时为 1）")
}

// bindPipelineFlags 注册缩略图链路的四个开关，返回读取结果的闭包。
func bindPipelineFlags(fs *flag.FlagSet) func() api.Options {
	palette := fs.Bool("palette", false, "打开调色板索引（索引不去重）")
	dither := fs.Bool("dither", false, "索引时做 Floyd-Steinberg 抖动")
	buckets := fs.Int("buckets", 16, "调色板桶数（1..255），palette 打开时为零报错")
	thumb := fs.Int("thumb", 0, "面积平均缩略除数；0=不缩略，负数报错")
	return func() api.Options {
		return api.Options{
			Palette:  *palette,
			Dither:   *dither,
			Buckets:  *buckets,
			ThumbDiv: *thumb,
		}
	}
}

func printReport(r *api.PipelineReport) {
	fmt.Printf("链路: 缩略=%v 调色板=%v 抖动=%v\n", r.ThumbApplied, r.PaletteApplied, r.DitherApplied)
	if r.ThumbApplied {
		fmt.Printf("缩略: %dx%d -> %dx%d (除数 %d, %dx%d 面积平均)\n",
			r.OrigW, r.OrigH, r.ThumbW, r.ThumbH, r.ThumbDiv, r.ThumbDiv, r.ThumbDiv)
	}
	if r.DitherApplied {
		fmt.Printf("抖动强度: %d/%d (= 1/缩略除数)\n", r.DitherStrengthNum, r.DitherStrengthDen)
	}
	if r.PaletteApplied {
		fmt.Printf("调色板: %d 桶, 索引不去重\n", r.Buckets)
	}
	// 索引数量变化按缩略前后两列对照，差值列可由两列直接复算。
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "缩略前索引数\t缩略后索引数\t差值(后-前)")
	fmt.Fprintf(tw, "%d\t%d\t%d\n", r.IndexCountBefore, r.IndexCountAfter, r.Delta)
	tw.Flush()
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	pick := bindPipelineFlags(fs)
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
	opt := pick()
	opt.Format = *format
	opt.Gain = *gain
	body, report, err := api.EncodeWithReport(img, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("encoded %dx%d -> %s (%d bytes)\n", img.W, img.H, *format, len(body))
	printReport(report)
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
