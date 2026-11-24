// Package quant 提供低比特灰度量化器，以及“重建是否保持单调”的逐点检验。
//
// 单调性约束（量化区间与重建电平都必须单调）：
//
//	区间边界 0=t0 < t1 < ... < t_L=256，I_j=[t_j,t_{j+1}) 严格按输入顺序排列；
//	重建电平 0 <= r0 <= r1 <= ... <= r_{L-1} <= 255（本实现的均匀表取严格递增）。
//
// 两条同时成立时，码字 c(x) 随 x 单调不减，而 r_c 随 c 单调不减，
// 于是重建 Q(x)=r_{c(x)} 也单调不减：x<=y => Q(x)<=Q(y)。
package quant

import "fmt"

// DomainMax 是 8 位灰度的最大取值，定义域为 [0,255] 共 256 个整数点。
const (
	DomainMin = 0
	DomainMax = 255
	DomainN   = 256
)

// Table 是一张显式量化表：L 个按码号排列的量化区间与重建电平。
//
// Bounds 长度 L+1，Bounds[j],Bounds[j+1) 为第 j 个区间（整数点）；
// Recon  长度 L，Recon[j] 为第 j 个码字的重建电平。
type Table struct {
	Name   string
	Bits   int
	Levels int
	Delta  int
	Bounds []int
	Recon  []int
}

// New 构造 b 位深的单调均匀量化器。
//
// L=2^b 个电平，步长 Δ=256/L（b<=8 时恒为整数）：
//
//	Bounds[j] = jΔ，j=0..L（末边界 256）
//	Recon[j]  = jΔ + Δ/2（箱内中点，b=8 时 Δ=1 退化为恒等）
//
// 区间严格有序、重建电平严格递增，因此满足全部单调性约束。
func New(bits int) (*Table, error) {
	if bits < 1 || bits > 8 {
		return nil, fmt.Errorf("位深必须在 1..8 之间，收到 %d", bits)
	}
	l := 1 << bits
	d := 256 / l
	t := &Table{
		Name:   fmt.Sprintf("monotone-uniform-%dbit", bits),
		Bits:   bits,
		Levels: l,
		Delta:  d,
		Bounds: make([]int, l+1),
		Recon:  make([]int, l),
	}
	for j := 0; j <= l; j++ {
		t.Bounds[j] = j * d
	}
	for j := 0; j < l; j++ {
		t.Recon[j] = j*d + d/2
	}
	return t, nil
}

// Code 返回灰度 x 对应的码字（区间编号）。区间等宽且有序，故码字随 x 不减。
func (t *Table) Code(x uint8) int {
	c := int(x) / t.Delta
	if c < 0 {
		return 0
	}
	if c >= t.Levels {
		return t.Levels - 1
	}
	return c
}

// Quantize 返回 x 的重建电平。
func (t *Table) Quantize(x uint8) uint8 {
	return uint8(t.Recon[t.Code(x)])
}

// Check 逐条核对两条量化单调性约束，返回第一个被破坏的约束（含量化位置与数值）。
//
//  1. 量化区间：端点正确，且 Bounds[j] < Bounds[j+1]（区间按输入顺序严格递增）；
//  2. 重建电平：取值在 [0,255]，且 Recon[j] <= Recon[j+1]（电平单调不减）。
//
// 均匀构造的正规量化器还满足 Recon[j] < Recon[j+1]。
func (t *Table) Check() error {
	l := t.Levels
	if l != 1<<t.Bits {
		return fmt.Errorf("电平数 %d 与位深 %d 不符", l, t.Bits)
	}
	if len(t.Bounds) != l+1 {
		return fmt.Errorf("区间边界长度应为 %d，实为 %d", l+1, len(t.Bounds))
	}
	if len(t.Recon) != l {
		return fmt.Errorf("重建电平长度应为 %d，实为 %d", l, len(t.Recon))
	}
	if t.Bounds[0] != DomainMin || t.Bounds[l] != DomainN {
		return fmt.Errorf("区间端点应为 [0,256]，实为 [%d,%d]", t.Bounds[0], t.Bounds[l])
	}
	for j := 0; j < l; j++ {
		if !(t.Bounds[j] < t.Bounds[j+1]) {
			return fmt.Errorf("量化区间单调性被破坏: Bounds[%d]=%d 不小于 Bounds[%d]=%d",
				j, t.Bounds[j], j+1, t.Bounds[j+1])
		}
	}
	for j := 0; j < l; j++ {
		v := t.Recon[j]
		if v < 0 || v > DomainMax {
			return fmt.Errorf("重建电平越界: Recon[%d]=%d 不在 [0,255]", j, v)
		}
		if j+1 < l && t.Recon[j] > t.Recon[j+1] {
			return fmt.Errorf("重建电平单调性被破坏: Recon[%d]=%d > Recon[%d]=%d",
				j, t.Recon[j], j+1, t.Recon[j+1])
		}
	}
	return nil
}

