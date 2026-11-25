package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	case "graymap":
		os.Exit(runGrayMap(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode|graymap --format pgm|bmp|rle --in F --out F [--gain N]")
	fmt.Fprintln(os.Stderr, "       gtk graymap --outdir DIR   # 生成 8/10/12 位深 LUT 与倒挂坐标 CSV")
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

// runGrayMap 生成三档位深的灰度映射数据：每档一份逐点 LUT CSV、一份倒挂坐标 CSV。
func runGrayMap(args []string) int {
	fs := flag.NewFlagSet("graymap", flag.ContinueOnError)
	outdir := fs.String("outdir", "artifacts", "输出目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := os.MkdirAll(*outdir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "建目录失败:", err)
		return 1
	}
	for _, bits := range []int{8, 10, 12} {
		lut, rep, err := api.BuildGrayMap(bits)
		if err != nil {
			fmt.Fprintln(os.Stderr, "构建失败:", err)
			return 1
		}
		lutPath := filepath.Join(*outdir, fmt.Sprintf("lut_%dbit.csv", bits))
		if err := writeLUTCSV(lutPath, lut); err != nil {
			fmt.Fprintln(os.Stderr, "写 LUT 失败:", err)
			return 1
		}
		invPath := filepath.Join(*outdir, fmt.Sprintf("inversions_%dbit.csv", bits))
		if err := writeInversionCSV(invPath, rep); err != nil {
			fmt.Fprintln(os.Stderr, "写倒挂坐标失败:", err)
			return 1
		}
		fmt.Printf("bits=%d n=%d inversions=%d rawTies=%d residualTieRuns=%d "+
			"maxDevRaw=%d maxDevMono=%d maxDevIdeal=%.3f endpoints raw=(%d,%d) fixed=(%d,%d)\n",
			bits, len(lut.Raw), rep.InversionCount, rep.RawTieCount, rep.ResidualTieRuns,
			rep.MaxDeviationRaw, rep.MaxDeviationMono, rep.MaxDeviationIdeal,
			rep.RawEnd0, rep.RawEndN, rep.FixedEnd0, rep.FixedEndN)
		fmt.Printf("  -> %s\n  -> %s\n", lutPath, invPath)
	}
	return 0
}

func writeLUTCSV(path string, lut *api.GrayLUT) error {
	var b strings.Builder
	b.WriteString("index,raw,mono,fixed\n")
	for i := range lut.Raw {
		fmt.Fprintf(&b, "%d,%d,%d,%d\n", i, lut.Raw[i], lut.Mono[i], lut.Fixed[i])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeInversionCSV(path string, rep *api.GrayReport) error {
	var b strings.Builder
	b.WriteString("i0,i1,y0,y1\n")
	for _, iv := range rep.Inversions {
		fmt.Fprintf(&b, "%d,%d,%d,%d\n", iv.I0, iv.I1, iv.Y0, iv.Y1)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
