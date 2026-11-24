package quantfb

import "math"

// Stats 是一组残差/误差指标。
type Stats struct {
	QuantRMSE  float64 `json:"quant_rmse"`   // sqrt(mean e²)，回路局部量化误差
	QuantMax   float64 `json:"quant_max"`    // max |e|，理论 ≤ q/2
	OutRMSE    float64 `json:"out_rmse"`     // sqrt(mean (Out−x)²)
	OutMax     float64 `json:"out_max"`      // max |Out−x|
	LFSharePi4 float64 `json:"lf_share_pi4"` // 行方向 DFT、ω≤π/4 的低频能量占比
	LFSharePi8 float64 `json:"lf_share_pi8"` // ω≤π/8
	LFRMSPi4   float64 `json:"lf_rms_pi4"`   // ω≤π/4 理想低通后的绝对 RMS
	Box3RMS    float64 `json:"box3_rms"`     // 3×3 盒式低通后残差 RMS（二维低频）
	Box5RMS    float64 `json:"box5_rms"`     // 5×5 盒式低通后残差 RMS
	Bound      float64 `json:"bound"`        // 理论输出误差界
	BoundHolds bool    `json:"bound_holds"`  // 全图实测 max|OutErr| ≤ Bound
}

// Analyze 从 Result 计算全部指标。bound<0 时不做误差界核对。
func Analyze(r *Result, bound float64) Stats {
	n := float64(r.W * r.H)
	var se, sd float64
	var me, md float64
	for i := 0; i < r.W*r.H; i++ {
		se += r.QErr[i] * r.QErr[i]
		sd += r.OutErr[i] * r.OutErr[i]
		me = math.Max(me, math.Abs(r.QErr[i]))
		md = math.Max(md, math.Abs(r.OutErr[i]))
	}
	s := Stats{
		QuantRMSE:  math.Sqrt(se / n),
		QuantMax:   me,
		OutRMSE:    math.Sqrt(sd / n),
		OutMax:     md,
		LFSharePi4: RowLFShare(r.OutErr, r.W, r.H, 1.0/8.0),
		LFSharePi8: RowLFShare(r.OutErr, r.W, r.H, 1.0/16.0),
		LFRMSPi4:   RowLFRMS(r.OutErr, r.W, r.H, 1.0/8.0),
		Box3RMS:    BoxLowRMS(r.OutErr, r.W, r.H, 1),
		Box5RMS:    BoxLowRMS(r.OutErr, r.W, r.H, 2),
		Bound:      bound,
	}
	if bound >= 0 {
		s.BoundHolds = md <= bound+1e-9
	}
	return s
}

// RowLFShare 计算逐行 DFT 的低频能量占比（所有行求和后作比）。
//
//	kmax=floor(W·binFrac)；ω_k=2πk/W，故 binFrac=1/8 对应 ω≤π/4。
//
// 分子直接计算单侧 DFT 的 k=1..kmax；分母由 Parseval 给出
// （与 numpy.fft.rfft 的原始单侧功率同口径）：
//
//	Σ_{k=1}^{W/2}|X_k|² = (W·Σ_n(x_n−mean)² + |X_{W/2}|²)/2。
func RowLFShare(d []float64, W, H int, binFrac float64) float64 {
	kmax := int(math.Floor(float64(W) * binFrac))
	if kmax < 1 {
		kmax = 1
	}
	if kmax > W/2 {
		kmax = W / 2
	}
	var num, den float64
	for y := 0; y < H; y++ {
		base := y * W
		var mean float64
		for x := 0; x < W; x++ {
			mean += d[base+x]
		}
		mean /= float64(W)
		var centered2 float64
		for x := 0; x < W; x++ {
			v := d[base+x] - mean
			centered2 += v * v
		}
		// X_{W/2} = Σ (d−mean)(−1)^x
		var nyq float64
		for x := 0; x < W; x++ {
			v := d[base+x] - mean
			if x%2 == 0 {
				nyq += v
			} else {
				nyq -= v
			}
		}
		den += (float64(W)*centered2 + nyq*nyq) / 2

		for k := 1; k <= kmax; k++ {
			var re, im float64
			ang := -2 * math.Pi * float64(k) / float64(W)
			for x := 0; x < W; x++ {
				v := d[base+x] - mean
				a := ang * float64(x)
				re += v * math.Cos(a)
				im += v * math.Sin(a)
			}
			num += re*re + im*im
		}
	}
	if den == 0 {
		return 0
	}
	return num / den
}

// RowLFRMS 返回逐行理想低通（保留 DC 与 k≤kmax）后信号的整体 RMS，
// 是低频的**绝对**幅度指标，避免能量被高频总功率稀释成占比假象。
func RowLFRMS(d []float64, W, H int, binFrac float64) float64 {
	kmax := int(math.Floor(float64(W) * binFrac))
	if kmax < 1 {
		kmax = 1
	}
	if kmax >= W/2 {
		kmax = W/2 - 1
	}
	var sum2 float64
	for y := 0; y < H; y++ {
		base := y * W
		var mean float64
		for x := 0; x < W; x++ {
			mean += d[base+x]
		}
		mean /= float64(W)
		reX := make([]float64, kmax+1)
		imX := make([]float64, kmax+1)
		for k := 1; k <= kmax; k++ {
			ang := -2 * math.Pi * float64(k) / float64(W)
			for x := 0; x < W; x++ {
				v := d[base+x] - mean
				a := ang * float64(x)
				reX[k] += v * math.Cos(a)
				imX[k] += v * math.Sin(a)
			}
		}
		for n := 0; n < W; n++ {
			var lo float64
			for k := 1; k <= kmax; k++ {
				a := 2 * math.Pi * float64(k) * float64(n) / float64(W)
				lo += reX[k]*math.Cos(a) - imX[k]*math.Sin(a)
			}
			lo *= 2 / float64(W) // 合并 ±k 边带
			sum2 += lo * lo
		}
	}
	return math.Sqrt(sum2 / float64(W*H))
}

// BoxLowRMS 计算 (2r+1)×(2r+1) 盒式均值低通后，内框残差的 RMS。
// 用积分图在 O(WH) 内完成。
func BoxLowRMS(d []float64, W, H, r int) float64 {
	if r < 0 {
		r = 0
	}
	// 积分图，尺寸 (H+1)×(W+1)
	st := make([]float64, (H+1)*(W+1))
	at := func(y, x int) float64 { return st[y*(W+1)+x] }
	for y := 1; y <= H; y++ {
		var rowSum float64
		for x := 1; x <= W; x++ {
			rowSum += d[(y-1)*W+(x-1)]
			st[y*(W+1)+x] = at(y-1, x) + rowSum
		}
	}
	var sum2 float64
	var cnt int
	for y := r; y < H-r; y++ {
		for x := r; x < W-r; x++ {
			y0, y1 := y-r, y+r+1
			x0, x1 := x-r, x+r+1
			tot := at(y1, x1) - at(y0, x1) - at(y1, x0) + at(y0, x0)
			v := tot / float64((2*r+1)*(2*r+1))
			sum2 += v * v
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return math.Sqrt(sum2 / float64(cnt))
}

// BoundAt 计算给定系数与步长的理论输出误差界 B=(1+|c1|+|c2|)q/2。
func BoundAt(q float64, c1, c2 float64) float64 {
	return (1 + CoeffL1(c1, c2)) * q / 2
}
