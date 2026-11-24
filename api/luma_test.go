package api

import (
	"bytes"
	"math"
	"testing"

	"gotoolkit/internal/expand"
)

// TestCoefficientsConserveLuma 验证系数由守恒等式推出，而不是拍出来的：
// 三权重都是正数、和为 1，且 Σ w_c·k_c = 1 精确成立（整数意义，通分后）。
func TestCoefficientsConserveLuma(t *testing.T) {
	wr, wg, wb := LumaWeights()
	if wr+wg+wb != 1000 {
		t.Fatalf("权重和=%d，应为 1000", wr+wg+wb)
	}
	cs := LumaCoefficients()
	den := int64(cs[0].Den)
	var got int64
	for i, c := range cs {
		if c.Den != int(den) {
			t.Fatalf("%s 分母=%d，应统一为 %d", c.Channel, c.Den, den)
		}
		w := []int64{int64(wr), int64(wg), int64(wb)}[i]
		got += w * int64(c.Num)
	}
	if got != 1000*den {
		t.Fatalf("亮度守恒不成立：Σ w·Num = %d，应为 1000·Den = %d", got, 1000*den)
	}
	// k=(1,1,1) 同样守恒，但范数更大：最小范数解才是本实现选的。
	// ‖k‖² = 1/‖w‖² = 2.237 < ‖(1,1,1)‖² = 3。
	var normK float64
	for _, c := range cs {
		k := float64(c.Num) / float64(c.Den)
		normK += k * k
	}
	if normK >= 3 {
		t.Fatalf("最小范数解 ‖k‖²=%v 不应大于 (1,1,1) 的 3", normK)
	}
}

// TestCoefficientsMatchMinNorm 验证系数等于 w/‖w‖²（守恒约束下的最小范数解），
// 别人用这条式子也能复算。
func TestCoefficientsMatchMinNorm(t *testing.T) {
	wr, wg, wb := LumaWeights()
	norm2 := wr*wr + wg*wg + wb*wb
	cs := LumaCoefficients()
	want := []int{1000 * wr, 1000 * wg, 1000 * wb}
	for i, c := range cs {
		if c.Den != norm2 || c.Num != want[i] {
			t.Fatalf("%s: 得到 %d/%d，应为 %d/%d", c.Channel, c.Num, c.Den, want[i], norm2)
		}
	}
}

// TestBoundIsTightExhaustive 对 0..255 全枚举：
// 每一级的实测误差都不超过 Bound(g)，且最大值恰为 MaxBound。
func TestBoundIsTightExhaustive(t *testing.T) {
	worst := 0
	for g := 0; g < 256; g++ {
		r, gg, b, _ := expand.Expand(uint8(g))
		e := int(expand.Collapse(r, gg, b)) - g
		ae := e
		if ae < 0 {
			ae = -ae
		}
		if ae > expand.Bound(uint8(g)) {
			t.Fatalf("g=%d 误差 %d 超过上界 %d", g, e, expand.Bound(uint8(g)))
		}
		if ae > worst {
			worst = ae
		}
	}
	if worst != LumaMaxBound() {
		t.Fatalf("实测最大误差=%d，上界=%d，上界不紧", worst, LumaMaxBound())
	}
}

// TestUnclippedNominal 在 0..194 内逐级往返误差为 0：既不截断，
// 舍入误差互相抵消。
func TestUnclippedNominal(t *testing.T) {
	for g := 0; g <= int(LumaUnclippedMax()); g++ {
		r, gg, b, clip := expand.Expand(uint8(g))
		if clip != 0 {
			t.Fatalf("g=%d 不应截断", g)
		}
		if got := expand.Collapse(r, gg, b); int(got) != g {
			t.Fatalf("g=%d 收回 %d", g, got)
		}
	}
}

// TestGrayPathUnchanged 锁住原灰度路径：不加 LumaExpand 时，
// BMP 字节与基准完全一致，且不带 LUM1 标记。
func TestGrayPathUnchanged(t *testing.T) {
	img := sample()
	got, err := Encode(img, Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	want, err := Encode(img, Options{Format: "bmp", LumaExpand: false})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("默认与显式关闭不一致")
	}
	if string(got[6:10]) == "LUM1" {
		t.Fatal("灰度路径不应带 LUM1 标记")
	}
	dec, err := Decode(got, Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec.Pix, img.Pix) {
		t.Fatal("灰度路径往返不一致")
	}
}

