// Command palettecurve 生成中位切分桶数—误差曲线的可复算实验产物。
//
// 它构造一条 256 级横向灰度渐变，扫描三档桶数 4/16/64，写出 PGM 与
// artifacts/palette-curve/report.md、curve.csv。只使用标准库和本仓库的
// api/internal/palette，不访问网络。
package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"gotoolkit/api"
	"gotoolkit/internal/palette"
)

const (
	outDir = "artifacts/palette-curve"
	width  = 256
	height = 16
)

type row struct {
	k         int
	predMSE   float64
	gotMSE    float64
	gotMAE    float64
	gotRMSE   float64
	maxAbs    int
	leftMSE   float64
	wrongAxis float64
}

func main() {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}
	pix := rampPixels()
	img := &api.Image{W: width, H: height, Pix: pix}

	if err := writePGM("ramp.pgm", img, false, 0); err != nil {
		fatal(err)
	}

	ks := []int{4, 16, 64}
	rows := make([]row, 0, len(ks))
	for _, k := range ks {
		res, err := palette.Quantize(pix, k)
		if err != nil {
			fatal(err)
		}
		left, err := palette.QuantizeOrdered(pix, palette.Options{
			Buckets: k,
			Order:   palette.OrderLeftmost,
		})
		if err != nil {
			fatal(err)
		}
		if err := writePGM(fmt.Sprintf("ramp-k%d.pgm", k), img, true, k); err != nil {
			fatal(err)
		}
		rows = append(rows, row{
			k:         k,
			predMSE:   predictMSE(k),
			gotMSE:    res.MSE,
			gotMAE:    res.MAE,
			gotRMSE:   res.RMSE,
			maxAbs:    res.MaxAbs,
			leftMSE:   left.MSE,
			wrongAxis: blockMSE(shuffledRamp(), k),
		})
	}

	targetK, err := palette.UniformRampBucketsForMSE(2.0)
	if err != nil {
		fatal(err)
	}
	targetCheck, err := palette.Quantize(pix, targetK)
	if err != nil {
		fatal(err)
	}
	if targetK != 60 {
		fatal(fmt.Errorf("target MSE=2 prediction changed: K=%d", targetK))
	}

	if err := writeCurveCSV(pix); err != nil {
		fatal(err)
	}
	if err := writeReport(rows, targetK, targetCheck.MSE); err != nil {
		fatal(err)
	}
	fmt.Println("palette curve artifacts written to", outDir)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "palettecurve:", err)
	os.Exit(1)
}

func rampPixels() []uint8 {
	pix := make([]uint8, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pix[y*width+x] = uint8(x)
		}
	}
	return pix
}

func writePGM(name string, img *api.Image, paletteOn bool, k int) error {
	opt := api.Options{Format: "pgm"}
	if paletteOn {
		opt.Palette = true
		opt.Buckets = k
	}
	body, err := api.Encode(img, opt)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, name), body, 0o644)
}

func predictMSE(k int) float64 {
	v, err := palette.UniformRampMSE(k)
	if err != nil {
		panic(err)
	}
	return v
}

