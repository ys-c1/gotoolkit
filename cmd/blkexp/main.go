// blkexp 是块压缩块长实验：在一张确定性合成图上跑 4/8/16 三档块长，
// 计算块内样本方差、重建误差（表达式预测 vs 实测）与块边界差均值，
// 把结果写成 Markdown 表并把原图/重建图导出为 P5 PGM 便于目视。
//
// 用法：go run ./cmd/blkexp -out artifacts/blkexp
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
	imgW = 256
	imgH = 256
)

var blockLens = []int{4, 8, 16}

// lcg 是确定性线性同余发生器（Numerical Recipes 常数），
// 保证实验图与随机噪声在任何机器上逐位可复现。
type lcg struct{ s uint64 }

func newLCG(seed uint64) *lcg { return &lcg{s: seed} }

// float64 返回 (0,1) 上的均匀数。
func (g *lcg) float64() float64 {
	g.s = g.s*6364136223846793005 + 1442695040888963407
	return float64(g.s>>11) / float64(uint64(1)<<53)
}

// gauss 用 Box-Muller 生成标准正态数。
func (g *lcg) gauss() float64 {
	u1, u2 := g.float64(), g.float64()
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// makeImage 生成 256×256 灰度图：缓变梯度 + 周期纹理 + 高斯噪声，
// 既含平滑区也含高纹理区，块间统计量有代表性。
func makeImage() []uint8 {
	rng := newLCG(42)
	pix := make([]uint8, imgW*imgH)
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			grad := 40.0 + 120.0*float64(x+y)/(imgW+imgH)
			tex := 24.0 * math.Sin(2*math.Pi*float64(x)/17.0) * math.Cos(2*math.Pi*float64(y)/23.0)
			noise := 10.0 * rng.gauss()
			v := grad + tex + noise
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			pix[y*imgW+x] = uint8(math.Round(v))
		}
	}
	return pix
}

// stats 汇总某一档块长的全部测量量。
type stats struct {
	L             int
	Blocks        int
	MeanVar       float64 // 块内无偏样本方差均值 s̄²(L)
	WithinMSE     float64 // 块内散布项实测：Σ(x-x̄)²/N
	RoundMSE      float64 // 均值取整项实测：(x̄-round(x̄))² 的像素均值
	MeasuredMSE   float64 // 实测重建 MSE
	PredMSE1_12   float64 // 表达式预测 MSE（取整项按 1/12）
	PredExact     float64 // 精确分解预测（取整项用实测 q，应与实测恒等）
	PSNR          float64
	BoundaryH     float64 // 竖向块边界（左右相邻块）差均值
	BoundaryV     float64 // 横向块边界（上下相邻块）差均值
	BoundaryAll   float64 // 全部边界差均值
	StreamBytes   int
	CompressRatio float64
}

func compute(pix []uint8, L int) stats {
	nx, ny := imgW/L, imgH/L
	n := imgW * imgH
	// 用真实编解码路径产出重建图，保证测的就是产物本身。
	img := &api.Image{W: imgW, H: imgH, Pix: pix}
	body, err := api.Encode(img, api.Options{Format: "blk", BlockLen: L})
	if err != nil {
		panic(err)
	}
	rec, err := api.Decode(body, api.Options{Format: "blk"})
	if err != nil {
		panic(err)
	}

	var sumVar, sumWithin, sumRound float64
	var sse float64
	for by := 0; by < ny; by++ {
		for bx := 0; bx < nx; bx++ {
			var sum int
			m := L * L
			for y := by * L; y < (by+1)*L; y++ {
				for x := bx * L; x < (bx+1)*L; x++ {
					sum += int(pix[y*imgW+x])
				}
			}
			mean := float64(sum) / float64(m)
			// 无偏样本方差 s_b² = Σ(x-x̄)²/(m-1)
			var ss float64
			for y := by * L; y < (by+1)*L; y++ {
				for x := bx * L; x < (bx+1)*L; x++ {
					d := float64(pix[y*imgW+x]) - mean
					ss += d * d
				}
			}
			sumVar += ss / float64(m-1)
			sumWithin += ss
			// 取整误差：块均值与四舍五入后存储值的差（块内每像素相同）。
			rd := mean - math.Round(mean)
			sumRound += rd * rd * float64(m)
		}
	}
	for i := range pix {
		d := float64(pix[i]) - float64(rec.Pix[i])
		sse += d * d
	}

	st := stats{
		L:             L,
		Blocks:        nx * ny,
		MeanVar:       sumVar / float64(nx*ny),
		WithinMSE:     sumWithin / float64(n),
		RoundMSE:      sumRound / float64(n),
		MeasuredMSE:   sse / float64(n),
		StreamBytes:   len(body),
		CompressRatio: float64(n) / float64(len(body)),
	}
	st.PredMSE1_12 = (1-1.0/float64(L*L))*st.MeanVar + 1.0/12.0
	st.PredExact = st.WithinMSE + st.RoundMSE
	st.PSNR = 10 * math.Log10(255.0*255.0/st.MeasuredMSE)

	// 边界差均值：只统计块与块之间的边界，不含画幅外边缘。
	var bhSum, bvSum float64
	bhCnt, bvCnt := 0, 0
	for k := 1; k < nx; k++ {
		x := k*L - 1
		for y := 0; y < imgH; y++ {
			bhSum += math.Abs(float64(rec.Pix[y*imgW+x+1]) - float64(rec.Pix[y*imgW+x]))
			bhCnt++
		}
	}
	for k := 1; k < ny; k++ {
		y := k*L - 1
		for x := 0; x < imgW; x++ {
			bvSum += math.Abs(float64(rec.Pix[(y+1)*imgW+x]) - float64(rec.Pix[y*imgW+x]))
			bvCnt++
		}
	}
	st.BoundaryH = bhSum / float64(bhCnt)
	st.BoundaryV = bvSum / float64(bvCnt)
	st.BoundaryAll = (bhSum + bvSum) / float64(bhCnt+bvCnt)
	return st
}

