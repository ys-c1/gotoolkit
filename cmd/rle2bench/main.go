// rle2bench 生成三类代表性灰度图（常值/长渐变/高熵噪声，以及一张三段混合图），
// 对 GTR2 三档分别给出体积与香农熵下界，并单列转义块统计与“一成判据”。
// 同时给出 GTR1 体积作对照。纯计算工具，不依赖 testdata。
package main

import (
	"fmt"
	"math"

	"gotoolkit/api"
	"gotoolkit/internal/codec"
)

const side = 256 // 256x256 = 65536 像素

func flatImage() *api.Image {
	return &api.Image{W: side, H: side, Pix: make([]uint8, side*side)}
}

// rampImage 长渐变：每行像素值随列缓慢变化，游程普遍很短，GTR1 几乎压不动。
func rampImage() *api.Image {
	pix := make([]uint8, side*side)
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			pix[y*side+x] = uint8((x + y/16) & 0xff)
		}
	}
	return &api.Image{W: side, H: side, Pix: pix}
}

// noiseImage 高熵伪噪声（确定性 LCG），应全部转义。
func noiseImage() *api.Image {
	pix := make([]uint8, side*side)
	seed := uint32(0x12345678)
	for i := range pix {
		seed = seed*1103515245 + 12345
		pix[i] = uint8(seed >> 16)
	}
	return &api.Image{W: side, H: side, Pix: pix}
}

// textureImage 中等纹理：16 个符号、每游程固定长度，落在转义阈值（H=4）
// 以下且游程众多。
func textureImage() *api.Image {
	pix := make([]uint8, side*side)
	runLen := 8
	for i := range pix {
		pix[i] = uint8((i / runLen) % 16)
	}
	return &api.Image{W: side, H: side, Pix: pix}
}

// longRunImage 长游程图：游程长度在 20..230 间循环，跨越三档 cap 的钳位
// 区间。tier3 的小 cap（H 较低时约 33..64）会把长游程切成多对，tier1 的
// cap=255 基本不切——三档体积因此拉开差距。
func longRunImage() *api.Image {
	pix := make([]uint8, side*side)
	runLen, sym := 20, uint8(0)
	for i := 0; i < len(pix); {
		n := runLen
		if rem := len(pix) - i; n > rem {
			n = rem
		}
		for k := 0; k < n; k++ {
			pix[i+k] = sym
		}
		i += n
		sym = (sym + 1) % 16 // 16 符号，H≈4 以下
		runLen += 23         // 游程长度在 20..250+ 间拉开，跨越三档 cap 阈值
		if runLen > 250 {
			runLen = 20
		}
	}
	return &api.Image{W: side, H: side, Pix: pix}
}

// mixedImage 三段混合：上 1/3 常值、中 1/3 长渐变、下 1/3 噪声。
func mixedImage() *api.Image {
	img := &api.Image{W: side, H: side, Pix: make([]uint8, side*side)}
	third := side / 3
	seed := uint32(0xabcdef01)
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			switch {
			case y < third:
				img.Pix[y*side+x] = 200
			case y < 2*third:
				img.Pix[y*side+x] = uint8((x + (y-third)/8) & 0xff)
			default:
				seed = seed*1103515245 + 12345
				img.Pix[y*side+x] = uint8(seed >> 16)
			}
		}
	}
	return img
}

// shannon 复算整图熵下界（字节），与 codec 内逐块求和相互独立、便于交叉核对。
func shannon(pix []uint8) int {
	var freq [256]int
	for _, v := range pix {
		freq[v]++
	}
	n := float64(len(pix))
	bits := 0.0
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		bits -= p * math.Log2(p)
	}
	return int(math.Ceil(bits * n / 8.0))
}

func main() {
	cases := []struct {
		name string
		img  *api.Image
	}{
		{"常值 flat", flatImage()},
		{"长渐变 ramp", rampImage()},
		{"中纹理 texture", textureImage()},
		{"长游程 longrun", longRunImage()},
		{"高熵 noise", noiseImage()},
		{"混合 mixed", mixedImage()},
	}

	for _, c := range cases {
		g1, err := codec.EncodeRLE(c.img.W, c.img.H, c.img.Pix)
		if err != nil {
			panic(err)
		}
		rawBound := shannon(c.img.Pix)
		fmt.Printf("=== %s（%dx%d，%d 像素；整图熵下界 %d 字节；GTR1 体积 %d 字节）===\n",
			c.name, c.img.W, c.img.H, len(c.img.Pix), rawBound, len(g1))
		for tier := 1; tier <= 3; tier++ {
			body, rep, err := api.EncodeRLE2WithStats(c.img, tier)
			if err != nil {
				panic(err)
			}
			// 回读校验，确保报告的体积对应无损流。
			w, h, got, err := codec.DecodeRLE2(body)
			if err != nil || w != c.img.W || h != c.img.H || len(got) != len(c.img.Pix) {
				panic(fmt.Sprintf("tier%d 回读失败: %v", tier, err))
			}
			// 一成判据只在熵下界非零时有定义；常值块（下界 0）标注为 n/a。
			gap, verdict := 0.0, "OK"
			if rep.EntropyBoundRaw == 0 {
				verdict = "n/a(常值块熵下界为0)"
			} else {
				gap = 100.0 * float64(rep.PayloadBytes-rep.EntropyBoundRaw) /
					float64(rep.EntropyBoundRaw)
				if gap > 10.0 {
					verdict = "超一成(末块已到64像素下限，RLE对该数据固有低效)"
				}
			}
			fmt.Printf("  档%d K=%4d | 体积=%6d 下界=%6d | 块=%4d(游程=%4d 转义=%4d) 二分=%4d | 最差块比=%.3f 负载差距=%6s %s\n",
				rep.Tier, rep.K, rep.TotalBytes, rep.EntropyBoundRaw,
				rep.BlockCount, rep.RLEBlocks, rep.EscapeBlocks, rep.SplitCount,
				rep.WorstRatio, gapLabel(gap, rep.EntropyBoundRaw), verdict)
		}
		fmt.Println()
	}
}

func gapLabel(gap float64, bound int) string {
	if bound == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%5.1f%%", gap)
}
