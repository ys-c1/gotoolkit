// Package scale 实现可切换色彩空间假设的盒式缩放与伽马偏差补偿。
// 本包是内部实现，外部调用方请使用 gotoolkit/api。
package scale

import (
	"errors"
	"fmt"
	"math"
)

// Mode 选择缩放路径。
type Mode string

const (
	// ModeEncoded 是原路径：直接在伽马编码值上做盒式平均。
	ModeEncoded Mode = "encoded"
	// ModeGammaDual 是与“暗部偏亮”符号约定对应的参照空间：
	// 先做 x^(1/gamma)，缩放后再做 ^gamma。
	ModeGammaDual Mode = "gamma-dual"
	// ModeCompensated 在原编码值路径的整数结果上反解局部覆盖系数并补偿。
	ModeCompensated Mode = "compensated"
	// ModeLinear 是标准线性光参照：x^gamma -> 平均 -> ^(1/gamma)。
	ModeLinear Mode = "linear"
)

// Image 是缩放包内部使用的灰度图。
type Image struct {
	W, H int
	Pix  []byte
}

// NormalizeGamma 把零值替换成默认伽马 2.2。
func NormalizeGamma(gamma float64) float64 {
	if gamma == 0 {
		return 2.2
	}
	return gamma
}

// ValidateGamma 拒绝非有限值和实验范围外的伽马。
func ValidateGamma(gamma float64) error {
	if math.IsNaN(gamma) || math.IsInf(gamma, 0) || gamma < 0.1 || gamma > 10 {
		return fmt.Errorf("gamma 越界: %v", gamma)
	}
	return nil
}

// EdgeCompensation 返回标准暗边模型在“偏亮”约定下的乘性补偿量 K。
//
//	编码空间结果 y_E = rho*C_d
//	对偶空间结果 y_D = rho^gamma*C_d
//	K = y_D/y_E = rho^(gamma-1)
func EdgeCompensation(rho, gamma float64) float64 {
	gamma = NormalizeGamma(gamma)
	return math.Pow(rho, gamma-1)
}

// EdgeBias 返回 y_E-y_D；正值表示直接在编码值上缩放的结果偏亮。
func EdgeBias(cDark, rho, gamma float64) float64 {
	gamma = NormalizeGamma(gamma)
	return cDark * (rho - math.Pow(rho, gamma))
}

// LinearEdgeBias 是标准线性光工作流的有符号偏差 y_E-y_L。
// gamma 是解码指数（典型值 2.2）。0<rho<1 时结果为负：
// 直接平均编码值通常不是偏亮，而是比线性光结果更暗。
func LinearEdgeBias(cDark, rho, gamma float64) float64 {
	gamma = NormalizeGamma(gamma)
	return cDark * (rho - math.Pow(rho, 1/gamma))
}

// LinearCompensation 反解标准线性光约定下的乘性补偿量。
func LinearCompensation(rho, gamma float64) float64 {
	gamma = NormalizeGamma(gamma)
	return math.Pow(rho, 1/gamma-1)
}

// CanonicalEdgeValues 按 rho=局部暗边覆盖率的标准模型计算三路径码值。
func CanonicalEdgeValues(cDark byte, rho, gamma float64) (encoded, dual, compensated byte) {
	gamma = NormalizeGamma(gamma)
	c := float64(cDark)
	encoded = round8(rho * c)
	dual = round8(math.Pow(rho, gamma) * c)
	k := EdgeCompensation(rho, gamma)
	compensated = round8(k * float64(encoded))
	return encoded, dual, compensated
}

// Box 按 denom×denom 整数盒缩小。调用方需保证宽高均可被 denom 整除。
func Box(src Image, denom int, gamma float64, mode Mode) (*Image, error) {
	gamma = NormalizeGamma(gamma)
	if src.W <= 0 || src.H <= 0 || len(src.Pix) != src.W*src.H {
		return nil, errors.New("画幅与像素长度不一致")
	}
	if denom <= 0 {
		return nil, fmt.Errorf("缩放分母必须为正: %d", denom)
	}
	if src.W%denom != 0 || src.H%denom != 0 {
		return nil, fmt.Errorf("画幅 %dx%d 不能被分母 %d 整除", src.W, src.H, denom)
	}
	if err := ValidateGamma(gamma); err != nil {
		return nil, err
	}
	switch mode {
	case "", ModeEncoded, ModeGammaDual, ModeCompensated, ModeLinear:
	default:
		return nil, fmt.Errorf("未知缩放空间: %q", mode)
	}
	if mode == "" {
		mode = ModeEncoded
	}

	out := &Image{W: src.W / denom, H: src.H / denom, Pix: make([]byte, (src.W/denom)*(src.H/denom))}
	n := float64(denom * denom)
	inv255 := 1.0 / 255.0

	for by := 0; by < out.H; by++ {
		for bx := 0; bx < out.W; bx++ {
			var sum, dualSum, linearSum float64
			var maxV byte
			for y := by * denom; y < (by+1)*denom; y++ {
				row := y * src.W
				for x := bx * denom; x < (bx+1)*denom; x++ {
					v := src.Pix[row+x]
					sum += float64(v)
					if v > maxV {
						maxV = v
					}
					nx := float64(v) * inv255
					dualSum += math.Pow(nx, 1/gamma)
					linearSum += math.Pow(nx, gamma)
				}
			}

			mean := sum / n
			qEncoded := round8(mean)
			oi := by*out.W + bx
			switch mode {
			case ModeEncoded:
				out.Pix[oi] = qEncoded
			case ModeGammaDual:
				shaped := dualSum / n
				out.Pix[oi] = round8(255 * math.Pow(shaped, gamma))
			case ModeLinear:
				shaped := linearSum / n
				out.Pix[oi] = round8(255 * math.Pow(shaped, 1/gamma))
			case ModeCompensated:
				// 对“0 背景 + 单一暗电平 C”的盒内边缘，mean=theta*C。
				// 用整数原路径结果估计 theta，再乘 theta^(gamma-1)。
				if maxV == 0 || qEncoded == 0 {
					out.Pix[oi] = 0
					continue
				}
				theta := float64(qEncoded) / float64(maxV)
				if theta > 1 {
					theta = 1
				}
				k := math.Pow(theta, gamma-1)
				out.Pix[oi] = round8(k * float64(qEncoded))
			}
		}
	}
	return out, nil
}

func round8(v float64) byte {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return byte(math.Round(v))
}
