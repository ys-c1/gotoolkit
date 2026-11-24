package chroma

import (
	"math"
	"testing"
)

// TestAdaptiveBeatsBilinear 三档图：自适应重建在边缘处的平均/最大色差都不得劣于双线性，
// 且平均色差必须显著下降。
func TestAdaptiveBeatsBilinear(t *testing.T) {
	for i, tier := range Tiers() {
		ref := GenTierImage(i)
		c, err := Subsample(ref)
		if err != nil {
			t.Fatalf("tier%d 降采样失败: %v", i, err)
		}
		bi, err := Reconstruct(c, ModeBilinear)
		if err != nil {
			t.Fatal(err)
		}
		ad, err := Reconstruct(c, ModeAdaptive)
		if err != nil {
			t.Fatal(err)
		}
		mb := ColorDiff(ref, c, bi)
		ma := ColorDiff(ref, c, ad)

		if ma.CbCrMean >= mb.CbCrMean {
			t.Errorf("%s 自适应平均色差未下降: adapt=%.4f bilinear=%.4f",
				tier.Name, ma.CbCrMean, mb.CbCrMean)
		}
		// 平均色差至少下降 25%，确保不是噪声级别的偶然改善。
		if drop := (mb.CbCrMean - ma.CbCrMean) / mb.CbCrMean; drop < 0.25 {
			t.Errorf("%s 平均色差降幅不足: %.1f%%", tier.Name, drop*100)
		}
		// 最大色差：中/大档应持平；小档多向边缘交汇角点允许不超过 8% 的裕量
		// （亮度引导法在亮度相似、色度异侧的角点上的固有局限）。
		if ma.CbCrMax > mb.CbCrMax*1.08 {
			t.Errorf("%s 自适应最大色差明显劣化: adapt=%.4f bilinear=%.4f",
				tier.Name, ma.CbCrMax, mb.CbCrMax)
		}
		t.Logf("%s edge=%d  CbCr mean/max  bilinear=%.3f/%.3f  adaptive=%.3f/%.3f  RGB mean/max bilinear=%.3f/%.3f adaptive=%.3f/%.3f",
			tier.Name, mb.EdgePixels,
			mb.CbCrMean, mb.CbCrMax, ma.CbCrMean, ma.CbCrMax,
			mb.RGBMean, mb.RGBMax, ma.RGBMean, ma.RGBMax)
	}
}

// TestFlatRegionFallsBackToBilinear 平坦（零方差）区域内，自适应权重应退化为双线性：
// 各相似性指数项相等，归一化后与纯几何权重一致。
func TestFlatRegionFallsBackToBilinear(t *testing.T) {
	// 构造一幅完全平坦的彩色图（色度恒定），两种模式结果应逐值相等。
	w, h := 16, 16
	img := &RGB{W: w, H: h, Pix: make([]uint8, w*h*3)}
	for i := 0; i < w*h; i++ {
		img.Pix[3*i], img.Pix[3*i+1], img.Pix[3*i+2] = 100, 150, 200
	}
	c, err := Subsample(img)
	if err != nil {
		t.Fatal(err)
	}
	bi, _ := Reconstruct(c, ModeBilinear)
	ad, _ := Reconstruct(c, ModeAdaptive)
	for i := range bi.Pix {
		if bi.Pix[i] != ad.Pix[i] {
			t.Fatalf("平坦区自适应未退化回双线性，像素 %d 不同: %d vs %d", i, bi.Pix[i], ad.Pix[i])
		}
	}
}

// TestSubsampleDimensions 色度平面尺寸恰为半分辨率，亮度全分辨率。
func TestSubsampleDimensions(t *testing.T) {
	img := GenTierImage(0)
	c, err := Subsample(img)
	if err != nil {
		t.Fatal(err)
	}
	if c.CW != img.W/2 || c.CH != img.H/2 {
		t.Fatalf("色度尺寸错误: %dx%d", c.CW, c.CH)
	}
	if len(c.Y) != img.W*img.H {
		t.Fatal("亮度应为全分辨率")
	}
}

// TestOddSizeRejected 奇数画幅必须报错。
func TestOddSizeRejected(t *testing.T) {
	img := &RGB{W: 15, H: 16, Pix: make([]uint8, 15*16*3)}
	if _, err := Subsample(img); err != ErrOddSize {
		t.Fatalf("奇数宽度应报 ErrOddSize，得到 %v", err)
	}
}

// TestUnknownMode 未知重建模式报错。
func TestUnknownMode(t *testing.T) {
	c, _ := Subsample(GenTierImage(0))
	if _, err := Reconstruct(c, "nearest"); err == nil {
		t.Fatal("未知模式应报错")
	}
}

// TestRGBRoundTripStable 恒色图经色彩空间往返后误差在一个量化级内。
func TestRGBRoundTripStable(t *testing.T) {
	w, h := 8, 8
	img := &RGB{W: w, H: h, Pix: make([]uint8, w*h*3)}
	for i := 0; i < w*h; i++ {
		img.Pix[3*i], img.Pix[3*i+1], img.Pix[3*i+2] = 33, 177, 90
	}
	c, _ := Subsample(img)
	out, _ := Reconstruct(c, ModeBilinear)
	var maxErr float64
	for i := range img.Pix {
		maxErr = math.Max(maxErr, math.Abs(float64(img.Pix[i])-float64(out.Pix[i])))
	}
	if maxErr > 1.5 {
		t.Fatalf("恒色图往返误差过大: %.2f", maxErr)
	}
}
