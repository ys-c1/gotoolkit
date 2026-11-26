package codec

// 块边界连续性 vs 块直流保真：约束系统、可行性判定、冲突定位与两个代价。
//
// 记号
//   图像切成 BS×BS 的块（本文件的理论推导针对整块网格，即 W、H 都被 BS 整除；
//   尾块布局由 ExactFeasible 的差分约束求解器保守处理）。
//   μ_b  是块 b 的量化直流（blockDC，块均值四舍五入）。
//   重建像素 x_p（p 是像素下标）。
//
// 两条约束
//   (DC)   直流保真：对块 b 内每个像素 p，|x_p − μ_b| ≤ ρ。
//   (SEAM) 边界连续：对块边界两侧 4-邻接的像素对 (p,q)，|x_p − x_q| ≤ τ。
//
// 这就是“压直流”和“边界连续”两个要求的精确形式：ρ 是允许偏离块直流的
// 最大灰度级，τ 是允许的最大跨边界台阶。二者越小要求越强，经常冲突。

// ConstraintParams 是两条约束的预算。
type ConstraintParams struct {
	Rho int // (DC) 每像素允许偏离块直流的灰度级，≥0
	Tau int // (SEAM) 跨块边界相邻像素允许的最大灰度差，≥0
}

// Valid 报告预算是否合法。
func (p ConstraintParams) Valid() bool { return p.Rho >= 0 && p.Tau >= 0 }

// EdgeThreshold 是不等式①的右端：边相邻两块直流差的上限 2ρ+τ。
// 也用作“块间边缘强度”的冲突阈值——一块数值。
func (p ConstraintParams) EdgeThreshold() int { return 2*p.Rho + p.Tau }

// CornerThreshold 是不等式②的右端：角相邻（对角）两块直流差的上限 2ρ+2τ。
func (p ConstraintParams) CornerThreshold() int { return 2*p.Rho + 2*p.Tau }

// DCField 是一张图的块直流网格。
type DCField struct {
	W, H, BS int
	NX, NY   int
	DC       []int
}

// At 返回块 (bx,by) 的直流。
func (f DCField) At(bx, by int) int { return f.DC[by*f.NX+bx] }

func (f DCField) blkOf(x, y int) int { return (y/f.BS)*f.NX + x/f.BS }

// BlockDCField 计算一张灰度图的块直流网格，取整规则与 GTR2 编码完全一致。
func BlockDCField(w, h, bs int, pix []uint8) DCField {
	nx, ny := blkDims(w, h, bs)
	dc := make([]int, nx*ny)
	for by := 0; by < ny; by++ {
		for bx := 0; bx < nx; bx++ {
			x0, y0 := bx*bs, by*bs
			x1, y1 := min(x0+bs, w), min(y0+bs, h)
			dc[by*nx+bx] = int(blockDC(pix, w, x0, y0, x1, y1))
		}
	}
	return DCField{W: w, H: h, BS: bs, NX: nx, NY: ny, DC: dc}
}

// BlockEdge 是一条块间边（水平或竖直相邻的两块）及其边缘强度。
type BlockEdge struct {
	BX0, BY0 int // 一端块坐标
	BX1, BY1 int // 另一端块坐标
	Strength int // |μ_a − μ_b|，块间边缘强度
}

// CornerPair 是一对在网格角点对角相邻的块及其强度。
type CornerPair struct {
	BX0, BY0 int
	BX1, BY1 int
	Strength int
}

// Feasibility 是可行性判定的结果。
type Feasibility struct {
	Params          ConstraintParams
	EdgeThreshold   int // 2ρ+τ
	CornerThreshold int // 2ρ+2τ

	EdgeMax   int // 所有边相邻块的最大强度
	CornerMax int // 所有角相邻块的最大强度

	EdgeOK   bool // 不等式①在所有边上成立
	CornerOK bool // 不等式②在所有角上成立
	Feasible bool // EdgeOK && CornerOK

	ConflictEdges    []BlockEdge // 强度 > EdgeThreshold 的边
	ViolatingCorners []CornerPair
	ConflictBlocks   [][2]int // 冲突边端点去重后的块坐标
}

