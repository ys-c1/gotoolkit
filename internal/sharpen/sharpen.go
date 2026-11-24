// Package sharpen 实现高斯反锐化掩模，并反解「过冲为零」约束下的最大增益与核宽。
//
// # 锐化模型
//
//	L  = G_k * I                  归一化高斯模糊，k 为奇数，σ=(k-1)/3
//	H  = I - L                    高通，DC 为 0
//	I' = I + g * H                反锐化掩模
//
// k=1 时 G_1=I、H≡0，锐化退化为恒等，这条路径保证「未锐化产物逐字节不变」。
// 计算在 float64 中进行，落盘前按四舍五入钳位到 [0,255]。
//
// # 过冲的定义
//
// 过冲量定义为锐化输出越过合法灰度区间 [0,255] 的部分：
//
//	over(x) = max(0, I'(x)-255) + max(0, -I'(x))
//
// 一旦 over>0，该像素量化后被截平为 255 或 0，在原本不是极值的边缘旁出现
// 一条纯白或纯黑的细线——即「锐化一开就出白边」的白边。因此「过冲为零」
// 即「输出始终落在 [0,255] 内」，锐化不得新造纯白/纯黑。
//
// 若改用「越过原图局部极值」作判据，任何 g>0 都必然过冲：任一局部极大
// 像素只要有更暗的邻居，模糊就把它拉低、高通为正，锐化随即抬高它，
// 该判据下唯一解是 g=0（不锐化），与需求矛盾。故采用「不越界」。
//
// # 逐像素闭式解
//
// 由 I'(x)=v+g*h(x) 与 0<=I'<=255：
//
//	g <= (255-v)/h   (h>0);      g <= v/(-h)   (h<0)。
//
// 整幅图允许的最大增益
//
//	g* = min_x { (255-v_x)/h_x (h_x>0); v_x/(-h_x) (h_x<0) }
//
// 是精确反解，无需搜索；数值二分只用于独立校核（见 Search）。
//
// # 阶跃边缘上的闭式解
//
// 单位阶跃经核模糊后，亮侧首像素的高通归一化幅值
//
//	D_k = (1 - w_c(k))/2,   w_c 为核中心权重，
//
// 暗侧首像素为 -D_k。阶跃置于 lo/hi、两侧为平台时可用 headroom
//
//	H = min(255-hi, lo)，
//
// 最大增益
//
//	g_max(C,k) = H / (C * D_k)，  C = hi-lo。
//
// 此时两边缘像素恰好被推到 255 与 0：边缘增幅 g*C*D_k = H 已达到「不截断」
// 下的上界，锐化效果最大而白边为零。增益 g ∝ 1/(C·D_k)：随核宽增大
// （D_k 增大）而减小，随对比度增大而减小。
package sharpen

import (
	"errors"
	"fmt"
	"math"
)

// eps 是浮点噪声容差（灰阶）。
const eps = 1e-9

// D 用闭式解给出核宽 k 的阶跃高通峰值 D_k=(1-w_c)/2。
func D(k int) float64 {
	w, err := Gaussian(k)
	if err != nil {
		return math.NaN()
	}
	return (1 - w[k/2]) / 2
}

// Gaussian 构造 σ=(k-1)/3 的归一化高斯核，3σ 截断落在 k 个抽头上。
func Gaussian(k int) ([]float64, error) {
	if k < 1 || k%2 == 0 {
		return nil, fmt.Errorf("核宽必须是正奇数: %d", k)
	}
	if k == 1 {
		return []float64{1}, nil
	}
	sigma := float64(k-1) / 3
	w := make([]float64, k)
	var sum float64
	c := float64(k / 2)
	for i := range w {
		d := float64(i) - c
		w[i] = math.Exp(-0.5 * d * d / (sigma * sigma))
		sum += w[i]
	}
	for i := range w {
		w[i] /= sum
	}
	return w, nil
}

