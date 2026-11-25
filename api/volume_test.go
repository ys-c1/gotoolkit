package api

import (
	"testing"

	"gotoolkit/internal/hybrid"
)

// TestVolumeNeverExceedsAnySingle 是本任务硬约束的回归测试：
// 混合模式（含回退）的输出长度必须 ≤ 任一种单一格式。
func TestVolumeNeverExceedsAnySingle(t *testing.T) {
	for name, img := range allImages() {
		sizes := map[string]int{}
		for _, f := range []string{"pgm", "bmp", "rle"} {
			b, err := Encode(img, Options{Format: f})
			if err != nil {
				t.Fatalf("%s/%s: %v", name, f, err)
			}
			sizes[f] = len(b)
		}
		body, used, err := EncodeHybrid(img, DefaultOptions())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for f, n := range sizes {
			if len(body) > n {
				t.Fatalf("%s: 混合(%s)=%d 超过 %s=%d", name, used, len(body), f, n)
			}
		}
		// 无论走 GTH1 还是回退，都必须能解码，且同一码流解码可复现。
		got, err := Decode(body, Options{Format: "hybrid"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		again, err := Decode(body, Options{Format: "hybrid"})
		if err != nil || !bytesEqual(again.Pix, got.Pix) {
			t.Fatalf("%s: 解码不可复现", name)
		}
		if used != "hybrid" && !bytesEqual(got.Pix, img.Pix) {
			t.Fatalf("%s: 回退体 %s 未能无损解码", name, used)
		}
		// 体积必须严格小于 PGM 与 BMP（结构性保证），否则说明分区没起作用。
		if len(body) >= sizes["pgm"] || len(body) >= sizes["bmp"] {
			t.Fatalf("%s: 未严格小于 PGM/BMP", name)
		}
	}
}

// TestVolumeWorstCaseStructural 检验最坏情形：全纹理图也不超过 PGM/BMP。
func TestVolumeWorstCaseStructural(t *testing.T) {
	rng := newRNG(0xfeed)
	img := &Image{W: 160, H: 160, Pix: make([]uint8, 160*160)}
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.next() >> 56) // 完全不相关，RLE 会退化
	}
	body, used, err := EncodeHybrid(img, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	pgm, _ := Encode(img, Options{Format: "pgm"})
	bmp, _ := Encode(img, Options{Format: "bmp"})
	rle, _ := Encode(img, Options{Format: "rle"})
	minS := len(pgm)
	if len(bmp) < minS {
		minS = len(bmp)
	}
	if len(rle) < minS {
		minS = len(rle)
	}
	if len(body) > minS {
		t.Fatalf("最坏情形 %d > 最小单一 %d", len(body), minS)
	}
	got, err := Decode(body, Options{Format: "hybrid"})
	if err != nil {
		t.Fatal(err)
	}
	if used != "hybrid" && !bytesEqual(got.Pix, img.Pix) {
		t.Fatalf("最坏情形回退 %s 解码不一致", used)
	}
	// GTH1 的最坏体积（9+⌈N/8⌉+128T）也必须小于 PGM，这是结构性下界。
	hyb := 9 + (100+7)/8 + 100*hybrid.Block*hybrid.Block/2
	if hyb >= len(pgm) {
		t.Fatalf("GTH1 最坏体积 %d 未严格小于 PGM %d", hyb, len(pgm))
	}
	t.Logf("160x160 随机图: used=%s body=%d pgm=%d bmp=%d rle=%d", used, len(body), len(pgm), len(bmp), len(rle))
}

// TestHybridLimits 混合编码的尺寸约束与错误路径。
func TestHybridLimits(t *testing.T) {
	img := &Image{W: 15, H: 16, Pix: make([]uint8, 15*16)}
	if _, err := Encode(img, Options{Format: "hybrid"}); err == nil {
		t.Fatal("非 16 倍数应报错")
	}
	if _, _, err := EncodeHybrid(&Image{W: 0, H: 16}, DefaultOptions()); err == nil {
		t.Fatal("零画幅应报错")
	}
	if _, _, err := EncodeHybrid(&Image{W: 16, H: 16, Pix: make([]uint8, 256)}, Options{Gain: 500}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Decode([]byte("GTH1"), Options{Format: "hybrid"}); err == nil {
		t.Fatal("截断应报错")
	}
	if _, err := Decode([]byte("XXXX"), Options{Format: "hybrid"}); err == nil {
		t.Fatal("未知魔数应报错")
	}
}

func newRNG(seed uint64) *rngT { return &rngT{s: seed} }

type rngT struct{ s uint64 }

func (r *rngT) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func bytesEqual(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