// CheckFeasibility 用闭式的两个不等式判定可行性，并列出冲突块。
//
// 定理（见 docs/block-constraints.md）：
//
//	(DC)+(SEAM) 有解  ⟺  ① 对所有边相邻块 |μ_b−μ_b'| ≤ 2ρ+τ
//	                     ② 对所有角对角块 |μ_b−μ_b''| ≤ 2ρ+2τ
//
// 冲突块的判定只用块间边缘强度 |μ_b−μ_b'| 与阈值 θ=2ρ+τ（不等式①）。
func CheckFeasibility(f DCField, p ConstraintParams) Feasibility {
	r := Feasibility{Params: p, EdgeThreshold: p.EdgeThreshold(), CornerThreshold: p.CornerThreshold()}
	r.EdgeOK, r.CornerOK = true, true
	if !p.Valid() {
		r.EdgeOK, r.CornerOK = false, false
		r.Feasible = false
		return r
	}
	seen := make(map[[2]int]bool)
	addConflictBlock := func(bx, by int) {
		k := [2]int{bx, by}
		if !seen[k] {
			seen[k] = true
			r.ConflictBlocks = append(r.ConflictBlocks, k)
		}
	}
	for by := 0; by < f.NY; by++ {
		for bx := 0; bx < f.NX; bx++ {
			if bx+1 < f.NX {
				e := BlockEdge{BX0: bx, BY0: by, BX1: bx + 1, BY1: by, Strength: cAbs(f.At(bx+1, by) - f.At(bx, by))}
				if e.Strength > r.EdgeMax {
					r.EdgeMax = e.Strength
				}
				if e.Strength > r.EdgeThreshold {
					r.EdgeOK = false
					r.ConflictEdges = append(r.ConflictEdges, e)
					addConflictBlock(bx, by)
					addConflictBlock(bx+1, by)
				}
			}
			if by+1 < f.NY {
				e := BlockEdge{BX0: bx, BY0: by, BX1: bx, BY1: by + 1, Strength: cAbs(f.At(bx, by+1) - f.At(bx, by))}
				if e.Strength > r.EdgeMax {
					r.EdgeMax = e.Strength
				}
				if e.Strength > r.EdgeThreshold {
					r.EdgeOK = false
					r.ConflictEdges = append(r.ConflictEdges, e)
					addConflictBlock(bx, by)
					addConflictBlock(bx, by+1)
				}
			}
		}
	}
	// 角对角：每个内部网格顶点有两条对角线。
	for by := 0; by+1 < f.NY; by++ {
		for bx := 0; bx+1 < f.NX; bx++ {
			pairs := [2]CornerPair{
				{BX0: bx, BY0: by, BX1: bx + 1, BY1: by + 1},
				{BX0: bx + 1, BY0: by, BX1: bx, BY1: by + 1},
			}
			for _, cp := range pairs {
				cp.Strength = cAbs(f.At(cp.BX1, cp.BY1) - f.At(cp.BX0, cp.BY0))
				if cp.Strength > r.CornerMax {
					r.CornerMax = cp.Strength
				}
				if cp.Strength > r.CornerThreshold {
					r.CornerOK = false
					r.ViolatingCorners = append(r.ViolatingCorners, cp)
				}
			}
		}
	}
	r.Feasible = r.EdgeOK && r.CornerOK
	return r
}

// constraintEdge 是差分约束 x_v − x_u ≤ c。
type constraintEdge struct {
	u, v, c int
}