// StepResponse 返回单位幅值阶跃（左 0 右 1）的高通响应 d[j]；j 从 0 起为
// 像素编号，边缘位于 r-1 与 r 之间。用真实核计算，供 StepD 校核闭式解。
func StepResponse(kern []float64, span int) []float64 {
	k := len(kern)
	r := k / 2
	out := make([]float64, span)
	for j := 0; j < span; j++ {
		var l float64
		for t := 0; t < k; t++ {
			sx := j + t - r
			if sx < 0 {
				sx = 0
			}
			if sx >= span {
				sx = span - 1
			}
			if sx >= r {
				l += kern[t]
			}
		}
		v := 0.0
		if j >= r {
			v = 1
		}
		out[j] = v - l
	}
	return out
}

// StepD 用真实核数值计算阶跃高通峰值，对照闭式 (1-w_c)/2。
func StepD(kern []float64) float64 {
	return Peak(StepResponse(kern, 4*len(kern)))
}

// ClosedFormStepGain 给出 lo/hi 阶跃在核宽 k 下的最大增益闭式解：
// g = H/(C*D_k)，H=min(255-hi, lo)，D_k=(1-w_c)/2。
func ClosedFormStepGain(lo, hi int, kern []float64) float64 {
	C := float64(hi - lo)
	H := math.Min(255-float64(hi), float64(lo))
	return H / (C * D(len(kern)))
}

func clampI(v, n int) int {
	if v < 0 {
		return 0
	}
	if v >= n {
		return n - 1
	}
	return v
}

// Blur 做可分离高斯模糊（边界复制），float64 结果。
func Blur(w, h int, pix []uint8, kern []float64) []float64 {
	k := len(kern)
	r := k / 2
	tmp := make([]float64, w*h)
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			var acc float64
			for t := 0; t < k; t++ {
				acc += kern[t] * float64(pix[row+clampI(x+t-r, w)])
			}
			tmp[row+x] = acc
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var acc float64
			for t := 0; t < k; t++ {
				acc += kern[t] * tmp[clampI(y+t-r, h)*w+x]
			}
			out[y*w+x] = acc
		}
	}
	return out
}

// HighPass 返回 H = I - G*I。
func HighPass(w, h int, pix []uint8, kern []float64) []float64 {
	blur := Blur(w, h, pix, kern)
	for i := range blur {
		blur[i] = float64(pix[i]) - blur[i]
	}
	return blur
}

// Unclamped 返回未钳位的 I' = I + g*H，用于测量过冲。
func Unclamped(w, h int, pix []uint8, kern []float64, gain float64) []float64 {
	hp := HighPass(w, h, pix, kern)
	out := make([]float64, len(pix))
	for i, v := range pix {
		out[i] = float64(v) + gain*hp[i]
	}
	return out
}

// Apply 按 I' = I + g*(I-L) 锐化，四舍五入并钳位到 [0,255]。
// gain==0 或 k==1 时复制输入，保证未锐化产物逐字节不变。
func Apply(w, h int, pix []uint8, kern []float64, gain float64) []uint8 {
	out := make([]uint8, len(pix))
	if len(kern) == 1 || gain == 0 {
		copy(out, pix)
		return out
	}
	hp := HighPass(w, h, pix, kern)
	for i, v := range pix {
		x := float64(v) + gain*hp[i]
		if x <= 0 {
			out[i] = 0
		} else if x >= 255 {
			out[i] = 255
		} else {
			out[i] = uint8(math.Round(x))
		}
	}
	return out
}

// Clipping 统计未钳位输出的越界总量（灰阶之和）与像素数。
func Clipping(vals []float64) (total float64, count int) {
	for _, x := range vals {
		if x > 255+eps {
			total += x - 255
			count++
		} else if x < -eps {
			total += -x
			count++
		}
	}
	return total, count
}

// Clears 判断未钳位输出是否全部在 [0,255]。
func Clears(vals []float64) bool {
	_, n := Clipping(vals)
	return n == 0
}

// Peak 返回 |H| 的最大值。
func Peak(hp []float64) float64 {
	var m float64
	for _, v := range hp {
		if a := math.Abs(v); a > m {
			m = a
		}
	}
	return m
}

