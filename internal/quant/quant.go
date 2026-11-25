// Package quant 实现位深（量化步长）量化与量化误差的熵测量。
//
// 量化模型（截断量化器，保留高位、丢弃低 d 位）：
//
//	Δ = 2^d，d = b-1 为被丢弃的低位数
//	q   = floor(x/Δ) = x >> d
//	x̂   = clamp(q·Δ, 0, 255)
//	e   = x - x̂ = x mod Δ   // 量化误差，取值 {0,1,…,Δ-1}，共 Δ 个
//
// 误差 e 就是被丢弃的低 d 位。当输入在一个完整周期 Δ 上等概率出现时，
// 误差均匀分布，H(e) = log2(Δ) = d = b-1 比特/像素。
// 档位 b 表示误差的有符号位宽（d 个数据位 + 1 个符号位），故 d = b-1。
package quant

import "math"

// MinDepth / MaxDepth 是对外档位 b 的合法区间。
const (
	MinDepth = 2
	MaxDepth = 8
)

// ErrBadDepth 在档位越界时返回。
type ErrBadDepth struct{ Depth int }

func (e ErrBadDepth) Error() string {
	return "位深档位越界"
}

// dropBits 返回档位 b 对应的丢弃位数 d=b-1 与步长 Δ=2^d；越界返回错误。
func dropBits(b int) (d int, delta int, err error) {
	if b < MinDepth || b > MaxDepth {
		return 0, 0, ErrBadDepth{Depth: b}
	}
	d = b - 1
	return d, 1 << d, nil
}

// Step 返回档位 b 的量化步长 Δ=2^(b-1)。
func Step(b int) (int, error) {
	_, delta, err := dropBits(b)
	return delta, err
}

// Quantize 把 8 位灰度像素按档位 b 量化为码字 q=floor(x/Δ)。
func Quantize(pix []uint8, b int) ([]int, error) {
	_, delta, err := dropBits(b)
	if err != nil {
		return nil, err
	}
	q := make([]int, len(pix))
	for i, x := range pix {
		q[i] = int(x) / delta
	}
	return q, nil
}

// Reconstruct 把码字重构回 8 位灰度，边界裁剪到 [0,255]。
func Reconstruct(q []int, b int) ([]uint8, error) {
	_, delta, err := dropBits(b)
	if err != nil {
		return nil, err
	}
	out := make([]uint8, len(q))
	for i, v := range q {
		x := v * delta
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		out[i] = uint8(x)
	}
	return out, nil
}

// Reduce 一步完成“量化 + 重构”，返回降位深后的像素。
func Reduce(pix []uint8, b int) ([]uint8, error) {
	q, err := Quantize(pix, b)
	if err != nil {
		return nil, err
	}
	return Reconstruct(q, b)
}

// Error 返回逐像素量化误差 e = x - x̂ = x mod Δ（未触边界时）。
func Error(pix []uint8, b int) ([]int, error) {
	rec, err := Reduce(pix, b)
	if err != nil {
		return nil, err
	}
	e := make([]int, len(pix))
	for i := range pix {
		e[i] = int(pix[i]) - int(rec[i])
	}
	return e, nil
}

// Histogram 统计离散符号的经验频次，键为符号取值。
func Histogram(syms []int) map[int]int {
	h := make(map[int]int)
	for _, s := range syms {
		h[s]++
	}
	return h
}

// Entropy 按经验分布计算香农熵，单位比特/符号（此处即比特/像素）。
// 零频次符号不计入；空输入返回 0。
func Entropy(syms []int) float64 {
	if len(syms) == 0 {
		return 0
	}
	h := Histogram(syms)
	n := float64(len(syms))
	var hh float64
	for _, c := range h {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		hh -= p * math.Log2(p)
	}
	return hh
}

// TheoreticalEntropy 返回均匀误差条件下的理论熵 H*=b-1（比特/像素）。
func TheoreticalEntropy(b int) (float64, error) {
	d, _, err := dropBits(b)
	if err != nil {
		return 0, err
	}
	return float64(d), nil
}

// IsUniform 判断误差直方图是否均匀：各非零桶频次相对平均频次的最大
// 相对偏差不超过 tol（实验取 1e-9，只容忍取整噪声）。
func IsUniform(syms []int, tol float64) bool {
	h := Histogram(syms)
	if len(h) == 0 {
		return false
	}
	var sum int
	for _, c := range h {
		sum += c
	}
	mean := float64(sum) / float64(len(h))
	for _, c := range h {
		if math.Abs(float64(c)-mean)/mean > tol {
			return false
		}
	}
	return true
}
