package subband

import (
	"errors"
)

var (
	errBitsLen     = errors.New("比特向量长度与子带数不符")
	errCapTooSmall = errors.New("字节上限连固定头部都放不下")
)

// Setup 保存一幅图的分解结果及预计算的失真表。
type Setup struct {
	W, H, Levels int
	N            int
	Pix          []uint8
	Bands        []Band
	Stats        []Stats
	Weights      []float64
	NS           []int
	Dist         [][]float64 // Dist[i][b] = weight_i * 实测 MSE_i(b)
	Model        [][]float64 // Model[i][b] = weight_i σ_i² 4^-b
}

// NewSetup 分解图像并准备统计/失真表。
func NewSetup(pix []uint8, w, h, levels int) (*Setup, error) {
	bands, err := Decompose(pix, w, h, levels)
	if err != nil {
		return nil, err
	}
	st := make([]Stats, len(bands))
	ns := make([]int, len(bands))
	for i, b := range bands {
		st[i] = ComputeStats(b)
		ns[i] = b.N
	}
	wts := SizeWeights(bands, w, h)
	return &Setup{
		W: w, H: h, Levels: levels, N: w * h, Pix: pix,
		Bands: bands, Stats: st, Weights: wts, NS: ns,
		Dist:  DistortionTables(bands, st, wts),
		Model: ModelDistortionTables(st, wts),
	}, nil
}

// RunResult 是给定整数比特向量的一次完整实验结果。
type RunResult struct {
	Bits             []int
	Used             int
	Quants           []QuantBand
	CoefReconBands    []Band
	Lin              []float64 // 浮点重构（量化后系数逆变换）
	Recon            []uint8   // 裁剪取整后的最终图
	MSE              float64   // 相对原图的像素域实测 MSE
	LinMSE           float64   // 浮点重构相对 float 原图的 MSE
	WeightedCoefMSE  float64   // Σ weight_i MSE_i，Parseval 下等于 LinMSE
	PSNR             float64
	BandErr          [][]float64 // 每带像素域误差图
	Art              ArtifactStats
}

// RunBits 按给定每带每系数比特向量执行量化、重构与统计。
func (s *Setup) RunBits(bits []int, tau float64) (*RunResult, error) {
	if len(bits) != len(s.Bands) {
		return nil, errBitsLen
	}
	r := &RunResult{Bits: append([]int(nil), bits...), Used: TotalBits(s.NS, bits)}
	r.Quants = make([]QuantBand, len(s.Bands))
	rb := make([]Band, len(s.Bands))
	var wmse float64
	for i, b := range s.Bands {
		q := Quantize(b, s.Stats[i], bits[i])
		r.Quants[i] = q
		wmse += s.Weights[i] * q.MSE
		nb := b
		nb.Coef = q.Recon
		rb[i] = nb
	}
	r.WeightedCoefMSE = wmse
	r.CoefReconBands = rb

	lin, err := Reconstruct(rb, s.W, s.H, s.Levels)
	if err != nil {
		return nil, err
	}
	r.Lin = lin
	r.Recon = ClampPixels(lin)

	var seLin, sePix float64
	for i := 0; i < s.N; i++ {
		d := lin[i] - float64(s.Pix[i])
		seLin += d * d
		dp := float64(r.Recon[i]) - float64(s.Pix[i])
		sePix += dp * dp
	}
	r.LinMSE = seLin / float64(s.N)
	r.MSE = sePix / float64(s.N)
	r.PSNR = PSNR(r.MSE)

	// 逐带误差图。
	r.BandErr = make([][]float64, len(s.Bands))
	for i, b := range s.Bands {
		delta := make([]float64, b.N)
		for k := range delta {
			delta[k] = r.Quants[i].Recon[k] - b.Coef[k]
		}
		e, err := BandErrorImage(s.Bands, i, delta, s.W, s.H, s.Levels)
		if err != nil {
			return nil, err
		}
		r.BandErr[i] = e
	}
	r.Art = AnalyzeArtifacts(s.Pix, s.W, s.H, s.Levels, s.Bands, r.BandErr, tau)
	return r, nil
}

// BestInteger 用方差解析模型做整数反向注水（精确/不超预算）。
func (s *Setup) BestInteger(budget int, exact bool) DPResult {
	return AllocateInteger(s.Model, s.NS, budget, exact)
}

// BestIntegerMeasured 用实测量化器失真表做整数最优分配（对照/预言机）。
func (s *Setup) BestIntegerMeasured(budget int, exact bool) DPResult {
	return AllocateInteger(s.Dist, s.NS, budget, exact)
}

// MSEAt 方便外部读取某带在某比特下的实测（未加权）MSE。
func (s *Setup) MSEAt(i, bits int) float64 {
	if bits < 0 {
		bits = 0
	}
	if bits > MaxBitsPerCoef {
		bits = MaxBitsPerCoef
	}
	return s.Dist[i][bits] / s.Weights[i]
}
