// ditherreport 打印误差扩散抖动的可复现数据：
// 权重表、三格式体积、相邻色阶间距方差与降幅。
//
// 用法示例：
//
//	go run ./cmd/ditherreport -out artifacts/dither
//
// 样本由本程序生成（横向灰度渐变，远端再叠一点曲率），不依赖外部素材；
// 两次运行的输出逐字节一致。
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

const (
	sampleW = 256
	sampleH = 128
)

// sampleGradient 生成横向灰度渐变：左端 0，右端 255，按 t^2.2 弯曲，
// 使色阶带宽在低端更宽、高端更窄，基线间距方差更大，便于观察抖动效果。
func sampleGradient(w, h int) *api.Image {
	img := &api.Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := float64(x) / float64(w-1)
			v := 255 * math.Pow(t, 2.2)
			img.Pix[y*w+x] = uint8(v + 0.5)
		}
	}
	return img
}

func main() {
	out := flag.String("out", "", "产物目录（为空则只打印表格）")
	flag.Parse()

	fmt.Println("== 扩散核权重表（原始与归一后）==")
	for _, k := range api.BuiltinDitherKernels() {
		fmt.Print(k.WeightTable())
		fmt.Println()
	}

	src := sampleGradient(sampleW, sampleH)
	if *out != "" {
		if err := os.MkdirAll(*out, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		raw, err := api.Encode(src, api.Options{Format: "pgm"})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(*out, "gradient.pgm"), raw, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	fmt.Printf("== 样本 %dx%d，色阶数 %d（%g 位等效）==\n", sampleW, sampleH, api.DitherLevelsDefault, math.Log2(float64(api.DitherLevelsDefault)))
	fmt.Println("间距口径：逐行切同值游程，去掉首尾被画幅截断的两条，其余游程长度即")
	fmt.Println("相邻两次色阶变化的间距；方差为该行集合的总体方差。降幅以同色阶数、")
	fmt.Println("只做逐像素量化（不扩散）的基线为分母。")
	fmt.Printf("%-12s %-6s %-8s %-12s %-12s %-10s %-8s %-8s %-8s\n",
		"强度", "格式", "字节数", "色阶间距均值", "间距方差", "降幅", "最短", "最长", "样本数")

	// 基线：同色阶数下只做逐像素量化、不扩散。间距方差的降幅以它为分母。
	baseImg, err := api.QuantizeOnly(src, api.DitherLevelsDefault)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	baseStats, err := api.MeasureLevelSpacing(baseImg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	kernel := api.FloydSteinbergKernel()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := api.Encode(baseImg, api.Options{Format: f})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%-12s %-6s %-8d %-12.4f %-12.4f %-10s %-8d %-8d %-8d\n",
			"0(仅量化)", f, len(body), baseStats.Mean, baseStats.Variance, "-",
			baseStats.Min, baseStats.Max, baseStats.Samples)
		writeArtifact(*out, "base."+f, body)
	}
	for _, strength := range []float64{0.5, 1.0} {
		for _, f := range []string{"pgm", "bmp", "rle"} {
			body, err := api.Encode(src, api.Options{
				Format:         f,
				DitherStrength: strength,
				DitherKernel:   kernel,
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			img, err := api.Decode(body, api.Options{Format: f})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			st, err := api.MeasureLevelSpacing(img)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			drop := "-"
			if d, ok := api.SpacingVarianceDrop(baseStats, st); ok {
				drop = fmt.Sprintf("%.2f%%", 100*d)
			}
			fmt.Printf("%-12g %-6s %-8d %-12.4f %-12.4f %-10s %-8d %-8d %-8d\n",
				strength, f, len(body), st.Mean, st.Variance, drop, st.Min, st.Max, st.Samples)
			writeArtifact(*out, fmt.Sprintf("s%g.%s", strength, f), body)
		}
	}

	fmt.Println("\n== 未归一核（Atkinson，6 拍各 1/8，和为 3/4）对照，强度 1 ==")
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := api.Encode(src, api.Options{
			Format:         f,
			DitherStrength: 1,
			DitherKernel:   api.AtkinsonKernel(),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		img, err := api.Decode(body, api.Options{Format: f})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		st, err := api.MeasureLevelSpacing(img)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		drop := "-"
		if d, ok := api.SpacingVarianceDrop(baseStats, st); ok {
			drop = fmt.Sprintf("%.2f%%", 100*d)
		}
		fmt.Printf("%-12s %-6s %-8d %-12.4f %-12.4f %-10s %-8d %-8d %-8d\n",
			"atkinson", f, len(body), st.Mean, st.Variance, drop, st.Min, st.Max, st.Samples)
		writeArtifact(*out, "atkinson."+f, body)
	}

	fmt.Println("\n== 蛇形扫描的方向与切换点（以 W=5、H=4 为例，可逐行复算）==")
	fmt.Printf("%-4s %-6s %-8s %-8s %-30s\n", "行y", "方向", "起始列", "结束列", "访问顺序")
	for y := 0; y < 4; y++ {
		order := ""
		d := api.DitherDirection(y)
		for x := api.DitherRowStart(y, 5); ; x += d {
			order += fmt.Sprintf("%d ", x)
			if x == api.DitherRowEnd(y, 5) {
				break
			}
		}
		fmt.Printf("%-4d %-6d %-8d %-8d %-30s\n", y, d,
			api.DitherRowStart(y, 5), api.DitherRowEnd(y, 5), order)
	}
	fmt.Println("切换规则：行索引奇偶决定方向（偶 +1、奇 -1）；切换点在每行行末，")
	fmt.Println("下一行起始列恒等于上一行结束列，故切换点可按 y 复算：")
	fmt.Println("  偶行结束于 x=W-1（右端），奇行结束于 x=0（左端）。")

	fmt.Println("\n== 关闭抖动（强度 0）时三格式与基准逐字节比对 ==")
	for _, f := range []string{"pgm", "bmp", "rle"} {
		got, err := api.Encode(goldenSample(), api.Options{Format: f, DitherStrength: 0})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		want, err := os.ReadFile(filepath.Join("testdata", "golden", f+".bin"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		same := len(got) == len(want) && string(got) == string(want)
		fmt.Printf("%-6s golden=%d bytes identical=%v\n", f, len(want), same)
	}
}

// writeArtifact 在给了 -out 时把码流落到产物目录。
func writeArtifact(dir, name string, body []byte) {
	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// goldenSample 复刻 ref/main.go 的 4×3 样本，仅用于基准比对。
func goldenSample() *api.Image {
	img := &api.Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	return img
}
