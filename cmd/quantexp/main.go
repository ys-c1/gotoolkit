// quantexp 是位深-量化误差熵的实验程序：
// 生成两张 256x256 的 8 位测试图，对三档位深 b=2,3,4 测量量化误差的
// 经验熵，与理论值 H*=b-1 对照，并验证“降一档熵增 1 比特/像素”。
//
// 两张测试图：
//   - uniform：全幅 0..255 周期铺满，误差在每档都严格均匀；
//   - uneven ：50% 全幅 + 50% 窄动态（0..3）区域，最粗一档误差只落在
//     0..3 四个桶，破坏均匀性，用于展示达不到理论熵的情形。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/quant"
)

const size = 256

// genUniform 生成全幅周期图，0..255 各出现 size 次。
func genUniform() []uint8 {
	pix := make([]uint8, size*size)
	for i := range pix {
		pix[i] = uint8(i % 256)
	}
	return pix
}

// genUneven 生成混合图：左 1/2 为全幅周期，右 1/2 为 0..3 窄动态。
// 窄区只覆盖 4 个残差：Δ=2、Δ=4 时误差仍均匀，Δ=8 时误差只落在
// 0..3 四个桶，4..7 桶为空，均匀性仅在最粗档被破坏。
func genUneven() []uint8 {
	pix := make([]uint8, size*size)
	split := size / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x < split {
				pix[y*size+x] = uint8((y*size + x) % 256)
			} else {
				pix[y*size+x] = uint8(x % 4)
			}
		}
	}
	return pix
}

type row struct {
	Depth     int
	Step      int
	Empirical float64
	Theory    float64
	Dev       float64
	Uniform   bool
	Symbols   int
}

func measure(name string, pix []uint8, depths []int) []row {
	rows := make([]row, 0, len(depths))
	for _, b := range depths {
		delta, _ := quant.Step(b)
		e, err := quant.Error(pix, b)
		if err != nil {
			panic(err)
		}
		emp := quant.Entropy(e)
		th, _ := quant.TheoreticalEntropy(b)
		rows = append(rows, row{
			Depth:     b,
			Step:      delta,
			Empirical: emp,
			Theory:    th,
			Dev:       emp - th,
			Uniform:   quant.IsUniform(e, 1e-9),
			Symbols:   len(quant.Histogram(e)),
		})
	}
	return rows
}

// slope 对 (b, H) 做最小二乘拟合，返回斜率与相邻档熵差。
func slope(rows []row) (float64, []float64) {
	n := float64(len(rows))
	var sx, sy, sxx, sxy float64
	diffs := make([]float64, 0, len(rows)-1)
	for i, r := range rows {
		x := float64(r.Depth)
		y := r.Empirical
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
		if i > 0 {
			diffs = append(diffs, y-rows[i-1].Empirical)
		}
	}
	m := (n*sxy - sx*sy) / (n*sxx - sx*sx)
	return m, diffs
}

func printTable(name string, rows []row) {
	fmt.Printf("== %s ==\n", name)
	fmt.Printf("%-6s %-8s %-14s %-12s %-12s %-8s %s\n",
		"档b", "步长Δ", "实测熵(bpp)", "理论(b-1)", "偏差", "均匀?", "符号数")
	for _, r := range rows {
		u := "是"
		if !r.Uniform {
			u = "否"
		}
		fmt.Printf("%-6d %-8d %-14.6f %-12.1f %-12.6f %-8s %d\n",
			r.Depth, r.Step, r.Empirical, r.Theory, r.Dev, u, r.Symbols)
	}
	m, diffs := slope(rows)
	fmt.Printf("相邻档熵增: ")
	for _, d := range diffs {
		fmt.Printf("%.6f  ", d)
	}
	fmt.Printf("\n三档最小二乘斜率: %.6f（理论斜率 +1，位深升一档/降档方向另见报告）\n\n", m)
}

func writePGM(dir, name string, pix []uint8) error {
	body, err := codec.EncodePGM(size, size, pix)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), body, 0o644)
}

func main() {
	out := flag.String("out", "artifacts", "测试图输出目录")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		panic(err)
	}
	depths := []int{2, 3, 4} // 对应 d=1,2,3，Δ=2,4,8，理论熵 1,2,3 bpp

	uni := genUniform()
	uneven := genUneven()
	if err := writePGM(*out, "uniform.pgm", uni); err != nil {
		panic(err)
	}
	if err := writePGM(*out, "uneven.pgm", uneven); err != nil {
		panic(err)
	}

	ru := measure("uniform", uni, depths)
	re := measure("uneven", uneven, depths)
	printTable("均匀输入 uniform.pgm", ru)
	printTable("含窄动态区域 uneven.pgm", re)

	// 不均匀档的误差分布，供报告引用
	fmt.Println("== uneven.pgm 最粗档 b=4 (Δ=8) 误差直方图 ==")
	e, _ := quant.Error(uneven, 4)
	h := quant.Histogram(e)
	n := float64(len(e))
	for k := 0; k < 8; k++ {
		c := h[k]
		fmt.Printf("e=%d  count=%6d  p=%.6f  理想p=%.6f\n",
			k, c, float64(c)/n, 1.0/8.0)
	}
	fmt.Printf("实测熵 = %.6f bpp，理论 = 3.000000 bpp，偏差 = %+.6f bpp\n",
		quant.Entropy(e), quant.Entropy(e)-3.0)
}
