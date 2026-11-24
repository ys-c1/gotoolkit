package sharpen

import (
	"fmt"
	"math"
)

// EdgeCase 是一档对比度的测试场景参数。
type EdgeCase struct {
	Contrast int     // 标称对比度 C = hi-lo（灰阶）
	Lo, Hi   int     // 平台灰度
	Alpha    float64 // 平台正弦纹理幅度 / C
	Period   float64 // 平台正弦纹理周期（像素）
	Width    int     // 画幅宽
	Height   int     // 画幅高
}

// DefaultEdgeCases 是报告使用的三档对比度：低 64、中 128、高 192。
func DefaultEdgeCases() []EdgeCase {
	return []EdgeCase{
		{Contrast: 64, Lo: 96, Hi: 160, Alpha: 0.09, Period: 3.2, Width: 96, Height: 24},
		{Contrast: 128, Lo: 64, Hi: 192, Alpha: 0.09, Period: 3.2, Width: 96, Height: 24},
		{Contrast: 192, Lo: 32, Hi: 224, Alpha: 0.09, Period: 3.2, Width: 96, Height: 24},
	}
}

// Render 生成一档对比度的灰度图：中央一条 4 像素过渡的软边，两侧平台叠加
// 与对比度成比例的高频正弦纹理。纹理是真实照片里决定「允许增益」的约束项：
// 没有纹理时，限制只来自平台到 255/0 的 headroom，增益与核宽无关；
// 有纹理时，宽核会先因高频响应被压住而腾出更多 headroom。
func (e EdgeCase) Render() []uint8 {
	pix := make([]uint8, e.Width*e.Height)
	c := e.Width / 2
	amp := e.Alpha * float64(e.Contrast)
	for y := 0; y < e.Height; y++ {
		for x := 0; x < e.Width; x++ {
			var v float64
			switch {
			case x < c-1:
				v = float64(e.Lo)
			case x > c+1:
				v = float64(e.Hi)
			default:
				v = float64(e.Lo) + float64(e.Hi-e.Lo)*float64(x-(c-2))/4
			}
			if x < c-7 || x > c+7 {
				v += amp * math.Sin(2*math.Pi*float64(x)/e.Period)
			}
			v = math.Round(v)
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			pix[y*e.Width+x] = uint8(v)
		}
	}
	return pix
}

// Name 是报告里的短名。
func (e EdgeCase) Name() string { return fmt.Sprintf("C=%d", e.Contrast) }