// buildConstraintEdges 把 (DC)+(SEAM) 写成差分约束。
// 节点 0 是零参考，像素 (x,y) 映射到节点 y*W+x+1。
// 每个约束 x_v − x_u ≤ c 是一条有向边 u→v、权 c。
func buildConstraintEdges(f DCField, p ConstraintParams) []constraintEdge {
	es := make([]constraintEdge, 0, 4*f.W*f.H)
	pid := func(x, y int) int { return y*f.W + x + 1 }
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			m := f.DC[f.blkOf(x, y)]
			lo, hi := m-p.Rho, m+p.Rho
			if lo < 0 {
				lo = 0
			}
			if hi > 255 {
				hi = 255
			}
			q := pid(x, y)
			es = append(es, constraintEdge{0, q, hi})  // x_q ≤ hi
			es = append(es, constraintEdge{q, 0, -lo}) // x_q ≥ lo
		}
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			if x+1 < f.W && f.blkOf(x, y) != f.blkOf(x+1, y) {
				a, b := pid(x, y), pid(x+1, y)
				es = append(es, constraintEdge{a, b, p.Tau}, constraintEdge{b, a, p.Tau})
			}
			if y+1 < f.H && f.blkOf(x, y) != f.blkOf(x, y+1) {
				a, b := pid(x, y), pid(x, y+1)
				es = append(es, constraintEdge{a, b, p.Tau}, constraintEdge{b, a, p.Tau})
			}
		}
	}
	return es
}

// bellmanFord 求差分约束的一组整数解（最短路径势）。
// 可行返回 dist（长度 = 像素数+1，dist[0] 为参考）；不可行（负环）返回 nil,false。
func bellmanFord(f DCField, p ConstraintParams) ([]int, bool) {
	es := buildConstraintEdges(f, p)
	n := f.W*f.H + 1
	dist := make([]int, n) // 全部 0：等价于接一个权 0 的超级源
	for it := 0; it <= n; it++ {
		changed := false
		for _, e := range es {
			if d := dist[e.u] + e.c; d < dist[e.v] {
				dist[e.v] = d
				changed = true
			}
		}
		if !changed {
			return dist, true
		}
	}
	return nil, false
}

// ExactFeasible 用差分约束（Bellman-Ford，无负环即有解）独立判定可行性。
// 它不做任何“整块网格”假设，可用来对账闭式两不等式。
func ExactFeasible(f DCField, p ConstraintParams) bool {
	if !p.Valid() {
		return false
	}
	_, ok := bellmanFord(f, p)
	return ok
}

// ReconstructConstrained 在可行时给出一组同时满足 (DC) 与 (SEAM) 的整数重建。
//
// 先用差分约束（Bellman-Ford 无负环）判定可行；可行时以一个高清度初值
// （块内取直流）做交替投影：反复把每个像素夹回它自己的窗口 [μ−ρ, μ+ρ]
// 与相邻像素的 |·|≤τ 约束。两个约束集都是凸的（盒 / 差分），投影到交集
// 不会把已在交集内的点推出，故一旦可行即收敛到可行点；块内像素取直流
// 已经是其盒的中心，最终直流偏差很小。
//
// 不可行返回 nil,false。
func ReconstructConstrained(f DCField, p ConstraintParams) ([]uint8, bool) {
	if _, ok := bellmanFord(f, p); !ok {
		return nil, false
	}
	return projectConstraints(f, p), true
}

