// Package lowbit 实现低位深量化、误差扩散抖动与调色板索引三处联动。
//
// 三处共享同一个参数字段 Bits（目标位深 d）：
//   - 量化：步长 q 由目标误差 E 反解，见 Step 的推导与 Inequality；
//   - 抖动：量化误差按归一化核扩散，或以有序阈值摊开；
//   - 调色板：条目数为 2^d，索引宽度恰为 d 位。
//
// 任一特性开启都走同一个 Params 与同一条量化口径，因此抖动会改变调色板所见的
// 图像，调色板索引也会随量化步长变化。
package lowbit

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// 位深与调色板相关错误。api 层会把它们翻译成稳定错误码。
var (
	ErrBadBits    = errors.New("位深越界")
	ErrBadTarget  = errors.New("目标误差越界")
	ErrBadPalette = errors.New("调色板条目数越界")
)

// MaxBits 是允许的最大位深。
const MaxBits = 8

// Params 是一次低比特处理的全部派生参数，三处联动共享它。
type Params struct {
	Bits        int // 目标位深 d，1..8
	TargetErr   int // 目标最大逐像素误差 E（0 表示按位深推导）
	Q           int // 反解出的量化步长
	EffErr      int // 实际可达的最大误差 ceil(Q/2)
	LevelCount  int // 码值个数 2^d
	MaxCode     int // 最高码值 2^d-1
	PaletteSize int // 调色板条目数，默认 2^d
}

// MinStep 是能覆盖 [0,255] 的最小整数步长：
//
//	(2^d-1)·q ≥ 255  ⇔  q ≥ 255/(2^d-1)
//	q_min = ⌈255/(2^d-1)⌉
func MinStep(bits int) int {
	if bits < 1 || bits > MaxBits {
		return 0
	}
	return ceilDiv(255, (1<<uint(bits))-1)
}

// DefaultTargetErr 是位深 d 默认的目标误差：最小覆盖步长的一半（向上取整）。
// 即 E_default = ⌈q_min/2⌉。
func DefaultTargetErr(bits int) int {
	q := MinStep(bits)
	if q == 0 {
		return 0
	}
	return (q + 1) / 2
}

// Step 由目标误差反解量化步长。
//
// 推导（最近电平重建）：码 k 的重建电平为 min(k·q,255)，把像素 v 映射到最近码值，
// 则重建误差满足 |v-Q(v)| ≤ q/2。若要求 |v-Q(v)| ≤ E，则
//
//	q ≤ 2E                                    (误差约束)
//	(2^d-1)·q ≥ 255  ⇒  q ≥ 255/(2^d-1)      (满量程覆盖约束)
//
// 两式合并即反解不等式
//
//	⌈255/(2^d-1)⌉ ≤ q ≤ 2E,
//
// 可行当且仅当 E ≥ 255/(2(2^d-1))。取 q = max(⌈255/(2^d-1)⌉, 2E) 并夹到 ≤255，
// 于是实际误差上界为 ⌈q/2⌉。E=0 时取默认值 ⌈q_min/2⌉，使 q=q_min。
func Step(targetErr, bits int) int {
	qMin := MinStep(bits)
	if qMin == 0 {
		return 0
	}
	if targetErr <= 0 {
		targetErr = (qMin + 1) / 2
	}
	q := 2 * targetErr
	if q < qMin {
		q = qMin
	}
	if q > 255 {
		q = 255
	}
	return q
}

// Inequality 返回反解不等式的可读形式，供报告直接引用。
func Inequality(targetErr, bits int) string {
	if bits < 1 || bits > MaxBits {
		return "位深越界"
	}
	if targetErr <= 0 {
		targetErr = DefaultTargetErr(bits)
	}
	qMin := MinStep(bits)
	s := fmt.Sprintf("ceil(255/(2^%d-1)) = %d <= q <= 2E = %d", bits, qMin, 2*targetErr)
	if 2*targetErr < qMin {
		s = fmt.Sprintf("ceil(255/(2^%d-1)) = %d > 2E = %d，误差约束不可行，q 被抬到覆盖下界", bits, qMin, 2*targetErr)
	}
	return s
}

// NewParams 校验并派生参数。
func NewParams(bits, targetErr, paletteSize int) (Params, error) {
	if bits < 1 || bits > MaxBits {
		return Params{}, fmt.Errorf("%w: %d 不在 [1,%d]", ErrBadBits, bits, MaxBits)
	}
	if targetErr < 0 || targetErr > 255 {
		return Params{}, fmt.Errorf("%w: %d 不在 [0,255]", ErrBadTarget, targetErr)
	}
	q := Step(targetErr, bits)
	p := Params{
		Bits:        bits,
		TargetErr:   targetErr,
		Q:           q,
		EffErr:      (q + 1) / 2,
		LevelCount:  1 << uint(bits),
		MaxCode:     (1 << uint(bits)) - 1,
		PaletteSize: paletteSize,
	}
	if p.TargetErr == 0 {
		p.TargetErr = DefaultTargetErr(bits)
	}
	if p.PaletteSize == 0 {
		p.PaletteSize = 1 << uint(bits)
	}
	if p.PaletteSize < 1 || p.PaletteSize > 256 {
		return Params{}, fmt.Errorf("%w: %d 不在 [1,256]", ErrBadPalette, p.PaletteSize)
	}
	return p, nil
}

