package codec

import "testing"

func TestQuantizeIdentity(t *testing.T) {
	pix := []uint8{0, 1, 7, 64, 128, 200, 254, 255}
	for _, b := range []int{0, 256} {
		got := Quantize(4, 2, pix, b, 0.5)
		for i := range pix {
			if got[i] != pix[i] {
				t.Fatalf("buckets=%d 应恒等映射，got[%d]=%d want %d", b, i, got[i], pix[i])
			}
		}
	}
}

func TestQuantizeBuckets(t *testing.T) {
	// K=4：Δ=64，重建电平 {0,64,128,192,255 夹断}；无抖动时是朴素四舍五入。
	pix := make([]uint8, 256)
	for i := range pix {
		pix[i] = uint8(i)
	}
	got := Quantize(256, 1, pix, 4, 0)
	// 32 -> round(32/64)*64 = round(0.5)=1*64=64（Go round half away from zero）
	if got[32] != 64 {
		t.Fatalf("got[32]=%d, want 64", got[32])
	}
	// 100 -> round(1.5625)=2 -> 128
	if got[100] != 128 {
		t.Fatalf("got[100]=%d, want 128", got[100])
	}
	// 全部输出只能落在量化电平集合内
	levels := map[uint8]bool{0: true, 64: true, 128: true, 192: true, 255: true}
	for i, v := range got {
		if !levels[v] {
			t.Fatalf("像素 %d 输出 %d 不在量化电平集合内", i, v)
		}
	}
}

func TestQuantizeDitherChangesOutput(t *testing.T) {
	// 平滑斜坡上，全强度误差扩散应改变无抖动结果（误差被传播到邻域）。
	pix := make([]uint8, 64)
	for i := range pix {
		pix[i] = uint8(i * 4)
	}
	plain := Quantize(64, 1, pix, 4, 0)
	dithered := Quantize(64, 1, pix, 4, 1)
	same := true
	for i := range pix {
		if plain[i] != dithered[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("s=1 的误差扩散结果不应与 s=0 完全相同")
	}
}