func writeCurveCSV(pix []uint8) error {
	f, err := os.Create(filepath.Join(outDir, "curve.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"k", "predicted_mse", "median_cut_mse", "mae", "rmse", "leftmost_order_mse"}); err != nil {
		return err
	}
	for k := 1; k <= 64; k++ {
		res, err := palette.Quantize(pix, k)
		if err != nil {
			return err
		}
		left, err := palette.QuantizeOrdered(pix, palette.Options{Buckets: k, Order: palette.OrderLeftmost})
		if err != nil {
			return err
		}
		rec := []string{
			fmt.Sprintf("%d", k),
			fmt.Sprintf("%.12f", predictMSE(k)),
			fmt.Sprintf("%.12f", res.MSE),
			fmt.Sprintf("%.12f", res.MAE),
			fmt.Sprintf("%.12f", res.RMSE),
			fmt.Sprintf("%.12f", left.MSE),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeReport(rows []row, targetK int, targetMSE float64) error {
	path := filepath.Join(outDir, "report.md")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintln(f, "# 中位切分桶数—误差曲线")
	fmt.Fprintln(f)
	fmt.Fprintf(f, "- 样本：确定性 256 级横向渐变，%d×%d，N=%d；每一级灰度在不同行重复 %d 次。\n", width, height, width*height, height)
	fmt.Fprintln(f, "- 误差：主指标为均方误差 MSE（灰度阶²），同时列出 MAE、RMSE。")
	fmt.Fprintln(f, "- 三档桶数：K=4 / 16 / 64；数值由 `go run ./cmd/palettecurve` 重新生成。")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "## 1. 误差曲线表达式")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "完整中位深度后 K0=2^s 个桶等宽 W=256/K0。离散均匀桶关于四舍五入均值的 MSE 为")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "```text")
	fmt.Fprintln(f, "E_W = (W²−1)/12 + 1/4 = W²/12 + 1/6。")
	fmt.Fprintln(f, "```")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "若 K=K0+r（0≤r<K0），方差优先顺序会先把 r 个宽 W 桶分裂成宽 W/2 的桶对，故按桶像素数加权：")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "```text")
	fmt.Fprintln(f, "Ê(K) = E_W − (r/K0)(E_W−E_{W/2}),  W=256/K0。")
	fmt.Fprintln(f, "```")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "在 K 为 2 的幂的锚点上 r=0，它退化为平滑包络")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "`Ê_env(K)=65536/(12K²)+1/6=16384/(3K²)+1/6`。三档代入：")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "- K=4：16384/(3·4²)+1/6 = 341.333…+0.166… = **341.500**；")
	fmt.Fprintln(f, "- K=16：16384/(3·16²)+1/6 = 21.333…+0.166… = **21.500**；")
	fmt.Fprintln(f, "- K=64：16384/(3·64²)+1/6 = 1.333…+0.166… = **1.500**。")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "曲线整体呈随 K 增大而快速下降的凸衰减，双对数锚点近似斜率 −2；非 2 的幂处按分裂顺序呈分段线性折线，早期加桶收益最大，K≥64 后明显趋缓。")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "给定目标 MSE*=2：在 32≤K≤64 段 K0=32、W=8，所以 `Ê(K)=5.5−(K−32)/8=9.5−K/8`，令其 ≤2 得 K≥60，预测桶数 K*=60。只套平滑幂律会算成约 54.58 并取 55，但 K=55 尚未完成这一轮等宽分裂，实测 MSE=2.625；分裂顺序修正式给出的 60 才达到 MSE=2.000。")
	fmt.Fprintln(f)

	fmt.Fprintln(f, "## 2. 三档桶数实测与预测")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "| K | 预测 MSE | 实测 MSE | 相对偏差 | 实测 MAE | 预测 MAE | RMSE | 最大绝对误差 | 错误下标维对照 MSE |")
	fmt.Fprintln(f, "|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, r := range rows {
		rel := (r.gotMSE - r.predMSE) / r.predMSE * 100
		predMAE := 64.0 / float64(r.k) // 三档均为 2 的幂锚点：W/4=(256/K)/4
		fmt.Fprintf(f, "| %d | %.3f | %.3f | %+.3f%% | %.3f | %.3f | %.3f | %d | %.3f |\n",
			r.k, r.predMSE, r.gotMSE, rel, r.gotMAE, predMAE, r.gotRMSE, r.maxAbs, r.wrongAxis)
	}
	fmt.Fprintln(f)
	for _, r := range rows {
		rel := (r.gotMSE - r.predMSE) / r.predMSE * 100
		fmt.Fprintf(f, "- K=%d 桶：实测 MSE=%.3f，预测 MSE=%.3f，相对偏差=%+.3f%%。\n", r.k, r.gotMSE, r.predMSE, rel)
	}
	fmt.Fprintln(f)
	fmt.Fprintf(f, "目标误差 MSE*=2 的桶数预测为 K*=%d；K=%d 实测 MSE=%.3f，达标。\n", targetK, targetK, targetMSE)
	fmt.Fprintln(f)

	fmt.Fprintln(f, "## 3. 分裂维与分裂顺序")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "分裂维选取依据：每轮计算各候选桶在灰度值维上的总体方差，优先切方差最大的桶并沿该维中位数分割，因为该方向离散度最大、一次分裂能消除的平方误差最多；灰度图只有一个颜色维，所以轴号恒为 0。")
	fmt.Fprintln(f)
	r4 := rows[0]
	fmt.Fprintf(f, "分裂顺序对照：同样 K=4 且都按中位数切，方差优先/广度均衡顺序的 MSE=%.3f，而始终先切最左桶的深度优先顺序 MSE=%.3f；后者在 K=4 时仍留一个宽 128 的大桶，曲线整体抬高，说明分裂顺序改变误差曲线。\n", r4.gotMSE, r4.leftMSE)
	fmt.Fprintf(f, "若偏差明显变大（例如 >20%%），应审计分裂维：把“维”误取为像素下标/空间位置而不是灰度值方差轴时，固定随机置换渐变的连续四块在 K=16 仍有 MSE≈%.1f，远高于预测的 21.500；这不是桶数公式失效，而是分裂维没有对准误差扩展方向。\n", blockMSE(shuffledRamp(), 16))
	fmt.Fprintln(f)

	fmt.Fprintln(f, "## 4. 关闭调色板时的一致性")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "`Palette=false`（缺省）时 `api.Encode` 不调用量化器，PGM/BMP/RLE 字节路径与改动前相同；`go test ./...` 中的 `TestGoldenBytes` 继续逐字节比对 `testdata/golden/`。开启调色板只改变像素负载，不改变图像尺寸和各格式文件头。")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "完整 K=1..64 数据见 `curve.csv`；`ramp-k4.pgm`、`ramp-k16.pgm`、`ramp-k64.pgm` 为三档产物。")
	return nil
}

// shuffledRamp 返回固定 LCG/Fisher–Yates 置换的 256 级渐变重复样本。
func shuffledRamp() []uint8 {
	pix := rampPixels()
	var state uint32 = 0x5eed1234
	for i := len(pix) - 1; i > 0; i-- {
		state = state*1664525 + 1013904223
		j := int(state>>16) % (i + 1)
		pix[i], pix[j] = pix[j], pix[i]
	}
	return pix
}

// blockMSE 模拟错误地沿原始像素下标/空间维切 K 个连续块，而不是沿灰度值维
// 做中位切分。每块用均值四舍五入作为代表值。
func blockMSE(pix []uint8, k int) float64 {
	n := len(pix)
	var sse float64
	for b := 0; b < k; b++ {
		lo := b * n / k
		hi := (b + 1) * n / k
		var sum int
		for _, v := range pix[lo:hi] {
			sum += int(v)
		}
		p := int(math.Round(float64(sum) / float64(hi-lo)))
		for _, v := range pix[lo:hi] {
			d := int(v) - p
			sse += float64(d * d)
		}
	}
	return sse / float64(n)
}