// ClosedForm 返回逐像素反解的闭式最大增益及起约束的像素下标。
// 全图高通为零时返回 +Inf、-1。
func ClosedForm(w, h int, pix []uint8, kern []float64) (gain float64, binding int) {
	gain = math.Inf(1)
	binding = -1
	hp := HighPass(w, h, pix, kern)
	for i, v := range pix {
		hh := hp[i]
		if math.Abs(hh) < eps {
			continue
		}
		bound := float64(v) / -hh
		if hh > 0 {
			bound = (255 - float64(v)) / hh
		}
		if bound < gain {
			gain, binding = bound, i
		}
	}
	return gain, binding
}

// Search 用二分法数值搜索零过冲的最大增益（上端从 2 倍增到越界为止）。
// 用于独立校核 ClosedForm，二者应在 1e-9 内一致。
func Search(w, h int, pix []uint8, kern []float64) (gain float64, iters int) {
	if len(kern) == 1 {
		return math.Inf(1), 0
	}
	hp := HighPass(w, h, pix, kern)
	ok := func(g float64) bool {
		for i, v := range pix {
			x := float64(v) + g*hp[i]
			if x > 255+eps || x < -eps {
				return false
			}
		}
		return true
	}
	lo, hi := 0.0, 1.0
	for ok(hi) {
		lo, hi = hi, hi*2
		iters++
		if hi > 1e18 {
			return math.Inf(1), iters
		}
	}
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		if ok(mid) {
			lo = mid
		} else {
			hi = mid
		}
		iters++
		if hi-lo <= 1e-12*math.Max(1, hi) {
			break
		}
	}
	return lo, iters
}

// Solution 是一次联合反解的结果。
type Solution struct {
	K       int       // 解出的核宽
	W       []float64 // 实际使用的核
	Gain    float64   // 解出的增益
	D       float64   // 该核 D_k
	HP      []float64 // 该核的高通
	Boost   float64   // 边缘增幅 g*max|H|（灰阶）
	Bind    int       // 起约束的像素下标
	PeakG   float64   // 实测未钳位最大越界（应为 0）
	Clipped int       // 实测越界像素数（应为 0）
}

// CandidateWidths 是默认候选核宽。
var CandidateWidths = []int{3, 5, 7, 9}

// Solve 在候选核宽中联合反解：每个核由 ClosedForm 得到零过冲最大增益，
// 取边缘增幅最大者；增幅并列时取最窄核。
//
// 在纯平台台阶上，g*max|H| 恒等于平台可用余量、与核宽无关（并列），此时
// 按最窄核选取；平台带纹理时，宽核对高频的响应更弱，能在不越界的前提下
// 保留更多边缘增幅，因而会被选中。
func Solve(w, h int, pix []uint8, candidates []int) (Solution, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return Solution{}, errors.New("画幅或像素数不匹配")
	}
	if len(candidates) == 0 {
		candidates = CandidateWidths
	}
	var best Solution
	found := false
	for _, k := range candidates {
		kern, err := Gaussian(k)
		if err != nil {
			return Solution{}, err
		}
		hp := HighPass(w, h, pix, kern)
		g, bind := ClosedForm(w, h, pix, kern)
		cand := Solution{
			K: k, W: kern, Gain: g, D: (1 - kern[k/2]) / 2, HP: hp, Bind: bind,
		}
		if math.IsInf(g, 1) {
			cand.Gain, cand.Boost = 1, 0
		} else {
			cand.Boost = g * Peak(hp)
		}
		better := !found || cand.Boost > best.Boost+eps ||
			(math.Abs(cand.Boost-best.Boost) <= eps && cand.K < best.K)
		if better {
			best, found = cand, true
		}
	}
	// 按实际使用的核回算实测过冲。
	vals := Unclamped(w, h, pix, best.W, best.Gain)
	best.PeakG, best.Clipped = Clipping(vals)
	return best, nil
}
