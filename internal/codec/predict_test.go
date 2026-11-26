package codec

import (
	"bytes"
	"math"
	"testing"
)

// lcgImage 生成确定性的伪随机图，seed 决定内容。
func lcgImage(w, h int, seed uint32) []uint8 {
	pix := make([]uint8, w*h)
	s := seed
	for i := range pix {
		s = s*1664525 + 1013904223
		pix[i] = uint8(s >> 24)
	}
	return pix
}

func gradientImage(w, h int) []uint8 {
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8((x + y) / 2)
		}
	}
	return pix
}

func TestPredRoundTrip(t *testing.T) {
	w, h := 17, 9
	pix := lcgImage(w, h, 42)
	for order := 0; order <= MaxOrder; order++ {
		body, err := EncodePRED(w, h, pix, order)
		if err != nil {
			t.Fatalf("order %d 编码失败: %v", order, err)
		}
		gw, gh, got, err := DecodePRED(body)
		if err != nil || gw != w || gh != h {
			t.Fatalf("order %d 解码失败: %v", order, err)
		}
		if !bytes.Equal(got, pix) {
			t.Fatalf("order %d 往返不一致", order)
		}
	}
}

// narrowNoise 生成均值 100、幅度 ±16 的 iid 窄带噪声：
// 0 阶熵远低于 8 bit 饱和点，加阶后熵明显增大，是标准反例。
func narrowNoise(w, h int, seed uint32) []uint8 {
	pix := make([]uint8, w*h)
	s := seed
	for i := range pix {
		s = s*1664525 + 1013904223
		pix[i] = 100 + uint8((s>>24)&31)
	}
	return pix
}

// 反例条件：iid 噪声下 σ²(e_1)/σ²(e_0) ≈ 2 > 1，加阶必增熵。
func TestVarianceRatioConditionOnNoise(t *testing.T) {
	w, h := 128, 128
	pix := narrowNoise(w, h, 7)
	stats, chosen := ChooseOrder(w, h, pix, 2)
	if chosen != 0 {
		t.Fatalf("噪声图应停在 0 阶，实际 %d", chosen)
	}
	if stats[1].Ratio <= 1 {
		t.Fatalf("噪声图方差比应 > 1，实际 %.3f", stats[1].Ratio)
	}
	if stats[1].Entropy <= stats[0].Entropy {
		t.Fatalf("噪声图加阶应增熵: %.4f -> %.4f", stats[0].Entropy, stats[1].Entropy)
	}
}

// 平滑图方差比 < 1，开关应持续加阶。
func TestChooseOrderOnGradient(t *testing.T) {
	w, h := 64, 64
	pix := gradientImage(w, h)
	stats, chosen := ChooseOrder(w, h, pix, 3)
	if chosen == 0 {
		t.Fatal("平滑图应至少升到 1 阶")
	}
	for _, st := range stats[1 : chosen+1] {
		if st.Ratio > 1 {
			t.Fatalf("order %d 方差比 %.3f > 1 却被接受", st.Order, st.Ratio)
		}
	}
}

// 开关的不变量：同一位深下，选定阶的熵不超过 0 阶（不预测）的熵。
func TestAutoOrderNeverRaisesEntropy(t *testing.T) {
	imgs := map[string][]uint8{
		"uniform-noise": lcgImage(128, 128, 7),
		"narrow-noise":  narrowNoise(128, 128, 7),
		"gradient":      gradientImage(64, 64),
	}
	for name, pix := range imgs {
		w := int(math.Sqrt(float64(len(pix))))
		stats, chosen := ChooseOrder(w, w, pix, MaxOrder)
		if stats[chosen].Entropy > stats[0].Entropy {
			t.Fatalf("%s: 开关选定阶熵 %.4f 高于 0 阶 %.4f", name, stats[chosen].Entropy, stats[0].Entropy)
		}
	}
}
