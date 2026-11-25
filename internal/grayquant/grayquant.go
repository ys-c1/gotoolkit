// Package grayquant 实现“灰度 -> M 个等宽区间 -> 伪逆反解”的可审计模型，
// 用来分析取整平台、映射可逆性与往返像素差异。
//
// 位深 b 对应输入灰度数 N=1<<b，灰阶 x∈[0,N-1]；区间数 M。
//
//	T(x) = floor(M*x/N)           // 灰阶 x 落入的区间号（取整/分箱）
//	R(y) = ceil(y*N/M)            // 区间 y 的第一个灰阶，伪逆代表值
//
// 平台段的唯一判定口径是相邻映射值之差为零：
//
//	T(i)-T(i-1) == 0
//
// 在映射非递减的前提下，不存在任何零差相邻边等价于严格递增、单射、可逆。
// 结论（证明见 docs/plateau-reversibility.md）：
//
//	某处存在平台  <=> M < N；
//	可逆          <=> 无零差边 <=> 最长等值段长度为 1 <=> M >= N；
//	可逆所需最小区间数 Mmin = N = 1<<b。
package grayquant

// SharedIntervals 是三档实验共用的区间数。
// gotoolkit 的像素类型是 uint8、PGM/BMP/RLE 交付码级为 0..255，
// 因此自然取 M=256；它恰好只让 8bit（N=256）一档可逆。
const SharedIntervals = 256

// BitsTiers 是题目要求的三档位深。
var BitsTiers = []int{8, 10, 12}

// Levels 返回位深对应的输入灰度数 N=2^b。
func Levels(bits int) int { return 1 << bits }

// MapValue 返回 T(x)=floor(M*x/N)，并把末点钳在 M-1。
// x 必须在 [0,N-1]；非负整数除法本身就是 floor。
func MapValue(x, n, m int) int {
	y := m * x / n
	if y > m-1 {
		y = m - 1
	}
	return y
}

// LUT 返回 N 个输入灰阶的完整映射表。
func LUT(n, m int) []int {
	lut := make([]int, n)
	for x := 0; x < n; x++ {
		lut[x] = MapValue(x, n, m)
	}
	return lut
}

// AdjacentDiff 返回 d[i]=LUT[i+1]-LUT[i]，长度 len(LUT)-1。
func AdjacentDiff(lut []int) []int {
	d := make([]int, 0, maxInt(0, len(lut)-1))
	for i := 1; i < len(lut); i++ {
		d = append(d, lut[i]-lut[i-1])
	}
	return d
}

// ZeroDiffSteps 返回满足 LUT[i]==LUT[i-1] 的右下标 i（i>=1）。
// 这是题目指定的平台段判定口径：相邻映射值之差为零。
func ZeroDiffSteps(lut []int) []int {
	var idx []int
	for i := 1; i < len(lut); i++ {
		if lut[i]-lut[i-1] == 0 {
			idx = append(idx, i)
		}
	}
	return idx
}

// Run 是 LUT 中的一个极大等值段（含单点平凡段）。
type Run struct {
	Start, End int // 闭区间下标
	Value      int
	Length     int // End-Start+1，段内输入灰阶数
	ZeroEdges  int // Length-1，段内零差相邻边数
}

// EqualRuns 合并全部极大等值段。
func EqualRuns(lut []int) []Run {
	var runs []Run
	start := 0
	for i := 1; i <= len(lut); i++ {
		if i == len(lut) || lut[i] != lut[start] {
			end := i - 1
			runs = append(runs, Run{
				Start: start, End: end, Value: lut[start],
				Length: end - start + 1, ZeroEdges: end - start,
			})
			start = i
		}
	}
	return runs
}

// PlateauRuns 只返回长度 >=2 的平台段（真正发生多阶共映的段）。
func PlateauRuns(lut []int) []Run {
	var out []Run
	for _, r := range EqualRuns(lut) {
		if r.Length >= 2 {
			out = append(out, r)
		}
	}
	return out
}

// MaxRunLength 返回最长等值段含多少个输入灰阶；恒等映射时为 1。
func MaxRunLength(lut []int) int {
	mx := 1
	for _, r := range EqualRuns(lut) {
		if r.Length > mx {
			mx = r.Length
		}
	}
	return mx
}

// IsInjective 判断映射是否单射。对这里的非递减 LUT，
// 等价于每个相邻差都不为零（严格递增）。
func IsInjective(lut []int) bool {
	for i := 1; i < len(lut); i++ {
		if lut[i] == lut[i-1] { // 与 ZeroDiffSteps 同一判据
			return false
		}
	}
	return true
}

// MinIntervals 给出 N 个输入灰阶可逆所需的最小区间数：恰为 N。
func MinIntervals(n int) int { return n }

// Repr 返回区间 y 的伪逆代表灰阶 R(y)=ceil(y*N/M)（区间内第一个灰阶）。
func Repr(y, n, m int) int { return (y*n + m - 1) / m }

// RoundtripCount 对一串原生灰阶统计 T 后再 R 的不同像素数。
func RoundtripCount(xs []int, n, m int) int {
	diff := 0
	for _, x := range xs {
		y := MapValue(x, n, m)
		if Repr(y, n, m) != x {
			diff++
		}
	}
	return diff
}

// FullRangeRamp 构造 W*H 像素的全覆盖梯度：
// pixel[p] = floor(p*N/(W*H))，每个灰阶出现 (W*H)/N 次（需整除或近似）。
func FullRangeRamp(w, h, n int) []int {
	pix := w * h
	xs := make([]int, pix)
	for p := 0; p < pix; p++ {
		xs[p] = p * n / pix
	}
	return xs
}

// ModularZeroCondition 给出 M<N 时第 i 条相邻边（i>=1）零差的算术充要条件：
//
//	T(i)==T(i-1)  <=>  (M*(i-1)) mod N < N-M。
//
// 返回该谓词，供与直接差值判定交叉核验。
func ModularZeroCondition(i, n, m int) bool {
	if m >= n {
		return false
	}
	return (m*(i-1))%n < n-m
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
