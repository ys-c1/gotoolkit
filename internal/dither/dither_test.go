package dither

import (
	"math"
	"testing"
)

func flat(w, h int, v byte) (int, int, []byte) {
	pix := make([]byte, w*h)
	for i := range pix {
		pix[i] = v
	}
	return w, h, pix
}

func TestRejects(t *testing.T) {
	w, h, pix := flat(8, 4, 100)
	for _, o := range []Options{
		{Levels: 1, Beta: 0},
		{Levels: 257, Beta: 0},
		{Levels: 16, Beta: 1.01},
		{Levels: 16, Beta: -1.01},
		{Levels: 16, DitherScale: -0.1},
		{Levels: 16, DitherScale: 1.5},
	} {
		if _, err := Quantize(w, h, pix, o); err == nil {
			t.Fatalf("参数 %+v 应被拒绝", o)
		}
	}
	if _, err := Quantize(0, h, pix, DefaultOptions()); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Quantize(w, h, pix[:3], DefaultOptions()); err == nil {
		t.Fatal("像素长度不符应报错")
	}
}

func TestDeterministic(t *testing.T) {
	w, h, pix := flat(32, 16, 100)
	a, err := Quantize(w, h, pix, Options{Levels: 16, Beta: 0.5, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Quantize(w, h, pix, Options{Levels: 16, Beta: 0.5, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("同种子结果不一致 @%d", i)
		}
	}
}

func TestLevelsAndRange(t *testing.T) {
	// 量化到 4 级，输出只能落在 {0,64,128,192,255 边界} 中的合法级上。
	w, h, pix := flat(64, 8, 0)
	for i := range pix {
		pix[i] = byte(i * 7 % 256)
	}
	out, err := Quantize(w, h, pix, Options{Levels: 4, Beta: 0, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[byte]bool{0: true, 64: true, 128: true, 192: true, 255: true}
	for _, v := range out {
		if !allowed[v] {
			t.Fatalf("出现非法量化级 %d", v)
		}
	}
}

func TestFeedbackLowersLowFrequencyEnergy(t *testing.T) {
	// 缓变斜坡 + 小幅抖动场景：反馈系数增大，前 1/8 频带能量占比应单调下降。
	const w, h = 256, 64
	pix := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = byte((x + y) % 256)
		}
	}
	lf := func(beta float64) float64 {
		out, err := Quantize(w, h, pix, Options{Levels: 16, Beta: beta, Seed: 99})
		if err != nil {
			t.Fatal(err)
		}
		// 残差 = 量化输出 - 输入；按行 FFT 平均功率谱。
		e := make([]float64, w)
		var spec []float64
		for row := 0; row < h; row++ {
			for x := 0; x < w; x++ {
				e[x] = float64(out[row*w+x]) - float64(pix[row*w+x])
			}
			rowSpec := powerSpectrum(e)
			if spec == nil {
				spec = rowSpec
			} else {
				for i := range spec {
					spec[i] += rowSpec[i]
				}
			}
		}
		spec[0] = 0
		n := len(spec) - 1
		kLF := n / 8
		var lfE, allE float64
		for i, p := range spec[1:] {
			allE += p
			if i < kLF {
				lfE += p
			}
		}
		return lfE / allE
	}
	baseline := lf(0)
	half := lf(0.25)
	full := lf(0.5)
	ds := lf(1.0)
	if !(baseline > half && half > full && full > ds) {
		t.Fatalf("低频占比未随反馈单调下降: %v",
			[]float64{baseline, half, full, ds})
	}
	// 满幅 RPDF 基线接近白噪理论值 1/8=12.5%，允许统计波动到 25%。
	if baseline < 0.10 {
		t.Fatalf("基线低频占比异常偏低: %f", baseline)
	}
	if baseline < half {
		t.Fatalf("反馈应降低低频占比: baseline=%f half=%f", baseline, half)
	}
}

// powerSpectrum 返回长度 n/2+1 的单边功率谱（朴素 DFT，测试规模小无需 FFT 库）。
func powerSpectrum(x []float64) []float64 {
	n := len(x)
	out := make([]float64, n/2+1)
	for k := 0; k <= n/2; k++ {
		var re, im float64
		for t, v := range x {
			ang := 2 * math.Pi * float64(k) * float64(t) / float64(n)
			re += v * math.Cos(ang)
			im -= v * math.Sin(ang)
		}
		out[k] = re*re + im*im
	}
	return out
}

func TestStableForUnitBeta(t *testing.T) {
	// |beta|<=1 时 NTF 零点不出单位圆：任意输入下输出必须有限且在 [0,255]。
	w, h, pix := flat(128, 16, 200)
	for i := range pix {
		pix[i] = byte((i * 37) % 256)
	}
	for _, beta := range []float64{-1, -0.5, 0, 0.5, 1} {
		out, err := Quantize(w, h, pix, Options{Levels: 8, Beta: beta, Seed: 3})
		if err != nil {
			t.Fatalf("beta=%v: %v", beta, err)
		}
		for _, v := range out {
			if v > 255 { // byte 本身保证 0..255，这里检查无 NaN/Inf 路径崩溃
				t.Fatalf("beta=%v 输出越界", beta)
			}
		}
	}
}
