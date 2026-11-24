// Command quantstudy 完成“量化误差是否均匀 → 偏置项 → 散度最优重建偏移 →
// 逐档验证”的完整实验：
//
//	go run ./cmd/quantstudy \
//	  -imgdir testdata/study -out artifacts/quant_bias/results.json
//
// 它会确定性生成四类图的 PGM，经 api 的 PGM 编解码往返后做分析，
// 把全部数值与画图用直方图写为 results.json，并在 stdout 打印判定表。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

func main() {
	size := flag.Int("size", api.DefaultQuantSize, "合成图边长")
	seed := flag.Int64("seed", api.DefaultQuantSeed, "随机种子")
	imgDir := flag.String("imgdir", filepath.Join("testdata", "study"), "四类 PGM 输出目录")
	out := flag.String("out", filepath.Join("artifacts", "quant_bias", "results.json"), "结果 JSON 路径")
	flag.Parse()

	if err := run(*size, *seed, *imgDir, *out); err != nil {
		fmt.Fprintln(os.Stderr, "quantstudy 失败:", err)
		os.Exit(1)
	}
}

func run(size int, seed int64, imgDir, out string) error {
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		return err
	}
	images := api.SampleQuantImages(size, seed)

	// 写 PGM 并解码回来，确保研究对象确实经过本仓库的编解码链路。
	roundtripped := make(map[string]*api.Image, len(images))
	for _, class := range api.QuantClassNames() {
		img := images[class]
		body, err := api.Encode(img, api.Options{Format: "pgm"})
		if err != nil {
			return err
		}
		path := filepath.Join(imgDir, class+".pgm")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
		back, err := api.Decode(body, api.Options{Format: "pgm"})
		if err != nil {
			return err
		}
		roundtripped[class] = back
	}

	rep, err := api.BuildQuantBiasReport(roundtripped, api.DefaultQuantBits(), seed)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		return err
	}

	printTable(rep)
	fmt.Printf("\n四类 PGM  -> %s\n结果 JSON -> %s\n", imgDir, out)
	if !rep.AllPass {
		return fmt.Errorf("存在未通过 0.05 阈值或散度未下降的单元")
	}
	fmt.Println("总判定: PASS（四类图 × 三档位深，残余偏置均 ≤ 0.05，三种散度均下降）")
	return nil
}

func printTable(rep *api.QuantBiasReport) {
	fmt.Printf("阈值 τ=%.2f（归一化步长）  图幅 %dx%d  seed=%d\n\n",
		rep.Threshold, rep.Width, rep.Height, rep.Seed)
	fmt.Println("class     bits  Δ     β      δ*     δint  |res|   JS0 -> JS1      KL0 -> KL1     χ²0 -> χ²1    down")
	for _, c := range rep.Cells {
		fmt.Printf("%-9s %d  %5.1f %6.4f %6.2f %4d  %.4f  %.4f->%.5f  %.4f->%.5f  %7.3f->%6.3f  %v\n",
			c.Class, c.Bits, c.Step, c.PhaseMean, c.OffsetClosedForm, c.OffsetInt,
			abs(c.ResidualBiasInteger),
			c.JSOriginal, c.JSInteger, c.KLOriginal, c.KLInteger,
			c.Chi2Original, c.Chi2Integer, c.DivergenceDown)
	}
	fmt.Println("\n四类图汇总（每类跨三档取最差）:")
	for _, v := range rep.ClassVerdicts {
		fmt.Printf("  %-9s max|res|=%.4f @b=%d  bias_pass=%v  divergence_down_all=%v  PASS=%v\n",
			v.Class, v.MaxResidualBias, v.WorstDepthBits, v.BiasPass, v.AllDivergenceDown, v.Pass)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
