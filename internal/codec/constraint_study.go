package codec

// 约束系统与三档块长实验的可复现入口。
//
// 对同一张图、三档块长 B=4/8/16（都能整除画幅），各做：
//   1. 算块直流网格 → 块间边缘强度；
//   2. 用两不等式判定可行性，列出冲突边/冲突块；
//   3. 可行档：差分约束求一组同时满足 (DC) 与 (SEAM) 的整数重建，实测缝宽与直流偏差；
//   4. 不可行档：分别构造“放弃连续性”与“放弃直流”两条重建，量出各自的量化代价。

// FixtureName 是内置测试图的名字。
type FixtureName string

const (
	// FixtureGradient 是双线性渐变 pix=32+2x+y：三档都可行，缝宽随块长线性增大。
	FixtureGradient FixtureName = "gradient"
	// FixtureQuadratic 是二次缓坡：小档可行、大档不可行，制造“因块长而异”的不可行。
	FixtureQuadratic FixtureName = "quadratic"
	// FixtureTexture 是细棋盘格纹理叠加线缓慢变：三档都因细节被块均值抹平而不可行。
	FixtureTexture FixtureName = "texture"
)

// StudyParams 是三档实验采用的约束预算。
//
// ρ=8、τ=16 使三张图分别落在三种情形：
//   - gradient：三档全可行（缝宽随块长线性增大，但都在 2ρ+τ 之内）；
//   - quadratic：4/8 可行、16 不可行（不可行只出现在最长档）；
//   - texture：三档全不可行（细纹理在任何块长下都被块均值抹成常数）。
var StudyParams = ConstraintParams{Rho: 8, Tau: 16}

// FixtureImage 生成内置确定性测试图。
func FixtureImage(name FixtureName, w, h int) []uint8 {
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var v int
			switch name {
			case FixtureGradient:
				v = 32 + 2*x + y
			case FixtureQuadratic:
				// 20 + 2x + 0.01x² + y/4（64 宽时取整后 0..232，不截断）
				v = int(20 + 2*float64(x) + 0.01*float64(x*x) + float64(y)/4 + 0.5)
			case FixtureTexture:
				// 左右两半有 160 级的大尺度亮度差，各自再叠 4×4 细棋盘（±20）。
				// 细纹理在任何块长下都被块均值抹成常数，但左右大尺度差在每档都保留，
				// 于是三档块长的块直流都出现 ~160 级的边缘强度，全部不可行。
				base := 40
				if x >= w/2 {
					base = 200
				}
				if ((x/4)+(y/4))%2 == 0 {
					base += 20
				} else {
					base -= 20
				}
				v = base
			default:
				v = 128
			}
			pix[y*w+x] = uint8(clampInt(v, 0, 255))
		}
	}
	return pix
}

// TierResult 是某一档块长的完整实测结果。
type TierResult struct {
	BlockSize int
	Feasible  bool

	// 块间边缘强度与阈值。
	EdgeThreshold   int
	CornerThreshold int
	EdgeMax         int
	EdgeMeanMilli   int
	CornerMax       int
	CornerMeanMilli int

	ConflictEdges  []BlockEdge
	ConflictBlocks [][2]int

	// 可行档：约束内重建。
	Constrained      bool
	ConstrainedSeam  DevStat // 跨块缝宽（应 ≤ τ）
	ConstrainedDCDev DevStat // 相对块直流的偏差（应 ≤ ρ）
	ConstrainedMSE   float64

	// 参照：纯直流重建（不连续）。
	FlatSeam  DevStat
	FlatDCDev DevStat
	FlatMSE   float64

	// 不可行档的两条重建各自代价。
	KeepDC   ReconstructCost
	KeepSeam ReconstructCost

	// 不可行档的解析代价（可行档为零值）。
	Quant TierQuantities
}

func (tr TierResult) costSeamGiveUp() int   { return tr.Quant.SeamGiveUpMax }
func (tr TierResult) costDCGiveUpHalf() int { return tr.Quant.DCGiveUpHalf }

// ReconstructCost 是一条“放弃某条约束”的重建的量化代价。
type ReconstructCost struct {
	SeamMax        int // 实测最大缝宽（灰度级）
	SeamMeanMilli  int // 实测平均缝宽 ×1000
	DCDevMax       int // 实测最大直流偏差（灰度级）
	DCDevMeanMilli int
	MSE            float64
}

// TierQuantities 是不可行档“必须放弃其中一条”的解析代价（灰度级）。
type TierQuantities struct {
	// Shortfall 是不等式①的超出量：EdgeMax − (2ρ+τ)，>0 即边不可行。
	Shortfall int
	// SeamGiveUpMax 是“放弃连续性、守住直流”时最强冲突边上无法消除的缝宽：
	//   EdgeMax − 2ρ = τ + Shortfall（当不可行由边冲突主导）。
	SeamGiveUpMax int
	// DCGiveUpHalf 是“守住连续性、放弃直流”时最大直流偏差的两倍。
	// 边关系要求 EdgeMax − τ；角点两步关系要求 CornerMax − 2τ，取大者。
	// 保留两倍是为了不丢半级——单侧偏差可能落在 .5 上。
	DCGiveUpHalf int
}