func writePGM(path string, pix []uint8) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "P5\n%d %d\n255\n", imgW, imgH); err != nil {
		return err
	}
	_, err = f.Write(pix)
	return err
}

func main() {
	out := flag.String("out", "artifacts/blkexp", "产物目录")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pix := makeImage()
	if err := writePGM(filepath.Join(*out, "original.pgm"), pix); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	all := make([]stats, 0, len(blockLens))
	for _, L := range blockLens {
		st := compute(pix, L)
		all = append(all, st)
		body, err := api.Encode(&api.Image{W: imgW, H: imgH, Pix: pix}, api.Options{Format: "blk", BlockLen: L})
		if err != nil {
			panic(err)
		}
		rec, err := api.Decode(body, api.Options{Format: "blk"})
		if err != nil {
			panic(err)
		}
		if err := writePGM(filepath.Join(*out, fmt.Sprintf("recon_L%d.pgm", L)), rec.Pix); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	var b []byte
	add := func(format string, args ...any) { b = append(b, fmt.Sprintf(format, args...)...) }
	add("# 块压缩块长实验结果\n\n")
	add("- 测试图：256×256 确定性合成图（梯度 + 周期纹理 + σ=10 高斯噪声，LCG seed=42），见 `original.pgm`\n")
	add("- 编解码：`api.Encode/Decode`（Format=blk），块均值四舍五入存为 uint8，整块回填\n")
	add("- 三档块长 L=4/8/16 均同时整除 256（256=2⁸）\n\n")

	add("## 1. 块内样本方差（无偏估计，公式见 docs/blk-blocklen.md）\n\n")
	add("| 块长 L | 块数 | 块大小 m=L² | s̄²(L) 块内样本方差均值 |\n")
	add("|---|---:|---:|---:|\n")
	for _, st := range all {
		add("| %d | %d | %d | %.4f |\n", st.L, st.Blocks, st.L*st.L, st.MeanVar)
	}

	add("\n## 2. 重建误差：表达式预测 vs 实测（像素均值 MSE）\n\n")
	add("| 块长 L | (1−1/m)·s̄²(L) 块内项 | 取整项 1/12 | 预测 MSE | 实测 MSE | 预测−实测 差 | 相对偏差 | 实测 PSNR(dB) |\n")
	add("|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, st := range all {
		within := (1 - 1.0/float64(st.L*st.L)) * st.MeanVar
		diff := st.PredMSE1_12 - st.MeasuredMSE
		rel := diff / st.MeasuredMSE * 100
		add("| %d | %.4f | %.4f | %.4f | %.4f | %+.4f | %+.2f%% | %.2f |\n",
			st.L, within, 1.0/12.0, st.PredMSE1_12, st.MeasuredMSE, diff, rel, st.PSNR)
	}

	add("\n### 精确分解（取整项改用实测 q̄，等式应恒成立）\n\n")
	add("| 块长 L | 实测块内项 (1−1/m)·s̄² | 实测取整项 q̄ | 精确预测=两项之和 | 实测 MSE | 差 |\n")
	add("|---|---:|---:|---:|---:|---:|\n")
	for _, st := range all {
		diff := st.PredExact - st.MeasuredMSE
		add("| %d | %.6f | %.6f | %.6f | %.6f | %+.2e |\n",
			st.L, st.WithinMSE, st.RoundMSE, st.PredExact, st.MeasuredMSE, diff)
	}

	add("\n## 3. 方格可见度：相邻块边界差均值（重建图，|Δ| 均值）\n\n")
	add("| 块长 L | 竖向边界差均值（左右块） | 横向边界差均值（上下块） | 全部边界差均值 | 边界数 |\n")
	add("|---|---:|---:|---:|---:|\n")
	for _, st := range all {
		nx, ny := imgW/st.L, imgH/st.L
		add("| %d | %.4f | %.4f | %.4f | %d |\n",
			st.L, st.BoundaryH, st.BoundaryV, st.BoundaryAll, (nx-1)*imgH+(ny-1)*imgW)
	}

	add("\n## 4. 块长与画幅整除、压缩率\n\n")
	add("| 块长 L | 整除关系 | 块数 | 流字节数 | 压缩率（原 %d 字节） |\n", imgW*imgH)
	add("|---|---|---:|---:|---:|\n")
	for _, st := range all {
		add("| %d | 256/%d=%d 余 0（宽高均可整除） | %d | %d | %.1f× |\n",
			st.L, st.L, 256/st.L, st.Blocks, st.StreamBytes, st.CompressRatio)
	}

	path := filepath.Join(*out, "results.md")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(string(b))
	fmt.Printf("\n产物已写入 %s/\n", *out)
}
