package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N] [--predict-order N]")
	fmt.Fprintf(os.Stderr, "  --predict-order N  预测最大候选阶数 0..%d，0=关闭（默认）；仅 rle\n", api.MaxPredictOrder)
}

// cliFlags 是命令行可配项。解析后统一转成公共配置结构 api.Options。
// 不在这里引入任何只属于测试的开关。
type cliFlags struct {
	fs     *flag.FlagSet
	format *string
	in     *string
	out    *string
	gain   *int
	order  *int
}

func newFlags(name string) *cliFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return &cliFlags{
		fs:     fs,
		format: fs.String("format", "pgm", "输出/输入格式"),
		in:     fs.String("in", "", "输入文件"),
		out:    fs.String("out", "", "输出文件"),
		gain:   fs.Int("gain", 0, "亮度增益百分比"),
		order:  fs.Int("predict-order", 0, "预测最大候选阶数，0=关闭"),
	}
}

func (c *cliFlags) options() api.Options {
	return api.Options{Format: *c.format, Gain: *c.gain, PredictOrder: *c.order}
}

func runEncode(args []string) int {
	c := newFlags("encode")
	in, out := c.in, c.out
	if err := c.fs.Parse(args); err != nil {
		return 2
	}
	opt := c.options()
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
	body, rep, err := api.EncodeWithReport(img, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码失败:", err)
		return 1
	}
	if rep != nil {
		printReport(img, rep)
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("encoded %dx%d -> %s (%d bytes)\n", img.W, img.H, opt.Format, len(body))
	return 0
}

func runDecode(args []string) int {
	c := newFlags("decode")
	in, out := c.in, c.out
	if err := c.fs.Parse(args); err != nil {
		return 2
	}
	opt := c.options()
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	img, rep, err := api.DecodeWithReport(raw, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}
	if rep.Predict.KX != 0 || rep.Predict.KY != 0 {
		fmt.Printf("predicted stream: candidate=%s\n", rep.Predict.String())
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
	fmt.Printf("decoded %s -> %dx%d pgm (%d bytes)\n", opt.Format, img.W, img.H, len(body))
	return 0
}

func printReport(img *api.Image, rep *api.EncodeReport) {
	reduction := 100 * (1 - float64(rep.Bytes)/float64(rep.BaselineBytes))
	fmt.Printf("predict: image %dx%d samples=%d baseline(GTR1)=%d bytes predicted=%d bytes reduction=%.2f%%\n",
		img.W, img.H, rep.Samples, rep.BaselineBytes, rep.Bytes, reduction)
	var names []string
	for _, st := range rep.Stats {
		names = append(names, st.Candidate.String())
	}
	fmt.Printf("candidate set: %s\n", strings.Join(names, " "))
	fmt.Println("entropy: H = -sum_v p_v*log2(p_v) over all W*H residuals (row-major, boundary pixels included),")
	fmt.Println("          one pass per candidate; 0*log2(0) := 0")
	fmt.Printf("  %-8s %12s %10s\n", "candidate", "entropy", "zeros")
	for _, st := range rep.Stats {
		fmt.Printf("  %-8s %12.6f %10d\n", st.Candidate.String(), st.Entropy, st.Hist[0])
	}
	sel := rep.Selected
	fmt.Printf("selected by minimum residual entropy: %s", sel.String())
	if rep.FallbackToGTR {
		fmt.Printf(" -> predicted runs not smaller than baseline, emitted GTR1")
	} else {
		fmt.Printf(" -> emitted %s", rep.Emitted.String())
	}
	fmt.Println()
	var st *api.PredictStat
	for i := range rep.Stats {
		if rep.Stats[i].Candidate == sel {
			st = &rep.Stats[i]
			break
		}
	}
	if st == nil {
		return
	}
	printDistribution(st)
}

// printDistribution 打印选中候选的残差分布：先给 16 个粗桶（覆盖
// 0..255），再给计数最多的 16 个精确值，便于看清长尾。
func printDistribution(st *api.PredictStat) {
	fmt.Printf("residual distribution for selected candidate %s:\n", st.Candidate.String())
	fmt.Printf("  %-12s %10s %9s\n", "value range", "count", "percent")
	for b := 0; b < 16; b++ {
		c := 0
		for v := b * 16; v < b*16+16; v++ {
			c += st.Hist[v]
		}
		pct := 100 * float64(c) / float64(st.Samples)
		fmt.Printf("  %3d..%-3d %13d %8.3f%%%s\n", b*16, b*16+15, c, pct, bar(pct))
	}
	type bucket struct {
		v, c int
	}
	var top []bucket
	for v, c := range st.Hist {
		if c > 0 {
			top = append(top, bucket{v, c})
		}
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].c != top[j].c {
			return top[i].c > top[j].c
		}
		return top[i].v < top[j].v
	})
	const show = 16
	fmt.Printf("  top %d exact residual values:\n", show)
	for i, b := range top {
		if i == show {
			break
		}
		pct := 100 * float64(b.c) / float64(st.Samples)
		fmt.Printf("    value %3d %11d %8.3f%%%s\n", b.v, b.c, pct, bar(pct))
	}
	if len(top) > show {
		fmt.Printf("    ... %d more nonzero values\n", len(top)-show)
	}
}

func bar(pct float64) string {
	n := int(pct / 2)
	if n <= 0 {
		return ""
	}
	return " " + strings.Repeat("#", n)
}
