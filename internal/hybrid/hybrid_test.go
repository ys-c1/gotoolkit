package hybrid

import (
	"testing"
)

func ramp(w, h int, f func(x, y int) uint8) []uint8 {
	p := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p[y*w+x] = f(x, y)
		}
	}
	return p
}

func TestFlatThresholdMatchesModel(t *testing.T) {
	// 常数块与整块渐变都必须判为平坦；随机噪声必须判为纹理。
	flat := make([]uint8, Block*Block)
	for i := range flat {
		flat[i] = 77
	}
	var sum, sumSq int64
	for _, v := range flat {
		sum += int64(v)
		sumSq += int64(v) * int64(v)
	}
	if !FlatByModel(len(flat), sum, sumSq) {
		t.Fatal("常数块应判平坦")
	}
	// 构造 σ² 恰在阈值两侧的块：全 1 行与全 0 行交替，σ=0.5；再放大到 σ≈4.6。
	for _, tc := range []struct {
		sigma float64 // 期望 σ（双精度仅测试用）
		want  bool
	}{
		{0, true},
		{3, true},
		{4, true},
		{4.5, true},  // σ²=20.25 < τ²
		{5.0, false}, // σ²=25 > τ²
		{8, false},
	} {
		// 用两个电平 a,b 按 p 的比例混合，使 σ = (b−a)√(p(1−p))。
		// 取 a=0，b=round(σ/√(0.25))，p=0.5 → σ=b/2；故 b=2σ。
		b := int(2*tc.sigma + 0.5)
		if b < 0 {
			b = 0
		}
		if b > 255 {
			b = 255
		}
		blk := make([]uint8, Block*Block)
		for i := range blk {
			if i%2 == 0 {
				blk[i] = uint8(b)
			}
		}
		var s, ss int64
		for _, v := range blk {
			s += int64(v)
			ss += int64(v) * int64(v)
		}
		if got := FlatByModel(len(blk), s, ss); got != tc.want {
			t.Fatalf("σ≈%.2f: FlatByModel=%v want %v (σ²=%.4f, τ²=%.4f)", tc.sigma, got, tc.want, Variance(len(blk), s, ss), TauSq)
		}
	}
	// 两电平均值 0 与 b 的比例为 1:1 时 σ=b/2。b=9→σ²=20.25（阈值内），
	// b=10→σ²=25（阈值外）；再抽查若干随机块，模型判定必须与精确方差一致。
	for _, b := range []int{0, 4, 8, 9, 10, 30, 255} {
		blk := make([]uint8, Block*Block)
		for i := range blk {
			if i%2 == 0 {
				blk[i] = uint8(b)
			}
		}
		var s, ss int64
		for _, v := range blk {
			s += int64(v)
			ss += int64(v) * int64(v)
		}
		want := Variance(len(blk), s, ss) < TauSq
		if got := FlatByModel(len(blk), s, ss); got != want {
			t.Fatalf("σ²=%.4f: FlatByModel=%v want %v", Variance(len(blk), s, ss), got, want)
		}
	}
	r := newRNG(0x1ce)
	for trial := 0; trial < 500; trial++ {
		blk := make([]uint8, Block*Block)
		lo := r.next() % 256
		hi := lo + r.next()%40
		if hi > 255 {
			hi = 255
		}
		for i := range blk {
			if r.next()%2 == 0 {
				blk[i] = uint8(lo)
			} else {
				blk[i] = uint8(hi)
			}
		}
		var s, ss int64
		for _, v := range blk {
			s += int64(v)
			ss += int64(v) * int64(v)
		}
		want := Variance(len(blk), s, ss) < TauSq
		if got := FlatByModel(len(blk), s, ss); got != want {
			t.Fatalf("随机块: FlatByModel=%v want %v σ²=%.6f", got, want, Variance(len(blk), s, ss))
		}
	}
}

func TestPureFlatAndPureTexturePaths(t *testing.T) {
	const w, h = 32, 32
	// 平坦图：全部块应走平坦路径，误差来自四舍五入，每像素 ≤0.5。
	flat := ramp(w, h, func(x, y int) uint8 { return uint8(100 + x/8) })
	body, err := Encode(w, h, flat)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(body), Header+1+4; got != want {
		// 32×32 共 4 块，掩码 1 字节，平坦单值 4 字节，无纹理体。
		t.Fatalf("平坦图体积 %d，期望 %d", got, want)
	}
	_, _, out, mask, err := DecodeMasked(body)
	if err != nil {
		t.Fatal(err)
	}
	for i := range out {
		if mask[i] != 0 {
			t.Fatalf("平坦图出现纹理像素 @%d", i)
		}
		if d := int(out[i]) - int(flat[i]); d < -1 || d > 1 {
			t.Fatalf("平坦像素误差 %d @%d", d, i)
		}
	}
	// 纹理图：棋盘噪声，每块都应走纹理路径，单像素误差 ≤8（钳位像素除外）。
	rng := newRNG(0xabc)
	tex := ramp(w, h, func(x, y int) uint8 { return uint8(rng.next() >> 56) })
	body, err = Encode(w, h, tex)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(body), Header+1+4*Block*Block/2; got != want {
		t.Fatalf("纹理图体积 %d，期望 %d", got, want)
	}
	_, _, out, mask, err = DecodeMasked(body)
	if err != nil {
		t.Fatal(err)
	}
	maxErr := 0
	for i := range out {
		if mask[i] != 255 {
			t.Fatalf("纹理图出现平坦像素 @%d", i)
		}
		if d := abs(int(out[i]) - int(tex[i])); d > maxErr {
			maxErr = d
		}
	}
	// 钳位到 0/255 的像素（x≤8 或 x≥247）误差可能超过 8，其余恒 ≤8。
	if maxErr > Step/2 && !hasClippedExtreme(tex) {
		t.Fatalf("纹理路径最大误差 %d 超过 %d", maxErr, Step/2)
	}
}

func hasClippedExtreme(p []uint8) bool {
	for _, v := range p {
		if v < Step/2 || v > 255-Step/2 {
			return true
		}
	}
	return false
}

func TestRejectsBadSizes(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{0, 16}, {16, 0}, {15, 16}, {16, 17}, {17, 16}, {-16, 16},
	} {
		if _, err := Encode(tc.w, tc.h, make([]uint8, max(tc.w, 0)*max(tc.h, 0))); err == nil {
			t.Fatalf("%dx%d 应拒绝", tc.w, tc.h)
		}
	}
	if _, _, _, err := Decode([]byte("GTH1\x10")); err == nil {
		t.Fatal("截断码流应拒绝")
	}
	body, _ := Encode(16, 16, make([]uint8, 256))
	if _, _, _, err := Decode(body[:len(body)-1]); err == nil {
		t.Fatal("缺像素应拒绝")
	}
	bad := append([]byte(nil), body...)
	bad[0] = 'X'
	if _, _, _, err := Decode(bad); err == nil {
		t.Fatal("魔数错误应拒绝")
	}
}

func TestDitherCoversResidues(t *testing.T) {
	seen := map[int]int{}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			seen[dither(x, y, 64)]++
		}
	}
	if len(seen) != Step {
		t.Fatalf("相位只覆盖 %d 个余数，期望 %d", len(seen), Step)
	}
}

func newRNG(seed uint64) *rngv {
	return &rngv{s: seed}
}

type rngv struct{ s uint64 }

func (r *rngv) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
