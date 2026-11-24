// Command monoreport 在黑到白全渐变上逐点实测量化重建的单调性，
// 打印“修前(buggy 位反转电平表)/修后(单调表)”两张三档位深对照表，
// 并可用 -write-samples 把非单调样例 PGM 留档到 testdata/monotonic。
//
// 复现：
//
//	go run ./cmd/monoreport
//	go run ./cmd/monoreport -write-samples
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/api"
	"gotoolkit/internal/quant"
)

var tiers = []int{2, 3, 4}

func main() {
	write := flag.Bool("write-samples", false, "把渐变与修前/修后 PGM 样例写入 -outdir")
	outDir := flag.String("outdir", filepath.Join("testdata", "monotonic"), "样例输出目录")
	flag.Parse()

	fmt.Println("# 量化重建单调性实测（黑→白渐变 x=0..255，全 256 点 / 255 对相邻点，逐点非抽样）")
	fmt.Println()
	fmt.Println("定义：Δ_i = y[i+1] - y[i]（向右变亮为正，向下反转、右侧更暗为负）；")
	fmt.Println("破坏点 = Δ_i < 0 的相邻对；最大反转量 = min_i Δ_i（最负的相邻差）。")
	fmt.Println()

	fmt.Println("## 修前：电平表被位反转重排（区间仍单调，重建电平不单调）")
	printTable(true)
	printWorstPairs(true)

	fmt.Println("## 修后：区间边界严格递增、重建电平单调不减")
	printTable(false)

	fmt.Println("## 附：修后全位深扫描 1..8 bit")
	printSweep()

	if *write {
		if err := writeSamples(*outDir); err != nil {
			fmt.Fprintln(os.Stderr, "写样例失败:", err)
			os.Exit(1)
		}
		fmt.Println()
		fmt.Println("样例已写入", *outDir)
	}
}

func printTable(buggy bool) {
	fmt.Println()
	fmt.Println("| 位深 | 破坏点数 | 最大反转量 min Δ_i（相邻差，向下为负） |")
	fmt.Println("|------|---------:|---------------------------------------:|")
	for _, bits := range tiers {
		tab := mustTable(bits, buggy)
		m := quant.MeasureRamp(tab)
		rev := fmt.Sprintf("%d", m.WorstDelta)
		fmt.Printf("| %d bit（%d 电平） | %d | %s |\n", bits, m.Levels, m.BreakCount, rev)
	}
	fmt.Println()
	if buggy {
		fmt.Println("（带符号的最大反转量即最负相邻差；三档对应的向下幅度绝对值为 64 / 160 / 208 级。）")
		fmt.Println()
	}
}

func printWorstPairs(buggy bool) {
	fmt.Println("最严重的反转对（修之前具体坏在哪）：")
	fmt.Println()
	for _, bits := range tiers {
		tab := mustTable(bits, buggy)
		y := quant.Reconstruct(tab)
		m := quant.MeasureRamp(tab)
		i := indexOfWorst(y)
		fmt.Printf("- %d bit：x=%d→%d 时重建 %d→%d，Δ=%d；全表共 %d 个破坏点。\n",
			bits, i, i+1, y[i], y[i+1], m.WorstDelta, m.BreakCount)
	}
	fmt.Println()
}

func printSweep() {
	fmt.Println()
	fmt.Println("| 位深 | 电平数 | 破坏点数 | 最大反转量 min Δ_i |")
	fmt.Println("|------|--------|---------:|-------------------:|")
	for bits := 1; bits <= 8; bits++ {
		tab, err := quant.New(bits)
		if err != nil {
			panic(err)
		}
		m := quant.MeasureRamp(tab)
		fmt.Printf("| %d bit | %d | %d | %d |\n", bits, m.Levels, m.BreakCount, m.WorstDelta)
	}
	fmt.Println()
}

func mustTable(bits int, buggy bool) *quant.Table {
	var (
		tab *quant.Table
		err error
	)
	if buggy {
		tab, err = quant.NewLegacyBitReversed(bits)
	} else {
		tab, err = quant.New(bits)
	}
	if err != nil {
		panic(err)
	}
	return tab
}

func indexOfWorst(y []uint8) int {
	wi := 0
	wd := 1 << 30
	for i := 0; i+1 < len(y); i++ {
		d := int(y[i+1]) - int(y[i])
		if d < wd {
			wd, wi = d, i
		}
	}
	return wi
}

// writeSamples 生成 256×H 的 PGM：原始渐变、修前 4bit 非单调样例、修后 4bit 对照。
// 走 api.Encode 公共入口，保证样例就是工具能产出/读回的真实文件。
func writeSamples(dir string) error {
	const h = 16
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src := gradientImage(h)

	files := []struct {
		name string
		img  *api.Image
	}{
		{"gradient-src.pgm", src},
		{"before-bitreverse-b4.pgm", applyTable(src, mustTable(4, true))},
		{"after-monotone-b4.pgm", applyTable(src, mustTable(4, false))},
	}
	for _, f := range files {
		body, err := api.Encode(f.img, api.DefaultOptions()) // Bits=0：像素已在外部量化
		if err != nil {
			return fmt.Errorf("%s 编码: %w", f.name, err)
		}
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d bytes)\n", p, len(body))
	}
	return nil
}

func gradientImage(h int) *api.Image {
	img := &api.Image{W: 256, H: h, Pix: make([]uint8, 256*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < 256; x++ {
			img.Pix[y*256+x] = uint8(x)
		}
	}
	return img
}

func applyTable(img *api.Image, tab *quant.Table) *api.Image {
	out := &api.Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		out.Pix[i] = tab.Quantize(v)
	}
	return out
}
