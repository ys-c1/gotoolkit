// Package dither 实现带误差反馈回路的光栅误差扩散。
//
// 模型（光栅扫描，量化误差 e 经因果反馈滤波器 H 回到输入端）：
//
//	u[n] = x[n] + Σ_k h_k e[n-d_k]
//	y[n] = Q(u[n]),  e[n] = y[n] - u[n]
//
// 加性量化噪声模型下输出噪声 y-x 的传递函数为
//
//	NTF(z) = 1 + H(z),  H(z) = Σ_k h_k z_x^(-dx) z_y^(-dy)
//
// 现成 Floyd–Steinberg 核 Σh=1，故 |NTF(0,0)|=2，DC 噪声被放大 6 dB，
// 表现为低频噪声偏多。本包的反馈核由 analysis/design.py 按白噪声目标谱
// （低频段 |NTF|≈0.5、全谱能量中性 mean|NTF|²≈1）加权复最小二乘解出，
// 全部抽头为负反馈，使低频增益降到 0.58–0.79，总误差不升。
package dither

import (
	"errors"
	"math"
)

// Kernel 是固定 4 抽头因果反馈核，与 Floyd–Steinberg 同支撑。
// 字段按相对当前像素的空间偏移 (dx,dy) 命名，y 向下为正。
type Kernel struct {
	Name    string
	HRight  float64 // ( 1, 0) 右邻
	HBelow  float64 // ( 0, 1) 正下
	HBelowR float64 // ( 1, 1) 右下
	HBelowL float64 // (-1, 1) 左下
}

// FloydSteinberg 是现成核，保留为可选项；Σh=1，|NTF(0)|=2。
// 抽头顺序与 analysis/design.py 的 SUPPORT 一致。
var FloydSteinberg = Kernel{
	Name:    "floyd-steinberg",
	HRight:  7.0 / 16.0, // (1,0)
	HBelow:  5.0 / 16.0, // (0,1)
	HBelowR: 1.0 / 16.0, // (1,1) 右下
	HBelowL: 3.0 / 16.0, // (-1,1) 左下
}

// 三个反馈核由 analysis/design.py 的 design_h() 解出，见 analysis/DESIGN.md。
// 系数顺序对应 SUPPORT = [(1,0),(0,1),(1,1),(-1,1)]。
// 低频目标幅度 a=0.5，区别在低频带宽 r_b（r=1 对应 Nyquist π）。
var (
	// FeedbackRB15：r_b=0.15，|NTF(0)|=0.7886，mean|NTF|²=1.0112。
	FeedbackRB15 = Kernel{
		Name:    "feedback-rb15",
		HRight:  -0.05536150526357988,
		HBelow:  -0.05454378879122922,
		HBelowR: -0.05028455831066433,
		HBelowL: -0.05116134367225998,
	}
	// FeedbackRB25：r_b=0.25，|NTF(0)|=0.6539，mean|NTF|²=1.0304。
	FeedbackRB25 = Kernel{
		Name:    "feedback-rb25",
		HRight:  -0.10052589573427219,
		HBelow:  -0.09175054571702927,
		HBelowR: -0.0714866398678443,
		HBelowL: -0.08238108904710978,
	}
	// FeedbackRB35：r_b=0.35，|NTF(0)|=0.5802，mean|NTF|²=1.0481。
	FeedbackRB35 = Kernel{
		Name:    "feedback-rb35",
		HRight:  -0.14675986489004705,
		HBelow:  -0.1167217198867613,
		HBelowR: -0.05860738166348965,
		HBelowL: -0.09770878256935041,
	}
)

// Kernels 列出全部可选核，现成核在前。
func Kernels() []Kernel {
	return []Kernel{FloydSteinberg, FeedbackRB15, FeedbackRB25, FeedbackRB35}
}

// ErrBadSize 表示画幅、切片长度或量化级数不合法。
var ErrBadSize = errors.New("dither: 画幅或量化级数非法")

// Diffuse 对 [0,1] 灰度图做 levels 级均匀量化的误差扩散，返回量化后的
// [0,1] 图像与逐点量化误差 e=y-u。levels 必须 >= 2。
func Diffuse(x []float64, w, h, levels int, k Kernel) ([]float64, []float64, error) {
	if w <= 0 || h <= 0 || len(x) != w*h || levels < 2 {
		return nil, nil, ErrBadSize
	}
	q := 1.0 / float64(levels-1)
	lmax := float64(levels - 1)
	y := make([]float64, len(x))
	e := make([]float64, len(x))

	for row := 0; row < h; row++ {
		base := row * w
		prev := base - w
		var carry float64 // 右邻抽头的行内递推
		for col := 0; col < w; col++ {
			idx := base + col
			u := x[idx] + k.HRight*carry
			if row > 0 {
				u += k.HBelow * e[prev+col]
				if col > 0 {
					// (-1,1) 左下：上一行 col-1
					u += k.HBelowL * e[prev+col-1]
				}
				if col+1 < w {
					// (1,1) 右下：上一行 col+1
					u += k.HBelowR * e[prev+col+1]
				}
			}
			level := int(math.Round(u / q))
			if level < 0 {
				level = 0
			} else if level > int(lmax) {
				level = int(lmax)
			}
			yo := float64(level) * q
			ec := yo - u
			y[idx] = yo
			e[idx] = ec
			carry = ec
		}
	}
	return y, e, nil
}