// TierCosts 由可行性结果与参数解析地给出不可行档的两条代价。
func (f Feasibility) TierCosts() TierQuantities {
	var q TierQuantities
	if f.Feasible {
		return q
	}
	// 本实验的 B=16 由边冲突主导；边、角两个下界都计入 DC 代价，避免角点-only 反例漏估。
	s := f.EdgeMax - f.EdgeThreshold
	if s < 0 {
		s = 0
	}
	q.Shortfall = s
	q.SeamGiveUpMax = f.EdgeMax - 2*f.Params.Rho
	if q.SeamGiveUpMax < 0 {
		q.SeamGiveUpMax = 0
	}
	q.DCGiveUpHalf = max(f.EdgeMax-f.Params.Tau, f.CornerMax-2*f.Params.Tau)
	if q.DCGiveUpHalf < 0 {
		q.DCGiveUpHalf = 0
	}
	return q
}

// DCGiveUpMaxText 把 DCGiveUpHalf 渲染成“整数或 .5”的最大偏差。
func (q TierQuantities) DCGiveUpMaxText() string {
	if q.DCGiveUpHalf%2 == 0 {
		return itoa(q.DCGiveUpHalf / 2)
	}
	return itoa(q.DCGiveUpHalf/2) + ".5"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func mse(pix, ref []uint8) float64 {
	if len(pix) != len(ref) || len(pix) == 0 {
		return 0
	}
	var s float64
	for i := range pix {
		d := float64(int(pix[i]) - int(ref[i]))
		s += d * d
	}
	return s / float64(len(pix))
}

func costOf(w, h, bs int, f DCField, pix []uint8) ReconstructCost {
	s := MeasureSeamStat(w, h, bs, pix)
	d := MeasureDCDevStat(f, pix)
	return ReconstructCost{
		SeamMax: s.Max, SeamMeanMilli: s.MeanMilli,
		DCDevMax: d.Max, DCDevMeanMilli: d.MeanMilli,
	}
}

// RunTier 对一张图、一档块长跑完整个判定与重建。
func RunTier(name FixtureName, w, h, bs int, p ConstraintParams) TierResult {
	pix := FixtureImage(name, w, h)
	f := BlockDCField(w, h, bs, pix)
	feas := CheckFeasibility(f, p)

	r := TierResult{
		BlockSize: bs, Feasible: feas.Feasible,
		EdgeThreshold: feas.EdgeThreshold, CornerThreshold: feas.CornerThreshold,
		EdgeMax: feas.EdgeMax, CornerMax: feas.CornerMax,
		ConflictEdges: feas.ConflictEdges, ConflictBlocks: feas.ConflictBlocks,
	}
	// 均值：边/角的和 / 个数。
	edgeSum, edgeN := 0, 0
	for by := 0; by < f.NY; by++ {
		for bx := 0; bx < f.NX; bx++ {
			if bx+1 < f.NX {
				edgeSum += cAbs(f.At(bx+1, by) - f.At(bx, by))
				edgeN++
			}
			if by+1 < f.NY {
				edgeSum += cAbs(f.At(bx, by+1) - f.At(bx, by))
				edgeN++
			}
		}
	}
	cornerSum, cornerN := 0, 0
	for by := 0; by+1 < f.NY; by++ {
		for bx := 0; bx+1 < f.NX; bx++ {
			cornerSum += cAbs(f.At(bx+1, by+1) - f.At(bx, by))
			cornerSum += cAbs(f.At(bx, by+1) - f.At(bx+1, by))
			cornerN += 2
		}
	}
	r.EdgeMeanMilli = meanMilli(edgeSum, edgeN)
	r.CornerMeanMilli = meanMilli(cornerSum, cornerN)

	// 纯直流参照。
	flat := reconstructFlatDC(f)
	r.FlatSeam = MeasureSeamStat(w, h, bs, flat)
	r.FlatDCDev = MeasureDCDevStat(f, flat)
	r.FlatMSE = mse(flat, pix)

	if r.Feasible {
		out, ok := ReconstructConstrained(f, p)
		r.Constrained = ok
		if ok {
			r.ConstrainedSeam = MeasureSeamStat(w, h, bs, out)
			r.ConstrainedDCDev = MeasureDCDevStat(f, out)
			r.ConstrainedMSE = mse(out, pix)
		}
	} else {
		r.Quant = feas.TierCosts()
		a := ReconstructKeepDC(f, p)
		b := ReconstructKeepSeam(f, p)
		r.KeepDC = costOf(w, h, bs, f, a)
		r.KeepDC.MSE = mse(a, pix)
		r.KeepSeam = costOf(w, h, bs, f, b)
		r.KeepSeam.MSE = mse(b, pix)
	}
	return r
}

func reconstructFlatDC(f DCField) []uint8 {
	out := make([]uint8, f.W*f.H)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			out[y*f.W+x] = uint8(f.DC[f.blkOf(x, y)])
		}
	}
	return out
}

// StudyResult 是整张三档实验的结果。
type StudyResult struct {
	Fixture FixtureName
	W, H    int
	Params  ConstraintParams
	Tiers   []TierResult
}

// RunStudy 在 W×H 的图上对 B=4/8/16 三档跑完整实验。
func RunStudy(name FixtureName, w, h int, p ConstraintParams) StudyResult {
	s := StudyResult{Fixture: name, W: w, H: h, Params: p}
	for _, bs := range []int{4, 8, 16} {
		s.Tiers = append(s.Tiers, RunTier(name, w, h, bs, p))
	}
	return s
}
