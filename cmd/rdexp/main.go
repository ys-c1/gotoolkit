// rdexp 在 gotoolkit 的编码原语上搭一条三档有损流水线，实测码率-失真曲线。
//
// 流水线：原图 -> 标量量化(步长 Δ) -> GTR1 RLE(internal/codec) -> 码率
// 失真：量化重建图相对原图的 MSE / PSNR。
//
// 三档：
//   高档 Δ=2，round-to-nearest（重建电平在区间质心，理想量化器）
//   中档 Δ=4：
//     buggy 模式用 floor 截断重建（x/Δ*Δ，整数除法捷径）——复现凹点
//     fixed 模式仍用 round 质心重建
//   低档 Δ=8，round-to-nearest
//
// 输出 JSON 到 stdout，供分析脚本计算二阶差分、凸包与偏离量。
package main

import (
	"encoding/json"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

const W, H = 256, 256

// genImage 生成平滑渐变 + 低幅噪声的灰度图：量化后产生长游程，RLE 可压缩。
func genImage() []uint8 {
	pix := make([]uint8, W*H)
	// 确定性 LCG 噪声，保证可复现。
	seed := uint32(0x1234abcd)
	rand := func() uint32 {
		seed = seed*1103515245 + 12345
		return seed >> 16
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			base := 0.55*float64(x) + 0.35*float64(y) // 双线性平滑斜面
			// 叠加一条平缓正弦带，制造不同曲率的区域。
			base += 24 * math.Sin(2*math.Pi*float64(y)/64.0)
			n := int(rand()%7) - 3 // [-3,3] 均匀噪声
			v := int(math.Round(base)) + n
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			pix[y*W+x] = uint8(v)
		}
	}
	return pix
}

func clamp(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// quantizeRound：最近电平重建，重建点位于量化区间质心。
func quantizeRound(pix []uint8, delta int) []uint8 {
	out := make([]uint8, len(pix))
	for i, x := range pix {
		out[i] = clamp((int(x)+delta/2) / delta * delta)
	}
	return out
}

// quantizeFloor：截断（floor）重建，整数除法 x/Δ*Δ；重建点偏在区间下端。
func quantizeFloor(pix []uint8, delta int) []uint8 {
	out := make([]uint8, len(pix))
	for i, x := range pix {
		out[i] = clamp(int(x) / delta * delta)
	}
	return out
}

type Point struct {
	Level    string  `json:"level"`
	Delta    int     `json:"delta"`
	Mode     string  `json:"mode"` // round | floor
	RateBPP  float64 `json:"rate_bpp"`
	Bytes    int     `json:"bytes"`
	MSE      float64 `json:"mse"`
	PSNR     float64 `json:"psnr_db"`
}

func measure(level string, delta int, mode string, orig, q []uint8) Point {
	body, err := codec.EncodeRLE(W, H, q)
	if err != nil {
		panic(err)
	}
	var se float64
	for i := range orig {
		d := float64(orig[i]) - float64(q[i])
		se += d * d
	}
	mse := se / float64(len(orig))
	psnr := 10 * math.Log10(255.0*255.0/mse)
	return Point{
		Level:   level,
		Delta:   delta,
		Mode:    mode,
		RateBPP: float64(len(body)) / float64(len(orig)),
		Bytes:   len(body),
		MSE:     mse,
		PSNR:    psnr,
	}
}

func main() {
	orig := genImage()

	type run struct {
		label string
		delta int
		floor bool // true = buggy 截断重建
	}
	runs := []run{
		{"high", 2, false},
		{"mid", 4, false}, // fixed 基准：中档也用质心重建
		{"low", 8, false},
	}

	result := map[string][]Point{
		"fixed": {},
		"buggy": {},
	}
	for _, r := range runs {
		q := quantizeRound(orig, r.delta)
		result["fixed"] = append(result["fixed"], measure(r.label, r.delta, "round", orig, q))
	}
	// buggy：仅中档换成 floor 截断，高、低档不变。
	for _, r := range runs {
		var q []uint8
		mode := "round"
		if r.label == "mid" {
			q = quantizeFloor(orig, r.delta)
			mode = "floor"
		} else {
			q = quantizeRound(orig, r.delta)
		}
		result["buggy"] = append(result["buggy"], measure(r.label, r.delta, mode, orig, q))
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