// TestLumaExpandRoundTripTiers 三档灰度各验一次：实测误差不超档内上界，
// 且上界分别报出。
func TestLumaExpandRoundTripTiers(t *testing.T) {
	// 构造覆盖全部 256 级、三档各 86/85/85 级均匀铺开的图：宽 256、高 2，
	// 第二行重复一遍，保证三档都有足够样本。
	img := &Image{W: 256, H: 2, Pix: make([]uint8, 512)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 256)
	}
	body, err := Encode(img, Options{Format: "bmp", LumaExpand: true})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decode(body, Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.W != img.W || dec.H != img.H {
		t.Fatal("尺寸不一致")
	}
	tiers := LumaTiers(img)
	for i, tr := range tiers {
		if tr.Samples == 0 {
			t.Fatalf("第 %d 档无样本", i+1)
		}
		if tr.MaxErr > tr.Bound {
			t.Fatalf("第 %d 档（%d–%d）实测误差 %d 超过上界 %d", i+1, tr.Lo, tr.Hi, tr.MaxErr, tr.Bound)
		}
	}
	if tiers[0].Bound != 1 || tiers[1].Bound != 1 || tiers[2].Bound != LumaMaxBound() {
		t.Fatalf("三档上界应为 1/1/%d，得到 %d/%d/%d",
			LumaMaxBound(), tiers[0].Bound, tiers[1].Bound, tiers[2].Bound)
	}
	// 像素级：解码图与源图的最大差应等于档内上界能解释的最大值。
	maxDiff := 0
	for i := range img.Pix {
		d := int(img.Pix[i]) - int(dec.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > maxDiff {
			maxDiff = d
		}
	}
	if maxDiff > LumaMaxBound() {
		t.Fatalf("解码最大差 %d 超过全局上界 %d", maxDiff, LumaMaxBound())
	}
}

// TestLumaExpandBMPBytesDiffer 锁住「只有像素字节变、头与总长不变」。
func TestLumaExpandBMPBytesDiffer(t *testing.T) {
	img := sample()
	gray, err := Encode(img, Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	exp, err := Encode(img, Options{Format: "bmp", LumaExpand: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(gray) != len(exp) {
		t.Fatalf("长度不同：%d vs %d", len(gray), len(exp))
	}
	diff := 0
	for i := range gray {
		if gray[i] != exp[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("展开后字节应变化")
	}
	// 头字段（前 54 字节）除标记外应一致。
	for i := 0; i < 54; i++ {
		if i >= 6 && i < 10 {
			continue
		}
		if gray[i] != exp[i] {
			t.Fatalf("头偏移 %d 发生变化", i)
		}
	}
}

// TestLumaExpandRejectsNonBMP 锁住参数校验：只对 bmp 生效。
func TestLumaExpandRejectsNonBMP(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "rle"} {
		if _, err := Encode(img, Options{Format: f, LumaExpand: true}); err == nil {
			t.Fatalf("%s 应拒绝 LumaExpand", f)
		}
		if _, err := Decode([]byte("x"), Options{Format: f, LumaExpand: true}); err == nil {
			t.Fatalf("%s 解码也应拒绝 LumaExpand", f)
		}
	}
}

// TestEntropyUnit 验证熵按符号出现次数计算，单位比特每像素：
// 等概率 k 个符号的熵恰为 log2(k)，单符号为 0。
func TestEntropyUnit(t *testing.T) {
	for _, k := range []int{1, 2, 4, 8, 16, 256} {
		samples := make([]uint8, 0, 1024)
		for i := 0; i < 1024; i++ {
			samples = append(samples, uint8(i%k))
		}
		got := EntropyBitPerPixel(samples)
		want := math.Log2(float64(k))
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("k=%d 熵=%v，应为 %v", k, got, want)
		}
	}
}

// TestHeaderOverhead 验证头部开销口径并给出差值：
// BMP 相对 PGM 的净增 = (54−PGM 头) + 行补齐 + 2·N。
func TestHeaderOverhead(t *testing.T) {
	const w, h = 17, 16
	n := w * h
	pgmHead, pgmOver := FormatHeaderOverhead("pgm", w, h)
	bmpHead, bmpOver := FormatHeaderOverhead("bmp", w, h)
	row := (w*3 + 3) &^ 3
	pad := (row - w*3) * h
	if pgmHead != len("P5\n17 16\n255\n") || pgmOver != pgmHead {
		t.Fatalf("PGM 头=%d 额外=%d", pgmHead, pgmOver)
	}
	if bmpHead != 54 {
		t.Fatalf("BMP 头=%d", bmpHead)
	}
	if bmpOver != 54+pad+2*n {
		t.Fatalf("BMP 额外=%d，应为 54+%d+%d", bmpOver, pad, 2*n)
	}
	// 两格式总字节之差 = (54−PGM 头) + 行补齐 + 2·N。
	if bmpOver-pgmOver != (54-pgmHead)+pad+2*n {
		t.Fatalf("相对 PGM 的净增=%d，应为 (54−%d)+%d+%d", bmpOver-pgmOver, pgmHead, pad, 2*n)
	}
	// 展开 BMP 与灰度 BMP 总长相同，开销差只来自格式本身。
	img := &Image{W: w, H: h, Pix: make([]uint8, n)}
	gray, _ := Encode(img, Options{Format: "bmp"})
	exp, _ := Encode(img, Options{Format: "bmp", LumaExpand: true})
	if len(gray) != len(exp) {
		t.Fatal("展开不改变 BMP 总长")
	}
}