// projectConstraints 是 ReconstructConstrained 的迭代部分（调用方已确认可行）。
func projectConstraints(f DCField, p ConstraintParams) []uint8 {
	w, h := f.W, f.H
	// 浮点工作区，最后一次性取整，避免每步取整引入的吸附。
	x := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for i := 0; i < w; i++ {
			x[y*w+i] = float64(f.DC[f.blkOf(i, y)])
		}
	}
	// 每个像素自己的窗口。
	lo := make([]float64, w*h)
	hi := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for i := 0; i < w; i++ {
			m := float64(f.DC[f.blkOf(i, y)])
			lo[y*w+i] = m - float64(p.Rho)
			hi[y*w+i] = m + float64(p.Rho)
		}
	}
	tau := float64(p.Tau)
	for iter := 0; iter < 400; iter++ {
		maxMove := 0.0
		// SEAM 投影：把跨块相邻对的中点对齐到差 ≤ τ。
		for y := 0; y < h; y++ {
			for i := 0; i < w; i++ {
				a := y*w + i
				if i+1 < w && f.blkOf(i, y) != f.blkOf(i+1, y) {
					b := a + 1
					if d := x[a] - x[b]; d > tau || d < -tau {
						mid := (x[a] + x[b]) / 2
						na, nb := mid+tau/2, mid-tau/2
						if d < 0 {
							na, nb = mid-tau/2, mid+tau/2
						}
						if m := absF(na - x[a]); m > maxMove {
							maxMove = m
						}
						x[a], x[b] = na, nb
					}
				}
				if y+1 < h && f.blkOf(i, y) != f.blkOf(i, y+1) {
					b := a + w
					if d := x[a] - x[b]; d > tau || d < -tau {
						mid := (x[a] + x[b]) / 2
						na, nb := mid+tau/2, mid-tau/2
						if d < 0 {
							na, nb = mid-tau/2, mid+tau/2
						}
						if m := absF(na - x[a]); m > maxMove {
							maxMove = m
						}
						x[a], x[b] = na, nb
					}
				}
			}
		}
		// DC 投影：夹回各自窗口。
		for i := range x {
			if x[i] < lo[i] {
				if m := lo[i] - x[i]; m > maxMove {
					maxMove = m
				}
				x[i] = lo[i]
			} else if x[i] > hi[i] {
				if m := x[i] - hi[i]; m > maxMove {
					maxMove = m
				}
				x[i] = hi[i]
			}
		}
		if maxMove < 1e-3 {
			break
		}
	}
	out := make([]uint8, w*h)
	for i := range x {
		out[i] = uint8(clampInt(divRound(int(x[i]*1000), 1000), 0, 255))
	}
	return out
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func isBlockBoundary(f DCField, x, y int) bool {
	b := f.blkOf(x, y)
	if x > 0 && f.blkOf(x-1, y) != b {
		return true
	}
	if x+1 < f.W && f.blkOf(x+1, y) != b {
		return true
	}
	if y > 0 && f.blkOf(x, y-1) != b {
		return true
	}
	if y+1 < f.H && f.blkOf(x, y+1) != b {
		return true
	}
	return false
}

// crossNeighborDC 返回像素 (x,y) 的跨块 4-邻像素所在块的直流列表。
func crossNeighborDC(f DCField, x, y int) []int {
	b := f.blkOf(x, y)
	var out []int
	if x > 0 && f.blkOf(x-1, y) != b {
		out = append(out, f.DC[f.blkOf(x-1, y)])
	}
	if x+1 < f.W && f.blkOf(x+1, y) != b {
		out = append(out, f.DC[f.blkOf(x+1, y)])
	}
	if y > 0 && f.blkOf(x, y-1) != b {
		out = append(out, f.DC[f.blkOf(x, y-1)])
	}
	if y+1 < f.H && f.blkOf(x, y+1) != b {
		out = append(out, f.DC[f.blkOf(x, y+1)])
	}
	return out
}

// ReconstructKeepDC 是“放弃连续性、守住直流”的重建：
// 每个边界像素向它最远的跨块邻块方向最多移动 ρ，块内像素保持直流。
// 于是任意跨块像素对的灰度差至少压到 max(0, |μ_a−μ_b|−2ρ)，
// 直流偏差严格 ≤ ρ。
func ReconstructKeepDC(f DCField, p ConstraintParams) []uint8 {
	out := make([]uint8, f.W*f.H)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			m := f.DC[f.blkOf(x, y)]
			dev := 0
			for _, nb := range crossNeighborDC(f, x, y) {
				d := clampInt(nb-m, -p.Rho, p.Rho)
				if cAbs(d) > cAbs(dev) {
					dev = d
				}
			}
			out[y*f.W+x] = uint8(clampInt(m+dev, 0, 255))
		}
	}
	return out
}

