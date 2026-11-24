package subband

import "math"

// BandRow 是某一档预算下单个子带的逐带对比行。
type BandRow struct {
	Name        string
	Level       int
	N           int
	Weight      float64
	Variance    float64
	WaterBits   int     // 注水整数比特
	EqualBits   int     // 平分比特
	Continuous  float64 // 注水连续比特
	WaterMSE    float64 // 注水路径该带实测失真
	EqualMSE    float64 // 平分路径该带实测失真
}

// InverseCheck 验证“各带误差与比特数成反比”。
// 均匀量化的精确关系是指数律 D/σ² ≈ c·4^-b（log 后为线性），
// 同时按题面对“反比”的字面表述拟合 D/σ² ≈ k/b，用 R² 与秩相关对照两种说法。
type InverseCheck struct {
	SampleBands int     // 参与拟合的（带,预算）样本数（仅 b≥1、σ²>0）
	LogSlope    float64 // log(D/σ²) 对 b 的斜率，理论值 -ln4 ≈ -1.386
	LogR2       float64 // 指数律拟合优度
	InvR2       float64 // 字面反比 D/σ²~1/b 的拟合优度
	Spearman    float64 // b 与实测 MSE 的 Spearman 秩相关，期望为负
}

// BudgetReport 是一档预算的完整结果。
type BudgetReport struct {
	Budget        int
	BitsPerPixel  float64
	Water         WaterfillResult
	Equal         EqualResult
	Rows          []BandRow
	WaterMSE      float64
	EqualMSE      float64
	WaterPSNR     float64
	EqualPSNR     float64
	Inverse       InverseCheck
	WaterRecon    []uint8 // 注水重构图
	EqualRecon    []uint8 // 平分重构图
}

// Report 是一次分析的全部产出。
type Report struct {
	W, H    int
	Levels  int
	NBands  int
	Stats   []Stats
	Budgets []BudgetReport
}

// Analyze 对给定图像做子带分解，并对每档预算分别跑注水与平分，
// 实测两条路径的逐带/整图失真，并验证误差-比特关系。
func Analyze(pix []uint8, w, h, levels int, budgets []int) (*Report, error) {
	bands, err := ForwardDWT(pix, w, h, levels)
	if err != nil {
		return nil, err
	}
	stats := make([]Stats, len(bands))
	for i, b := range bands {
		stats[i] = ComputeStats(b)
	}
	rep := &Report{W: w, H: h, Levels: levels, NBands: len(bands), Stats: stats}

	for _, budget := range budgets {
		br := BudgetReport{Budget: budget, BitsPerPixel: float64(budget) / float64(w*h)}
		br.Water = Waterfill(stats, budget)
		br.Equal = EqualAllocation(stats, budget)

		waterQ := make([]QuantBand, len(bands))
		equalQ := make([]QuantBand, len(bands))
		br.Rows = make([]BandRow, len(bands))
		for i, b := range bands {
			waterQ[i] = Quantize(b, stats[i], br.Water.Details[i].Bits)
			equalQ[i] = Quantize(b, stats[i], br.Equal.Details[i].Bits)
			br.Rows[i] = BandRow{
				Name:       b.Name,
				Level:      b.Level,
				N:          b.N,
				Weight:     b.Weight,
				Variance:   stats[i].Var,
				WaterBits:  waterQ[i].Bits,
				EqualBits:  equalQ[i].Bits,
				Continuous: br.Water.Details[i].Continuous,
				WaterMSE:   waterQ[i].MSE,
				EqualMSE:   equalQ[i].MSE,
			}
		}
		br.WaterMSE = WeightedMSE(waterQ, stats)
		br.EqualMSE = WeightedMSE(equalQ, stats)
		br.WaterPSNR = PSNR(br.WaterMSE)
		br.EqualPSNR = PSNR(br.EqualMSE)

		// 用重构系数分别逆变换，得到两条路径的重构图（量化误差之外 DWT 完全可逆）。
		waterBands := cloneBands(bands, waterQ)
		equalBands := cloneBands(bands, equalQ)
		if br.WaterRecon, err = InverseDWT(waterBands, w, h, levels); err != nil {
			return nil, err
		}
		if br.EqualRecon, err = InverseDWT(equalBands, w, h, levels); err != nil {
			return nil, err
		}
		br.Inverse = checkInverse(stats, waterQ)
		rep.Budgets = append(rep.Budgets, br)
	}
	return rep, nil
}

func cloneBands(bands []Band, qs []QuantBand) []Band {
	out := make([]Band, len(bands))
	for i, b := range bands {
		out[i] = Band{Name: b.Name, Level: b.Level, N: b.N, Weight: b.Weight,
			Coef: append([]float64(nil), qs[i].Recon...)}
	}
	return out
}

