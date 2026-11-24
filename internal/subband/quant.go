package subband

import "math"

// Stats 是单个子带的统计量。
type Stats struct {
	Label string
	Kind  string
	Level int
	N     int
	Mean  float64 // 系数均值；0 比特时用均值整体重构
	Var   float64 // 去均值方差 σ²；也是 0 比特均值重构的系数域 MSE
	Std   float64
	Min   float64
	Max   float64
	Range float64
}

// ComputeStats 计算子带均值、总体方差与极值。
func ComputeStats(b Band) Stats {
	var sum, sumSq float64
	mn, mx := math.Inf(1), math.Inf(-1)
	for _, c := range b.Coef {
		sum += c
		sumSq += c * c
		if c < mn {
			mn = c
		}
		if c > mx {
			mx = c
		}
	}
	n := float64(b.N)
	mean := sum / n
	variance := sumSq/n - mean*mean
	if variance < 0 {
		variance = 0
	}
	return Stats{
		Label: b.Label, Kind: b.Kind, Level: b.Level, N: b.N,
		Mean: mean, Var: variance, Std: math.Sqrt(variance),
		Min: mn, Max: mx, Range: mx - mn,
	}
}

// QuantBand 是一个子带在某比特数下的量化结果与实测失真。
type QuantBand struct {
	Bits int
	Lo   float64 // 量化区间下界（写头用）
	Step float64 // 量化步长 Δ
	Idx  []int   // 每个系数的量化下标（打包用）
	Recon []float64
	MSE  float64
}

// MaxBitsPerCoef 限制每系数最多 8 比特（256 级）。
const MaxBitsPerCoef = 8

// quantMSEUnder 计算给定区间 [lo,hi] 上 L 级均匀箱中心量化的 MSE，
// 可选地把下标/重构写入 idx/recon（传 nil 则只算 MSE）。
func quantMSEUnder(coef []float64, lo, hi float64, levels int, idx []int, recon []float64) float64 {
	step := (hi - lo) / float64(levels)
	if step <= 0 {
		return math.Inf(1)
	}
	var se float64
	for i, c := range coef {
		k := int((c - lo) / step)
		if k >= levels {
			k = levels - 1
		}
		if k < 0 {
			k = 0
		}
		r := lo + (float64(k)+0.5)*step
		d := c - r
		se += d * d
		if idx != nil {
			idx[i] = k
		}
		if recon != nil {
			recon[i] = r
		}
	}
	return se / float64(len(coef))
}

// Quantize 对单个子带做 b 比特均匀量化并实测 MSE。
//   - b<=0：不发下标，整带用均值重构，MSE=σ²；
//   - b>=1：在若干“以均值为中心、半宽 R=kσ”的候选区间以及实测 [Min,Max]
//     区间中，选实测 MSE 最小的一个，再做 2^b 级均匀量化、箱中心重构。
//
// 端点优选是为了适配子带系数的重尾分布：低比特时宁可让极少数边缘大系数
// 削波，也要让占多数的近零系数落在合理箱内，避免“1 比特比 0 比特更差”的
// 量化器病态。选出的 Lo/Step 与普通均匀量化流格式完全一致。
func Quantize(b Band, st Stats, bits int) QuantBand {
	q := QuantBand{Bits: bits, Recon: make([]float64, b.N), Idx: make([]int, b.N)}
	if bits <= 0 || st.Var <= 1e-12 {
		for i := range q.Recon {
			q.Recon[i] = st.Mean
		}
		var se float64
		for i, c := range b.Coef {
			d := c - st.Mean
			se += d * d
			q.Idx[i] = 0
		}
		q.MSE = se / float64(b.N)
		q.Lo = st.Mean
		return q
	}
	levels := 1 << uint(bits)

	// 候选半宽（单位 σ）。细网格覆盖从“激进削波”到“完整范围”。
	ks := []float64{0.6, 0.8, 1.0, 1.3, 1.6, 2.0, 2.5, 3.0, 4.0, 6.0, 9.0}
	bestMSE := math.Inf(1)
	var bestLo, bestHi float64
	consider := func(lo, hi float64) {
		if hi-lo < 1e-9 {
			return
		}
		mse := quantMSEUnder(b.Coef, lo, hi, levels, nil, nil)
		if mse < bestMSE {
			bestMSE = mse
			bestLo, bestHi = lo, hi
		}
	}
	for _, k := range ks {
		r := k * st.Std
		if r > 1e-9 {
			consider(st.Mean-r, st.Mean+r)
		}
	}
	// 实测完整范围（不对称也保留）。
	consider(st.Min, st.Max)
	// 以均值为中心、覆盖完整范围的对称区间。
	rFull := math.Max(st.Mean-st.Min, st.Max-st.Mean)
	if rFull > 1e-9 {
		consider(st.Mean-rFull, st.Mean+rFull)
	}

	q.Lo = bestLo
	q.Step = (bestHi - bestLo) / float64(levels)
	q.MSE = quantMSEUnder(b.Coef, bestLo, bestHi, levels, q.Idx, q.Recon)
	return q
}

// PSNR 由像素域 MSE 计算峰值信噪比（峰值 255）。
func PSNR(mse float64) float64 {
	if mse <= 1e-12 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/mse)
}