// ReconstructKeepSeam 是“放弃直流、守住连续性”的重建：
// 只投影 (SEAM)，完全放开 (DC)。从块直流出发反复把跨块相邻对的中点对齐、
// 灰度差压到 τ，块内像素不参与跨块约束故保持直流。
// 直流偏差因此不再受限，它的大小就是“守住连续性”必须付出的代价。
func ReconstructKeepSeam(f DCField, p ConstraintParams) []uint8 {
	w, h := f.W, f.H
	x := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for i := 0; i < w; i++ {
			x[y*w+i] = float64(f.DC[f.blkOf(i, y)])
		}
	}
	tau := float64(p.Tau)
	projectSeam(x, f, tau)
	out := make([]uint8, w*h)
	for i := range x {
		out[i] = uint8(clampInt(divRound(int(x[i]*1000), 1000), 0, 255))
	}
	return out
}

// projectSeam 只做跨块相邻对的 |·|≤τ 投影（不动盒约束）。
func projectSeam(x []float64, f DCField, tau float64) {
	w, h := f.W, f.H
	for iter := 0; iter < 400; iter++ {
		maxMove := 0.0
		for y := 0; y < h; y++ {
			for i := 0; i < w; i++ {
				a := y*w + i
				if i+1 < w && f.blkOf(i, y) != f.blkOf(i+1, y) {
					maxMove = pullSeam(x, a, a+1, tau, maxMove)
				}
				if y+1 < h && f.blkOf(i, y) != f.blkOf(i, y+1) {
					maxMove = pullSeam(x, a, a+w, tau, maxMove)
				}
			}
		}
		if maxMove < 1e-3 {
			break
		}
	}
}

// pullSeam 把一对像素围绕其中点收拢，使差值压到 τ；返回本次最大移动量。
func pullSeam(x []float64, a, b int, tau, maxMove float64) float64 {
	d := x[a] - x[b]
	if d <= tau && d >= -tau {
		return maxMove
	}
	mid := (x[a] + x[b]) / 2
	half := tau / 2
	if d > 0 {
		x[a], x[b] = mid+half, mid-half
	} else {
		x[a], x[b] = mid-half, mid+half
	}
	if m := absF(x[a] - mid); m > maxMove {
		return m
	}
	return maxMove
}

// DevStat 是一组偏差的统计（Max 为灰度级，MeanMilli 为均值×1000）。
type DevStat struct {
	Max       int
	MeanMilli int
}

// MeasureSeamStat 统计块边界两侧相邻像素灰度差（两方向合并）。
func MeasureSeamStat(w, h, bs int, pix []uint8) DevStat {
	maxD, sum, n := 0, 0, 0
	for x := bs; x < w; x += bs {
		for y := 0; y < h; y++ {
			d := cAbs(int(pix[y*w+x]) - int(pix[y*w+x-1]))
			if d > maxD {
				maxD = d
			}
			sum += d
			n++
		}
	}
	for y := bs; y < h; y += bs {
		for x := 0; x < w; x++ {
			d := cAbs(int(pix[y*w+x]) - int(pix[(y-1)*w+x]))
			if d > maxD {
				maxD = d
			}
			sum += d
			n++
		}
	}
	return DevStat{Max: maxD, MeanMilli: meanMilli(sum, n)}
}

// MeasureDCDevStat 统计每个像素相对其块直流的偏差。
func MeasureDCDevStat(f DCField, pix []uint8) DevStat {
	maxD, sum, n := 0, 0, 0
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			d := cAbs(int(pix[y*f.W+x]) - f.DC[f.blkOf(x, y)])
			if d > maxD {
				maxD = d
			}
			sum += d
			n++
		}
	}
	return DevStat{Max: maxD, MeanMilli: meanMilli(sum, n)}
}

func meanMilli(sum, n int) int {
	if n == 0 {
		return 0
	}
	return divRound(sum*1000, n)
}

func cAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