// checkInverse 汇总本档预算下所有带的 (b, D/σ²)，做指数律/反比律拟合与秩相关。
func checkInverse(stats []Stats, qs []QuantBand) InverseCheck {
	var pts []invPoint
	for i, q := range qs {
		if q.Bits >= 1 && stats[i].Var > 0 {
			pts = append(pts, invPoint{float64(q.Bits), q.MSE / stats[i].Var})
		}
	}
	c := InverseCheck{SampleBands: len(pts)}
	if len(pts) < 2 {
		return c
	}
	xs := make([]float64, len(pts))
	ysLog := make([]float64, len(pts))
	ysInv := make([]float64, len(pts))
	for i, p := range pts {
		xs[i] = p.b
		ysLog[i] = math.Log(p.dnorm)
		ysInv[i] = p.dnorm
	}
	// 指数律：log(D/σ²)=a+s·b
	s, intercept, rLog := leastSquares(xs, ysLog)
	c.LogSlope, c.LogR2 = s, rLog
	// 字面反比：D/σ² = k·(1/b)，过原点单变量回归
	xinv := make([]float64, len(pts))
	for i, p := range pts {
		xinv[i] = 1 / p.b
	}
	_, _, c.InvR2 = leastSquares(xinv, ysInv)
	_ = intercept
	// 秩相关：归一化失真 D/σ² 越大秩越高，比特越大则该秩应越小（负相关）。
	// 不能直接用原始 MSE 跨带比——各带方差量级不同，会被方差主导。
	rb := rank(xs)
	dn := make([]float64, len(pts))
	for i, p := range pts {
		dn[i] = p.dnorm
	}
	rm := rank(dn)
	c.Spearman = pearson(rb, rm)
	return c
}

type invPoint struct {
	b, dnorm float64
}

// leastSquares 一元线性回归 y=a+s x，返回斜率、截距与 R²。
func leastSquares(x, y []float64) (slope, intercept, r2 float64) {
	n := float64(len(x))
	var sx, sy, sxx, sxy, syy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxx += x[i] * x[i]
		sxy += x[i] * y[i]
		syy += y[i] * y[i]
	}
	den := n*sxx - sx*sx
	if math.Abs(den) < 1e-15 {
		return 0, sy / n, 0
	}
	slope = (n*sxy - sx*sy) / den
	intercept = (sy - slope*sx) / n
	ssRes, ssTot := 0.0, 0.0
	ybar := sy / n
	for i := range x {
		f := intercept + slope*x[i]
		ssRes += (y[i] - f) * (y[i] - f)
		ssTot += (y[i] - ybar) * (y[i] - ybar)
	}
	if ssTot < 1e-15 {
		return slope, intercept, 1
	}
	return slope, intercept, 1 - ssRes/ssTot
}

// rank 返回平均秩（1 起），用于 Spearman 相关。
func rank(x []float64) []float64 {
	n := len(x)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	// 简单插入排序（样本量很小）。
	for i := 1; i < n; i++ {
		for j := i; j > 0 && x[idx[j-1]] > x[idx[j]]; j-- {
			idx[j-1], idx[j] = idx[j], idx[j-1]
		}
	}
	out := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && x[idx[j+1]] == x[idx[i]] {
			j++
		}
		avg := float64(i+j+2) / 2 // 平均秩
		for k := i; k <= j; k++ {
			out[idx[k]] = avg
		}
		i = j + 1
	}
	return out
}

func pearson(x, y []float64) float64 {
	n := float64(len(x))
	var sx, sy, sxx, syy, sxy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxx += x[i] * x[i]
		syy += y[i] * y[i]
		sxy += x[i] * y[i]
	}
	den := math.Sqrt((n*sxx - sx*sx) * (n*syy - sy*sy))
	if den < 1e-15 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

// SyntheticImage 生成确定性的 64×64 测试图：
// 径向缓变（低频能量集中在 LL）+ 硬边方块（给中频）+ 弱噪声（给高频）。
func SyntheticImage(size int) (int, int, []uint8) {
	pix := make([]uint8, size*size)
	seed := uint32(0x1234abcd)
	nextRand := func() float64 {
		// 确定性 LCG，避免依赖全局 rand 状态。
		seed = seed*1103515245 + 12345
		return float64(seed>>16)/65536.0 - 0.5
	}
	c := float64(size-1) / 2.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-c, float64(y)-c) / c
			v := 200 * (1 - d) // 径向渐变
			if (x/16+y/16)%2 == 0 { // 16px 棋盘方块，制造跨带边缘
				v += 28
			}
			v += 10 * nextRand() // 弱噪声
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			pix[y*size+x] = uint8(v + 0.5)
		}
	}
	return size, size, pix
}
