package hybrid

import "sort"

// BlockStat 是单个块的统计量、两条路径的真实失真与判定结果。
type BlockStat struct {
	BX, BY    int
	N         int
	Sum       int64
	SumSq     int64
	Mean      float64
	Variance  float64
	Sigma     float64
	Flat      bool
	FlatLevel uint8
	// MSEFlat / MSETex 是该块分别走平坦/纹理路径时的逐像素均方误差（真实值）。
	MSEFlat float64
	MSETex  float64
	// MaxAbsFlat / MaxAbsTex 是对应路径的最大绝对像素误差。
	MaxAbsFlat int
	MaxAbsTex  int
}

// Region 汇总一类区域。
type Region struct {
	Blocks   int
	Pixels   int
	MSE      float64 // 区域逐像素均方误差（按像素加权）
	MaxBlock float64 // 块均方误差的最大值
	MaxAbs   int     // 最大绝对像素误差
	Patches  int     // 色块数：平坦区每块一个单色块；纹理区为 0
	Colors   int     // 平坦区使用的不同灰度级数
}

// SweepPoint 是阈值扫描上的一个可行切分点：τ 取相邻两块 σ 的中点。
type SweepPoint struct {
	Tau        float64 // 阈值
	FlatBlocks int
	FlatShare  float64 // 平坦块占比
	MSEFlat    float64 // 平坦类平均逐像素 MSE（此刻入类的块）
	MSETex     float64 // 纹理类平均逐像素 MSE
	Total      float64 // 整幅平均逐像素 MSE（真实值）
	TotalModel float64 // 整幅平均逐像素 MSE（模型值）
}

// Analysis 是一幅图在阈值 τ 下的完整分区与失真报告。
type Analysis struct {
	W, H   int
	Tau    float64 // 采用的阈值 σ
	TauSq  float64
	Blocks []BlockStat
	Mask   []uint8 // 0 平坦 / 255 纹理，宽高与图相同
	Flat   Region
	Tex    Region
	Sweep  []SweepPoint
	// MinK 是真实总失真最小的切分点（平坦块数），MinTau 是该切分点对应的阈值。
	MinK   int
	MinTau float64
	// ModelK 是按模型阈值 τ* 实际选出的平坦块数；[Lo,Hi] 是使该切分保持最优的
	// 阈值区间（即第 ModelK 块与第 ModelK+1 块 σ 之间的开区间）。
	ModelK int
	Lo, Hi float64
}

// Analyze 统计整幅图，按模型阈值分区，并给出扫描曲线。
func Analyze(w, h int, pix []uint8) *Analysis {
	a := &Analysis{W: w, H: h, Tau: Tau, TauSq: TauSq}
	a.Blocks = Stats(w, h, pix)
	a.Mask = make([]uint8, w*h)
	for _, st := range a.Blocks {
		if !st.Flat {
			for y := st.BY * Block; y < st.BY*Block+Block; y++ {
				for x := st.BX * Block; x < st.BX*Block+Block; x++ {
					a.Mask[y*w+x] = 255
				}
			}
		}
	}
	var flatPix, texPix int
	for _, st := range a.Blocks {
		if st.Flat {
			flatPix += st.N
			a.Flat.Blocks++
			a.Flat.Patches++ // 平坦块即一个色块
			a.Flat.MSE += st.MSEFlat * float64(st.N)
			if st.MSEFlat > a.Flat.MaxBlock {
				a.Flat.MaxBlock = st.MSEFlat
			}
			if st.MaxAbsFlat > a.Flat.MaxAbs {
				a.Flat.MaxAbs = st.MaxAbsFlat
			}
		} else {
			texPix += st.N
			a.Tex.Blocks++
			a.Tex.MSE += st.MSETex * float64(st.N)
			if st.MSETex > a.Tex.MaxBlock {
				a.Tex.MaxBlock = st.MSETex
			}
			if st.MaxAbsTex > a.Tex.MaxAbs {
				a.Tex.MaxAbs = st.MaxAbsTex
			}
		}
	}
	a.Flat.Pixels, a.Tex.Pixels = flatPix, texPix
	if flatPix > 0 {
		a.Flat.MSE /= float64(flatPix)
	}
	if texPix > 0 {
		a.Tex.MSE /= float64(texPix)
	}
	colors := map[uint8]struct{}{}
	for _, st := range a.Blocks {
		if st.Flat {
			colors[st.FlatLevel] = struct{}{}
		}
	}
	a.Flat.Colors = len(colors)
	a.Sweep = sweep(a.Blocks)
	a.MinK, a.MinTau = minSweep(a.Sweep)
	// K(τ*)：按模型阈值实际选出的平坦块数，以及包含它的最小平台 [Lo,Hi]。
	a.ModelK = 0
	for _, st := range a.Blocks {
		if st.Sigma < Tau {
			a.ModelK++
		}
	}
	a.Lo, a.Hi = plateau(a.Blocks, a.ModelK)
	return a
}