// Code 把像素值映射到最近码值，夹到 [0,2^d-1]。
// 允许输入越界（抖动后的值），越界会被夹住。
func (p Params) Code(v int) int {
	if v <= 0 {
		return 0
	}
	c := (v + p.Q/2) / p.Q
	if c > p.MaxCode {
		c = p.MaxCode
	}
	return c
}

// ReconstructCode 把码值还原成重建电平，三处共用这一口径。
func (p Params) ReconstructCode(code int) int {
	if code <= 0 {
		return 0
	}
	if code > p.MaxCode {
		code = p.MaxCode
	}
	v := code * p.Q
	if v > 255 {
		v = 255
	}
	return v
}

// Reconstruct 是像素值 → 码值 → 重建电平的完整口径。
func (p Params) Reconstruct(v int) int { return p.ReconstructCode(p.Code(v)) }

// Quantize 是 Reconstruct 的别名，保留“量化”调用点的语义。
func (p Params) Quantize(v int) int { return p.Reconstruct(v) }

// Bounds 返回码值 k 吸收的输入区间 [lo,hi]。
func (p Params) Bounds(code int) (int, int) {
	lo := code*p.Q - p.Q/2
	hi := lo + p.Q - 1
	if code <= 0 {
		lo = 0
	}
	if code >= p.MaxCode {
		hi = 255
	}
	return lo, hi
}

// FormatCode 把码值写成 d 位二进制串。
func (p Params) FormatCode(code int) string {
	b := make([]byte, p.Bits)
	for i := p.Bits - 1; i >= 0; i-- {
		if code&1 != 0 {
			b[i] = '1'
		} else {
			b[i] = '0'
		}
		code >>= 1
	}
	return string(b)
}

// GreedyPalette 返回至多 n 个调色板条目（升序），保证 0 在表内、存在 255 时也入表。
// 贪心：每步选能覆盖最多“尚未入表像素”的条目；并列取更小值，保证确定性。
func GreedyPalette(pix []uint8, n int) []int {
	var hist [256]int
	var used [256]bool
	for _, v := range pix {
		hist[v]++
		used[v] = true
	}
	var vals []int
	for v, ok := range used {
		if ok {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return []int{0}
	}
	if n > len(vals) {
		n = len(vals)
	}
	chosen := map[int]bool{}
	var out []int
	take := func(v int) {
		if !chosen[v] {
			chosen[v] = true
			out = append(out, v)
		}
	}
	take(0)
	if n > 1 && used[255] {
		take(255)
	}
	for len(out) < n {
		bestV, bestCover := -1, -1
		for _, c := range vals {
			if chosen[c] {
				continue
			}
			cover := 0
			for _, p := range vals {
				if chosen[p] {
					continue
				}
				if nearestIn(vals, p, chosen, c) {
					cover += hist[p]
				}
			}
			if cover > bestCover {
				bestV, bestCover = c, cover
			}
		}
		if bestV < 0 {
			break
		}
		take(bestV)
	}
	sort.Ints(out)
	return out
}

func nearestIn(vals []int, p int, chosen map[int]bool, c int) bool {
	best, bestD := -1, 1<<30
	consider := func(v int) {
		d := abs(v - p)
		if d < bestD || (d == bestD && v < best) {
			best, bestD = v, d
		}
	}
	for _, v := range vals {
		if chosen[v] {
			consider(v)
		}
	}
	consider(c)
	return best == c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// NearestIndex 返回 v 在有序调色板中的下标（并列取更小值）。
func NearestIndex(palette []int, v int) int {
	best, bestD := 0, 1<<30
	for i, p := range palette {
		d := abs(p - v)
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// BuiltinKernelNames 返回内置核名称，已排序。
func BuiltinKernelNames() []string {
	names := make([]string, 0, len(Builtins()))
	for n := range Builtins() {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ResolveKernel 解析抖动核。s 为空返回 nil（不抖动）；
// s 为内置名返回内置核；s 以 "custom:" 开头时解析其后的权重矩阵。
func ResolveKernel(s string) (*Kernel, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if k, ok := Builtin(s); ok {
		if err := k.Validate(); err != nil {
			return nil, err
		}
		return k, nil
	}
	if rest, ok := strings.CutPrefix(s, "custom:"); ok {
		return ParseKernel(rest)
	}
	return nil, fmt.Errorf("%w: 未知核 %q", ErrBadKernel, s)
}
