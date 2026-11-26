package codec

import (
	"encoding/binary"
	"errors"
	"math"
)

// 本文件实现按行的固定系数线性预测（FLAC 风格 0..4 阶），
// 残差按 mod 256 折回 8 bit，保证同一位深。
//
// 理想情形（已知真实联合分布、用条件期望作预测、熵编码达到条件熵）：
// 加阶不增熵，因为 H(X|Y,Z) = H(X|Y) - I(X;Z|Y) <= H(X|Y)。
//
// 实际情形（固定系数、残差量化回 8 bit、按直方图估计熵）：
// 高斯近似下 H ≈ 1/2·log2(2πe·σ²)，熵增当且仅当残差方差变大。
// 增大的条件：σ²(e_{k+1}) / σ²(e_k) > 1。
// ChooseOrder 即按此条件加阶：仅当方差比 <= 1 时才接受更高一阶。

// MaxOrder 是支持的最大预测阶数。
const MaxOrder = 4

// predict 用已重建的像素计算第 i 个像素的预测值，调用方保证 i 左侧同行有 order 个像素。
func predict(pix []uint8, order, i int) int {
	x := func(k int) int { return int(pix[i-k]) }
	switch order {
	case 1:
		return x(1)
	case 2:
		return 2*x(1) - x(2)
	case 3:
		return 3*x(1) - 3*x(2) + x(3)
	case 4:
		return 4*x(1) - 6*x(2) + 4*x(3) - x(4)
	}
	return 0
}

// Residuals 计算 order 阶预测残差（mod 256）。每行前 order 个像素原样保留作热身。
func Residuals(w, h int, pix []uint8, order int) []uint8 {
	res := make([]uint8, len(pix))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if x < order {
				res[i] = pix[i]
				continue
			}
			res[i] = pix[i] - uint8(predict(pix, order, i))
		}
	}
	return res
}

// reconstruct 是 Residuals 的逆，原地重建。
func reconstruct(w, h int, res []uint8, order int) []uint8 {
	pix := make([]uint8, len(res))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if x < order {
				pix[i] = res[i]
				continue
			}
			pix[i] = res[i] + uint8(predict(pix, order, i))
		}
	}
	return pix
}

// EntropyBits 是符号流的零阶直方图熵，单位 bit/符号。
func EntropyBits(sym []uint8) float64 {
	if len(sym) == 0 {
		return 0
	}
	var cnt [256]int
	for _, s := range sym {
		cnt[s]++
	}
	n := float64(len(sym))
	entropy := 0.0
	for _, c := range cnt {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// residualVariance 计算未折回的整数残差 e = x - p 的方差。
// 注意不能用 mod 256 后的残差估方差：接近均匀分布时折回会把
// σ²(e_1)=2σ²(e_0) 掩盖成比值 1，让开关在纯噪声上误判。
func residualVariance(w, h int, pix []uint8, order int) float64 {
	var sum, sq float64
	n := 0
	for y := 0; y < h; y++ {
		for x := order; x < w; x++ {
			i := y*w + x
			e := float64(int(pix[i]) - predict(pix, order, i))
			sum += e
			sq += e * e
			n++
		}
	}
	if n == 0 {
		return 0
	}
	mean := sum / float64(n)
	return sq/float64(n) - mean*mean
}

// OrderStat 记录某一阶的测量结果与开关判定。
type OrderStat struct {
	Order    int
	Variance float64 // 未折回整数残差的方差
	Ratio    float64 // 与上一阶残差方差之比，order 0 恒为 1
	Entropy  float64 // 残差熵 bit/像素（mod 256，同一位深）
	Accepted bool    // 开关是否接受这一阶（方差比 <= 1 才接受）
}

// ChooseOrder 从 0 阶起逐阶测量，仅当 σ²(e_{k+1})/σ²(e_k) <= 1 时加阶，
// 首次不满足即停。返回各阶统计与最终选定的阶数。
func ChooseOrder(w, h int, pix []uint8, maxOrder int) ([]OrderStat, int) {
	if maxOrder < 0 {
		maxOrder = 0
	}
	if maxOrder > MaxOrder {
		maxOrder = MaxOrder
	}
	stats := make([]OrderStat, 0, maxOrder+1)
	chosen, prevVar, blocked := 0, 0.0, false
	for k := 0; k <= maxOrder; k++ {
		res := Residuals(w, h, pix, k)
		v := residualVariance(w, h, pix, k)
		st := OrderStat{Order: k, Variance: v, Ratio: 1, Entropy: EntropyBits(res)}
		if k > 0 && prevVar > 0 {
			st.Ratio = v / prevVar
		}
		st.Accepted = !blocked && (k == 0 || st.Ratio <= 1)
		if st.Accepted {
			chosen = k
		} else {
			blocked = true
		}
		stats = append(stats, st)
		prevVar = v
	}
	return stats, chosen
}

// EncodePRED 输出 GTP1 + 宽高 + 阶数 + 残差字节流。
func EncodePRED(w, h int, pix []uint8, order int) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if order < 0 || order > MaxOrder {
		return nil, errors.New("预测阶数越界")
	}
	res := Residuals(w, h, pix, order)
	out := make([]byte, 9, 9+len(res))
	copy(out[0:], "GTP1")
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	out[8] = uint8(order)
	return append(out, res...), nil
}

// DecodePRED 解析 GTP1 流并重建像素。
func DecodePRED(data []byte) (int, int, []uint8, error) {
	if len(data) < 9 || string(data[0:4]) != "GTP1" {
		return 0, 0, nil, errors.New("不是 GTP1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	order := int(data[8])
	if w <= 0 || h <= 0 || order > MaxOrder {
		return 0, 0, nil, errors.New("GTP1 头非法")
	}
	if len(data)-9 < w*h {
		return 0, 0, nil, errors.New("残差数据不足")
	}
	pix := reconstruct(w, h, data[9:9+w*h], order)
	return w, h, pix, nil
}