// Stats 逐块统计两条路径的真实失真。
func Stats(w, h int, pix []uint8) []BlockStat {
	bpr, bpc := w/Block, h/Block
	out := make([]BlockStat, 0, bpr*bpc)
	for by := 0; by < bpc; by++ {
		for bx := 0; bx < bpr; bx++ {
			var sum, sumSq int64
			for y := by * Block; y < by*Block+Block; y++ {
				row := y * w
				for x := bx * Block; x < bx*Block+Block; x++ {
					v := int64(pix[row+x])
					sum += v
					sumSq += v * v
				}
			}
			n := Block * Block
			st := BlockStat{
				BX: bx, BY: by, N: n,
				Sum: sum, SumSq: sumSq,
				Mean:     Mean(n, sum),
				Variance: Variance(n, sum, sumSq),
				Flat:     FlatByModel(n, sum, sumSq),
			}
			st.Sigma = mathSqrt(st.Variance)
			st.FlatLevel = FlatLevel(n, sum)
			var seFlat, seTex int64
			for y := by * Block; y < by*Block+Block; y++ {
				row := y * w
				for x := bx * Block; x < bx*Block+Block; x++ {
					v := int(pix[row+x])
					df := v - int(st.FlatLevel)
					ad := absInt(df)
					if ad > st.MaxAbsFlat {
						st.MaxAbsFlat = ad
					}
					seFlat += int64(df * df)
					e := dither(x, y, w)
					dt := v - int(texRecon(v, e))
					ad = absInt(dt)
					if ad > st.MaxAbsTex {
						st.MaxAbsTex = ad
					}
					seTex += int64(dt * dt)
				}
			}
			fn := float64(n)
			st.MSEFlat = float64(seFlat) / fn
			st.MSETex = float64(seTex) / fn
			out = append(out, st)
		}
	}
	return out
}

// sweep 按块 σ 升序逐个把块并入平坦类，逐步累计真实总失真与模型总失真。
func sweep(blocks []BlockStat) []SweepPoint {
	order := make([]int, len(blocks))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return blocks[order[i]].Sigma < blocks[order[j]].Sigma
	})
	n := Block * Block
	var total, model float64
	for _, st := range blocks {
		total += st.MSETex * float64(n)
		model += TexMSE * float64(n)
	}
	// 扫描快照用的累计量。texSum 为尚未并入平坦类的块的纹理路径平方误差和；
	// flatMSE 为已并入平坦类的块的平坦路径平方误差和。
	var flatMSE, texSum float64
	for _, st := range blocks {
		texSum += st.MSETex * float64(n)
	}
	out := make([]SweepPoint, 0, len(blocks)+1)
	for k := 0; k <= len(blocks); k++ {
		p := SweepPoint{FlatBlocks: k, FlatShare: float64(k) / float64(len(blocks))}
		p.Total = total / float64(len(blocks)*n)
		p.TotalModel = model / float64(len(blocks)*n)
		if k > 0 {
			p.MSEFlat = flatMSE / float64(k*n)
		}
		if k < len(blocks) {
			p.MSETex = texSum / float64((len(blocks)-k)*n)
		}
		switch {
		case k == 0:
			p.Tau = 0
		case k == len(blocks):
			p.Tau = blocks[order[k-1]].Sigma + 1
		default:
			p.Tau = (blocks[order[k-1]].Sigma + blocks[order[k]].Sigma) / 2
		}
		out = append(out, p)
		if k == len(blocks) {
			break
		}
		st := blocks[order[k]]
		total += (st.MSEFlat - st.MSETex) * float64(n)
		model += (st.Sigma*st.Sigma + FlatOff - TexMSE) * float64(n)
		flatMSE += st.MSEFlat * float64(n)
		texSum -= st.MSETex * float64(n)
	}
	return out
}

// minSweep 返回真实总失真的最小切分点与对应阈值；平局取第一个（最小 τ）。
func minSweep(sw []SweepPoint) (int, float64) {
	best := 0
	for i := 1; i < len(sw); i++ {
		if sw[i].Total < sw[best].Total {
			best = i
		}
	}
	return best, sw[best].Tau
}

// plateau 返回使“平坦块数恰为 k”保持不变的阈值区间 [σ_(k), σ_(k+1))。
// k=0 时下界为 0；k=n 时上界取 σ_(n)+1。
func plateau(blocks []BlockStat, k int) (lo, hi float64) {
	order := make([]int, len(blocks))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return blocks[order[i]].Sigma < blocks[order[j]].Sigma })
	lo, hi = 0, orderSigma(blocks, order, len(order)-1)+1
	if k > 0 {
		lo = orderSigma(blocks, order, k-1)
	}
	if k < len(order) {
		hi = orderSigma(blocks, order, k)
	}
	return lo, hi
}

func orderSigma(blocks []BlockStat, order []int, k int) float64 { return blocks[order[k]].Sigma }

// Total 返回整幅按像素加权的逐像素均方误差（真实值）。
func (a *Analysis) Total() float64 {
	var se float64
	for _, st := range a.Blocks {
		if st.Flat {
			se += st.MSEFlat * float64(st.N)
		} else {
			se += st.MSETex * float64(st.N)
		}
	}
	return se / float64(a.W*a.H)
}

// TotalModel 返回按模型（平坦 σ²+1/12、纹理 24）计算的整幅均方误差。
func (a *Analysis) TotalModel() float64 {
	var s float64
	for _, st := range a.Blocks {
		if st.Flat {
			s += st.Sigma*st.Sigma + FlatOff
		} else {
			s += TexMSE
		}
	}
	return s / float64(len(a.Blocks))
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
