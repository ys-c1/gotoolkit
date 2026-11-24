package quantbias

import (
	"math"
	"math/rand"
)

// DefaultSize 是合成测试图的边长（256×256，65536 像素）。
const DefaultSize = 256

// DefaultSeed 固定随机种子，保证四类图与全部数值可复现。
const DefaultSeed int64 = 20261125

// SampleImages 确定性地生成四类测试图（每类 256×256）：
//
//   - gradient：非线性平滑渐变（低频、相位铺得开）
//   - photo   ：低频影调场 + 柔边色块 + 传感器噪声，模拟照片
//   - texture ：调频正弦/斜向织纹叠加缓变影调与中噪声（有方向的中频结构）
//   - noise   ：强高斯噪声 + 高频正弦纹理
//
// 每类使用独立派生种子，调用顺序不影响结果。
func SampleImages(n int, seed int64) map[string][]uint8 {
	out := make(map[string][]uint8, len(ClassNames))
	for i, class := range ClassNames {
		r := rand.New(rand.NewSource(seed + int64(i)*7919))
		switch class {
		case "gradient":
			out[class] = genGradient(n)
		case "photo":
			out[class] = genPhoto(n, r)
		case "texture":
			out[class] = genTexture(n, r)
		case "noise":
			out[class] = genNoise(n, r)
		}
	}
	return out
}

func clamp8(v float64) uint8 {
	v = math.Round(v)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func genGradient(n int) []uint8 {
	pix := make([]uint8, n*n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			// 轻微非线性的横向渐变 + 缓慢纵向起伏，避免完全理想的均匀相位。
			v := 255*math.Pow(float64(x)/float64(n-1), 1.15) +
				6*math.Sin(float64(y)/29.0)
			pix[y*n+x] = clamp8(v)
		}
	}
	return pix
}

type circle struct {
	cx, cy, rad, add float64
}

func genPhoto(n int, r *rand.Rand) []uint8 {
	circles := []circle{
		{70, 80, 46, 60},
		{190, 150, 60, -55},
		{170, 60, 26, 80},
	}
	pix := make([]uint8, n*n)
	for y := 0; y < n; y++ {
		fy := float64(y)
		for x := 0; x < n; x++ {
			fx := float64(x)
			v := 128 +
				52*math.Sin(fx/33.0)*math.Cos(fy/41.0) +
				28*math.Sin((fx+fy)/57.0)
			for _, c := range circles {
				dx, dy := fx-c.cx, fy-c.cy
				if dx*dx+dy*dy <= c.rad*c.rad {
					v += c.add
				}
			}
			v += r.NormFloat64() * 9
			pix[y*n+x] = clamp8(v)
		}
	}
	return pix
}

func genTexture(n int, r *rand.Rand) []uint8 {
	// 结构化纹理：调频正弦/斜向织纹叠加缓变影调与中等噪声。频谱与白噪声类
	// 不同（中频、有方向），但灰度级铺得足够密，相位分布接近均匀。
	pix := make([]uint8, n*n)
	for y := 0; y < n; y++ {
		fy := float64(y)
		for x := 0; x < n; x++ {
			fx := float64(x)
			v := 126 +
				54*math.Sin(fx*0.22+2.5*math.Sin(fy*0.045))*math.Cos(fy*0.18) +
				26*math.Sin((fx-fy)*0.55) +
				18*math.Sin((fx+fy)*0.11) +
				r.NormFloat64()*12
			pix[y*n+x] = clamp8(v)
		}
	}
	return pix
}

func genNoise(n int, r *rand.Rand) []uint8 {
	pix := make([]uint8, n*n)
	for y := 0; y < n; y++ {
		fy := float64(y)
		for x := 0; x < n; x++ {
			v := 128 + r.NormFloat64()*58 + 40*math.Sin(float64(x)*0.9) +
				12*math.Sin(fy*0.35)
			pix[y*n+x] = clamp8(v)
		}
	}
	return pix
}