// BitReverse 把 x 的低 bits 位按位反转（公开以便报告/测试引用同一实现）。
func BitReverse(x, bits int) int {
	y := 0
	for i := 0; i < bits; i++ {
		y = (y << 1) | (x & 1)
		x >>= 1
	}
	return y
}

// NewLegacyBitReversed 复刻“修之前”的量化器：区间表仍是单调均匀的，
// 但重建电平表被按码号做了 b 位位反转重排（典型的收发位序/表索引事故）。
//
// 于是 Recon[c]=固定均匀表的 Recon[bitreverse_b(c)]，不再随 c 单调，
// 重建在相邻区间交界处发生向下跳变——暗输入可能被重建得比亮输入更亮。
// 该构造仅用于留档与回归对照，新代码必须使用 New。
func NewLegacyBitReversed(bits int) (*Table, error) {
	t, err := New(bits)
	if err != nil {
		return nil, err
	}
	fixed := append([]int(nil), t.Recon...)
	for c := 0; c < t.Levels; c++ {
		t.Recon[c] = fixed[BitReverse(c, bits)]
	}
	t.Name = fmt.Sprintf("LEGACY-bitreverse-%dbit", bits)
	return t, nil
}

// Ramp256 返回一条从黑(0)到白(255)的完整渐变：整个 uint8 定义域，
// 共 256 点。对它逐点比较就是全量检查，不存在抽样。
func Ramp256() []uint8 {
	y := make([]uint8, DomainN)
	for i := range y {
		y[i] = uint8(i)
	}
	return y
}

// Reconstruct 把整条渐变送进量化器，返回重建强度序列。
func Reconstruct(t *Table) []uint8 {
	in := Ramp256()
	out := make([]uint8, len(in))
	for i, x := range in {
		out[i] = t.Quantize(x)
	}
	return out
}

// Deltas 按题目指定的定义给出全部相邻两点之差：
//
//	Δ_i = y[i+1] - y[i], i=0..n-2
//
// 向右（变亮方向）为正，向下反转（右侧反而更暗）为负。长度 n-1。
func Deltas(y []uint8) []int {
	d := make([]int, 0, len(y)-1)
	for i := 0; i+1 < len(y); i++ {
		d = append(d, int(y[i+1])-int(y[i]))
	}
	return d
}

// BreakIndices 返回所有“破坏点”的位置 i：满足 y[i+1] < y[i]（Δ_i<0）
// 的相邻对左端点。逐点比较，一个不漏。
func BreakIndices(y []uint8) []int {
	var idx []int
	for i := 0; i+1 < len(y); i++ {
		if y[i+1] < y[i] {
			idx = append(idx, i)
		}
	}
	return idx
}

// WorstDelta 返回最大反转量的带符号取值：所有相邻差中的最小值 min_i Δ_i。
// 最严重的一次向下反转对应最负的 Δ；返回 0 表示没有任何向下反转
// （粗量化渐变存在同电平平坦段时最小值恰为 0；8 位恒等量化时为 +1）。
func WorstDelta(y []uint8) int {
	w := 0
	for i := 0; i+1 < len(y); i++ {
		d := int(y[i+1]) - int(y[i])
		if i == 0 || d < w {
			w = d
		}
	}
	return w
}

// WorstDropMagnitude 返回最严重向下反转的非负幅度 max_i (y[i]-y[i+1])，
// 即 -min_i Δ_i 的正部；单调不减时为 0。
func WorstDropMagnitude(y []uint8) int {
	m := 0
	for i := 0; i+1 < len(y); i++ {
		if drop := int(y[i]) - int(y[i+1]); drop > m {
			m = drop
		}
	}
	return m
}

// Measure 汇总一条渐变在某量化器下的单调性实测结果。
type Measure struct {
	Bits        int
	Levels      int
	Points      int // 参与检查的点数
	Pairs       int // 相邻对数 = Points-1
	BreakCount  int // 破坏点数（Δ_i<0 的相邻对数）
	WorstDelta  int // 最大反转量（带符号，min Δ_i，向下为负）
	WorstDrop   int // 最大向下幅度（非负）
	FirstBreaks []int
}

// MeasureRamp 对完整黑到白渐变做逐点、非抽样的单调性实测。
func MeasureRamp(t *Table) Measure {
	y := Reconstruct(t)
	breaks := BreakIndices(y)
	return Measure{
		Bits:        t.Bits,
		Levels:      t.Levels,
		Points:      len(y),
		Pairs:       len(y) - 1,
		BreakCount:  len(breaks),
		WorstDelta:  WorstDelta(y),
		WorstDrop:   WorstDropMagnitude(y),
		FirstBreaks: append([]int(nil), breaks...),
	}
}
