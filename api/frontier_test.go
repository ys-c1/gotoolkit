package api

import (
	"math"
	"testing"
)

// 本文件对应 docs/quant-step.md 的推导：
// 256×256 行斜坡图经 RLE 编码，体积模型 V(s) = 520 + K/s（K = 2·256·256），
// 失真模型 D(s) = s²/12（整数像素离散修正为 (s²+2)/12）。
// 拉格朗日解 s*(T) = K/(T-520)，三档目标体积的实测点应落在 Pareto 前沿上。

const (
	frontierK = 2 * 256 * 256 // V(s) 的 K：每条游程 2 字节，每行 256/s 条，共 256 行
	frontierC = 8 + 2*256     // V(s) 的常数项：8 字节头 + 每行 1 条边界游程
)

// rampImage 生成 256×256 行斜坡测试图：每行 0..255。
func rampImage() *Image {
	img := &Image{W: 256, H: 256, Pix: make([]uint8, 256*256)}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Pix[y*256+x] = uint8(x)
		}
	}
	return img
}

// measure 走完整 RLE 编解码，返回产物体积（字节）与 MSE。
func measure(t *testing.T, img *Image, step int) (int, float64) {
	t.Helper()
	body, err := Encode(img, Options{Format: "rle", Step: step})
	if err != nil {
		t.Fatalf("step=%d 编码失败: %v", step, err)
	}
	got, err := Decode(body, Options{Format: "rle"})
	if err != nil {
		t.Fatalf("step=%d 解码失败: %v", step, err)
	}
	var sse float64
	for i, v := range got.Pix {
		d := float64(v) - float64(img.Pix[i])
		sse += d * d
	}
	return len(body), sse / float64(len(img.Pix))
}

// TestStepFrontier 三档目标体积：验证拉格朗日解出的步长实测后落在 Pareto 前沿上。
func TestStepFrontier(t *testing.T) {
	img := rampImage()
	targets := []struct {
		name string
		targ int // 目标体积（字节）
	}{
		{"大", 33288}, // 解出 s*=4
		{"中", 16904}, // 解出 s*=8
		{"小", 8712},  // 解出 s*=16
	}

	// 扫描全部整数步长，建立实测点集，用于 Pareto 判定。
	type pt struct {
		s, v int
		d    float64
	}
	all := make([]pt, 0, 255)
	for s := 1; s <= 255; s++ {
		v, d := measure(t, img, s)
		all = append(all, pt{s, v, d})
	}
	dominated := func(p pt) bool {
		for _, q := range all {
			if q.v <= p.v && q.d <= p.d && (q.v < p.v || q.d < p.d) {
				return true
			}
		}
		return false
	}

	for _, tc := range targets {
		// 拉格朗日连续解 s* = K/(T-C)，乘子 λ* = s*³/(6K)。
		sStar := float64(frontierK) / float64(tc.targ-frontierC)
		lambda := sStar * sStar * sStar / (6 * frontierK)
		s := int(math.Round(sStar))
		v, d := measure(t, img, s)

		if v > tc.targ {
			t.Fatalf("%s档: step=%d 实测体积 %d 超目标 %d", tc.name, s, v, tc.targ)
		}
		// 最优性：预算内不存在误差更小的其它步长，否则说明步长分配不均。
		for _, q := range all {
			if q.v <= tc.targ && q.d < d {
				t.Fatalf("%s档: step=%d 偏离前沿，step=%d 同预算误差更小 (%.4f < %.4f)",
					tc.name, s, q.s, q.d, d)
			}
		}
		if dominated(pt{s, v, d}) {
			t.Fatalf("%s档: step=%d 被其它点支配，不在 Pareto 前沿上", tc.name, s)
		}
		dModel := (sStar*sStar + 2) / 12 // 离散修正前沿
		t.Logf("%s档 目标=%6dB  s*=%.3f  取整s=%2d  λ*=%.4e  前沿D*=%.4f  实测V=%6dB  实测MSE=%.4f  PSNR=%.2fdB",
			tc.name, tc.targ, sStar, s, lambda, dModel, v, d,
			10*math.Log10(255*255/d))
	}
}
