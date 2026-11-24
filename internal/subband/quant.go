package subband

import "math"

// Stats 是一个子带的一阶/二阶统计量与动态范围。
type Stats struct {
	Name   string
	Level  int
	Mean   float64 // 均值 μ（零比特时用它整体重构）
	Var    float64 // 总体方差 σ²（去均值后的能量，零比特重构的失真）
	Min    float64
	Max    float64
	Range  float64 // Max-Min
	N      int
	Weight float64
}

// ComputeStats 计算一个子带的统计量。
func ComputeStats(b Band) Stats {
	var sum, sumSq, mn, mx float64
	mn, mx = math.Inf(1), math.Inf(-1)
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
		variance = 0 // 浮点误差保护
	}
	return Stats{
		Name:   b.Name,
		Level:  b.Level,
		Mean:   mean,
		Var:    variance,
		N:      b.N,
		Weight: b.Weight,
		Min:    mn,
		Max:    mx,
		Range:  mx - mn,
	}
}

// QuantBand 是一个子带的量化结果与实测失真。
type QuantBand struct {
	Name    string
	Level   int
	Bits    int     // 分配到的每系数比特 b
	Step    float64 // 量化步长 Δ
	MSE     float64 // 该带实测均方误差（像素域系数差的平方均值）
	Recon   []float64
}

// Quantize 用每系数 bits 个量化级对单个子带做均匀量化并实测失真。
//   - bits<=0：不发任何系数，整体用均值重构，失真等于方差 σ²；
//   - bits>=1：在 [Min,Max] 上均匀分 2^bits 级，重构取量化箱中心，无截断。
func Quantize(b Band, st Stats, bits int) QuantBand {
	q := QuantBand{Name: b.Name, Level: b.Level, Bits: bits, Recon: make([]float64, b.N)}
	if bits <= 0 {
		for i := range q.Recon {
			q.Recon[i] = st.Mean
		}
		q.Step = 0
		q.MSE = st.Var
		return q
	}
	levelCount := 1 << uint(bits)
	step := st.Range / float64(levelCount)
	q.Step = step
	var se float64
	for i, c := range b.Coef {
		idx := int((c - st.Min) / step)
		if idx >= levelCount { // 恰好落在 Max 上
			idx = levelCount - 1
		}
		if idx < 0 {
			idx = 0
		}
		r := st.Min + (float64(idx)+0.5)*step
		q.Recon[i] = r
		d := c - r
		se += d * d
	}
	q.MSE = se / float64(b.N)
	return q
}

// WeightedMSE 按各带能量权重 N/Nimg 汇总，等于整图重构均方误差。
func WeightedMSE(qs []QuantBand, stats []Stats) float64 {
	var mse float64
	for i := range qs {
		mse += stats[i].Weight * qs[i].MSE
	}
	return mse
}

// PSNR 由整图 MSE 反算峰值信噪比（峰值 255），单位 dB。
func PSNR(mse float64) float64 {
	if mse <= 1e-12 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255 / mse)
}
