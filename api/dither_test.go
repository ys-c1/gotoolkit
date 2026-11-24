package api

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// requiredVarianceDrop 是本轮要求的间距方差降幅下限（60%）。
// 报告里两档强度实测为 72.10% 与 92.95%，都留有余量。
const requiredVarianceDrop = 0.6

// curvedGradient 与报告工具用同一条曲线：t^2.2 横向渐变，256×128。
func curvedGradient(w, h int) *Image {
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := float64(x) / float64(w-1)
			img.Pix[y*w+x] = uint8(255*math.Pow(t, 2.2) + 0.5)
		}
	}
	return img
}

func TestDitherKernelWeightTables(t *testing.T) {
	fs := FloydSteinbergKernel()
	wantFS := []float64{7, 3, 5, 1}
	if len(fs.Taps) != len(wantFS) {
		t.Fatalf("FS 项数 %d", len(fs.Taps))
	}
	for i, w := range wantFS {
		if fs.Taps[i].Weight != w {
			t.Fatalf("FS 第 %d 项原始权重 %v，应为 %v", i, fs.Taps[i].Weight, w)
		}
	}
	if fs.RawSum() != 16 {
		t.Fatalf("FS 原始权重和 %v", fs.RawSum())
	}
	eff, err := fs.EffectiveTaps()
	if err != nil {
		t.Fatal(err)
	}
	wantEff := []float64{7.0 / 16, 3.0 / 16, 5.0 / 16, 1.0 / 16}
	for i, w := range wantEff {
		if math.Abs(eff[i].Weight-w) > 1e-12 {
			t.Fatalf("FS 归一后第 %d 项 %v，应为 %v", i, eff[i].Weight, w)
		}
	}
	if s, err := fs.EffectiveSum(); err != nil || math.Abs(s-1) > 1e-12 {
		t.Fatalf("FS 归一后权重和 %v err=%v", s, err)
	}

	at := AtkinsonKernel()
	if len(at.Taps) != 6 || at.Normalize {
		t.Fatalf("Atkinson 应有 6 项且不归一")
	}
	if s, _ := at.EffectiveSum(); math.Abs(s-0.75) > 1e-12 {
		t.Fatalf("Atkinson 有效权重和 %v，应为 3/4", s)
	}
	if err := at.Validate(); err != nil {
		t.Fatalf("Atkinson 应带理由通过校验: %v", err)
	}

	table := fs.WeightTable()
	for _, needle := range []string{"floyd-steinberg", "0.437500", "0.187500", "0.312500", "0.062500", "effectiveSum=1.000000"} {
		if !bytes.Contains([]byte(table), []byte(needle)) {
			t.Fatalf("权重表缺 %q:\n%s", needle, table)
		}
	}
}

func TestDitherKernelRejects(t *testing.T) {
	cases := map[string]DitherKernel{
		"空核":      {},
		"权重和为零":   {Name: "zero", Taps: []DitherTap{{1, 0, 0}, {0, 1, 0}}, Normalize: true},
		"负权重":     {Name: "neg", Taps: []DitherTap{{1, 0, -1}, {0, 1, 2}}, Normalize: true},
		"后向偏移":    {Name: "back", Taps: []DitherTap{{-1, 0, 1}}, Normalize: true},
		"原点偏移":    {Name: "origin", Taps: []DitherTap{{0, 0, 1}}, Normalize: true},
		"重复偏移":    {Name: "dup", Taps: []DitherTap{{1, 0, 1}, {1, 0, 1}}, Normalize: true},
		"未归一没理由":  {Name: "raw", Taps: []DitherTap{{1, 0, 0.5}}, Normalize: false},
		"未归一和大于1": {Name: "big", Taps: []DitherTap{{1, 0, 1}, {0, 1, 1}}, Normalize: false, Note: "理由"},
		"NaN 权重":  {Name: "nan", Taps: []DitherTap{{1, 0, math.NaN()}}, Normalize: true},
	}
	for name, k := range cases {
		if err := k.Validate(); err == nil {
			t.Fatalf("%s 应被拒绝", name)
		}
	}
	zeroSum := DitherKernel{Name: "zero", Taps: []DitherTap{{1, 0, 0}, {0, 1, 0}}, Normalize: true}
	if err := zeroSum.Validate(); !errors.Is(err, ErrKernelSum) {
		t.Fatalf("权重和为零应返回 ErrKernelSum，实际 %v", err)
	}
}

func TestDitherRejectsNegativeStrengthAndZeroSum(t *testing.T) {
	img := curvedGradient(8, 4)
	if _, err := Dither(img, -0.1, FloydSteinbergKernel()); !errors.Is(err, ErrDitherStrength) {
		t.Fatalf("负强度应返回 ErrDitherStrength，实际 %v", err)
	}
	if _, err := Dither(img, math.NaN(), FloydSteinbergKernel()); !errors.Is(err, ErrDitherStrength) {
		t.Fatalf("NaN 强度应返回 ErrDitherStrength，实际 %v", err)
	}
	zero := DitherKernel{Name: "zero", Taps: []DitherTap{{1, 0, 0}, {0, 1, 0}}, Normalize: true}
	if _, err := Dither(img, 1, zero); !errors.Is(err, ErrKernelSum) {
		t.Fatalf("权重和为零应返回 ErrKernelSum，实际 %v", err)
	}
	// 通过公开入口 Options 也要拦下。
	if _, err := Encode(img, Options{Format: "pgm", DitherStrength: -1, DitherKernel: FloydSteinbergKernel()}); !errors.Is(err, ErrDitherStrength) {
		t.Fatalf("Options 负强度应报错，实际 %v", err)
	}
	if _, err := Encode(img, Options{Format: "pgm", DitherStrength: 1, DitherKernel: zero}); !errors.Is(err, ErrKernelSum) {
		t.Fatalf("Options 零和核应报错，实际 %v", err)
	}
	if _, err := Encode(img, Options{Format: "pgm", DitherStrength: 1}); !errors.Is(err, ErrDitherKernel) {
		t.Fatalf("强度为正但无核应报错，实际 %v", err)
	}
}

func TestDitherDisabledMatchesGolden(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 关闭抖动时与基准不逐字节一致", f)
		}
	}
}

func TestDitherSnakeDirectionAndSwitchPoints(t *testing.T) {
	for y := 0; y < 6; y++ {
		wantDir := 1
		if y%2 == 1 {
			wantDir = -1
		}
		if got := DitherDirection(y); got != wantDir {
			t.Fatalf("第 %d 行方向 %d，应为 %d", y, got, wantDir)
		}
		s, e := DitherRowStart(y, 5), DitherRowEnd(y, 5)
		if wantDir == 1 && (s != 0 || e != 4) {
			t.Fatalf("第 %d 行起止 %d..%d，应为 0..4", y, s, e)
		}
		if wantDir == -1 && (s != 4 || e != 0) {
			t.Fatalf("第 %d 行起止 %d..%d，应为 4..0", y, s, e)
		}
		// 下一行从上一行结束列开始，方向相反 —— 切换点可复算。
		ns, _ := DitherRowStart(y+1, 5), DitherRowEnd(y+1, 5)
		if ns != e {
			t.Fatalf("第 %d 行结束列 %d 与第 %d 行起始列 %d 不连续", y, e, y+1, ns)
		}
	}
}

// refDither 是测试内的独立参照实现：直接按“偶数行左→右、奇数行右→左，
// 反向行把 DX 取反”逐像素手算，不复用被测代码的任何函数。
// 用来核对蛇形推进、核镜像与归一化。
func refDither(src *Image, strength float64, k DitherKernel) *Image {
	step := 255.0 / float64(DitherLevelsDefault-1)
	pal := make([]float64, DitherLevelsDefault)
	for i := range pal {
		// 与实现一致：四舍五入到整数色阶。
		pal[i] = math.Round(float64(i) * step)
	}
	sum := 0.0
	for _, t := range k.Taps {
		sum += t.Weight
	}
	norm := 1.0
	if k.Normalize {
		norm = 1 / sum
	}
	buf := make([]float64, len(src.Pix))
	for i, v := range src.Pix {
		buf[i] = float64(v)
	}
	out := make([]uint8, len(src.Pix))
	for y := 0; y < src.H; y++ {
		xs := make([]int, src.W)
		for i := range xs {
			if y%2 == 0 {
				xs[i] = i
			} else {
				xs[i] = src.W - 1 - i
			}
		}
		for _, x := range xs {
			i := y*src.W + x
			v := buf[i]
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			best, bestD := 0, math.Abs(v-pal[0])
			for j := 1; j < len(pal); j++ {
				if d := math.Abs(v - pal[j]); d < bestD {
					best, bestD = j, d
				}
			}
			out[i] = uint8(pal[best])
			e := (v - pal[best]) * strength
			for _, t := range k.Taps {
				dx := t.DX
				if y%2 == 1 {
					dx = -dx
				}
				nx, ny := x+dx, y+t.DY
				if nx < 0 || nx >= src.W || ny < 0 || ny >= src.H {
					continue
				}
				buf[ny*src.W+nx] += e * t.Weight * norm
			}
		}
	}
	return &Image{W: src.W, H: src.H, Pix: out}
}

func TestDitherMatchesReference(t *testing.T) {
	src := &Image{W: 7, H: 5, Pix: make([]uint8, 35)}
	for i := range src.Pix {
		src.Pix[i] = uint8((i*37 + 11) % 256)
	}
	for _, k := range BuiltinDitherKernels() {
		for _, s := range []float64{0.5, 1} {
			got, err := Dither(src, s, k)
			if err != nil {
				t.Fatalf("%s 强度 %v: %v", k.Name, s, err)
			}
			want := refDither(src, s, k)
			if !bytes.Equal(got.Pix, want.Pix) {
				for i := range got.Pix {
					if got.Pix[i] != want.Pix[i] {
						t.Fatalf("%s 强度 %v: 像素 %d 为 %d，参照为 %d",
							k.Name, s, i, got.Pix[i], want.Pix[i])
					}
				}
			}
		}
	}
}

// TestDitherSerpentineMirror 单独验证反向行镜像：一个只向右扩散的核，
// 在第 1 行上应把误差扩散给左边邻居而不是右边。
func TestDitherSerpentineMirror(t *testing.T) {
	k := DitherKernel{Name: "right-only", Normalize: true, Taps: []DitherTap{{DX: 1, DY: 0, Weight: 1}}}
	// 第 1 行从右向左扫，首个被访问的是 x=3（值 200）。
	// 色阶表为 8 档、步长约 36.43：200→182，误差 +18。
	// 若方向正确（镜像后向左），x=2 的 17 收到 +18 变成 35，量化到 36；
	// 若没镜像，误差会从 x=4 流出画幅，x=2 仍是 0。
	img := &Image{W: 4, H: 2, Pix: []uint8{
		40, 40, 40, 40,
		0, 0, 17, 200,
	}}
	out, err := Dither(img, 1, k)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Pix[1*4+2]; got != 36 {
		t.Fatalf("反向行未把误差向左扩散，x=2 输出 %d（应为 36），整行 %v", got, out.Pix[4:])
	}
	if got := out.Pix[1*4+1]; got != 0 {
		t.Fatalf("误差继续向左时 x=1 应为 0，实际 %d，整行 %v", got, out.Pix[4:])
	}
}

func TestDitherSpacingVariance(t *testing.T) {
	src := curvedGradient(256, 128)
	base, err := QuantizeOnly(src, DitherLevelsDefault)
	if err != nil {
		t.Fatal(err)
	}
	baseStats, err := MeasureLevelSpacing(base)
	if err != nil {
		t.Fatal(err)
	}
	if baseStats.Samples == 0 || baseStats.Variance <= 0 {
		t.Fatalf("基线间距统计应非零: %+v", baseStats)
	}
	prev := baseStats.Variance
	for _, strength := range []float64{0.5, 1.0} {
		out, err := Dither(src, strength, FloydSteinbergKernel())
		if err != nil {
			t.Fatal(err)
		}
		st, err := MeasureLevelSpacing(out)
		if err != nil {
			t.Fatal(err)
		}
		drop, ok := SpacingVarianceDrop(baseStats, st)
		if !ok {
			t.Fatal("降幅应可定义")
		}
		if drop < requiredVarianceDrop {
			t.Fatalf("强度 %v 的间距方差降幅 %.1f%%，应至少 %.0f%%",
				strength, drop*100, requiredVarianceDrop*100)
		}
		if st.Variance >= prev {
			t.Fatalf("强度 %v 的间距方差 %.3f 未低于上一档 %.3f", strength, st.Variance, prev)
		}
		prev = st.Variance
	}
}

// TestDitherFormatsAgree 三种格式分别编码同一张抖动图，解码后逐像素一致；
// 关闭抖动时三种格式也都与基准逐字节一致。
func TestDitherFormatsAgree(t *testing.T) {
	src := curvedGradient(48, 12)
	ref := refDither(src, 1, FloydSteinbergKernel())
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := Encode(src, Options{Format: f, DitherStrength: 1, DitherKernel: FloydSteinbergKernel()})
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !bytes.Equal(got.Pix, ref.Pix) {
			t.Fatalf("%s 解码结果与参照不一致", f)
		}
	}
}

func TestDitherLevelsOption(t *testing.T) {
	src := curvedGradient(16, 4)
	if _, err := Encode(src, Options{Format: "pgm", DitherStrength: 1, DitherKernel: FloydSteinbergKernel(), DitherLevels: 1}); !errors.Is(err, ErrBadOption) {
		t.Fatalf("DitherLevels=1 应报错，实际 %v", err)
	}
	if _, err := Encode(src, Options{Format: "pgm", DitherStrength: 1, DitherKernel: FloydSteinbergKernel(), DitherLevels: 257}); !errors.Is(err, ErrBadOption) {
		t.Fatalf("DitherLevels=257 应报错，实际 %v", err)
	}
	// 缺省 0 与显式 8 必须逐字节一致，证明缺省值是公开的、不是藏着的行为。
	def, err := Encode(src, Options{Format: "pgm", DitherStrength: 1, DitherKernel: FloydSteinbergKernel()})
	if err != nil {
		t.Fatal(err)
	}
	exp, err := Encode(src, Options{Format: "pgm", DitherStrength: 1, DitherKernel: FloydSteinbergKernel(), DitherLevels: DitherLevelsDefault})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(def, exp) {
		t.Fatal("DitherLevels 缺省值应与显式 DitherLevelsDefault 一致")
	}
}

func TestDitherZeroStrengthEqualsQuantize(t *testing.T) {
	src := curvedGradient(64, 16)
	q, err := QuantizeOnly(src, DitherLevelsDefault)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Dither(src, 0, DitherKernel{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(q.Pix, d.Pix) {
		t.Fatal("强度 0 时 Dither 应与 QuantizeOnly 逐像素一致")
	}
}

func TestDitherDoesNotMutateInput(t *testing.T) {
	src := curvedGradient(32, 8)
	before := append([]uint8(nil), src.Pix...)
	if _, err := Dither(src, 1, FloydSteinbergKernel()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src.Pix, before) {
		t.Fatal("Dither 不应改动入参")
	}
}

func TestDecodeRejectsDitherStrength(t *testing.T) {
	body, err := Encode(sample(), Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(body, Options{Format: "pgm", DitherStrength: 0.5, DitherKernel: FloydSteinbergKernel()}); !errors.Is(err, ErrDitherStrength) {
		t.Fatalf("解码传抖动强度应报错，实际 %v", err)
	}
}

func TestDitherOnEdgeCases(t *testing.T) {
	pal := ditherPalette(DitherLevelsDefault)
	onPalette := func(v uint8) bool {
		for _, p := range pal {
			if p == v {
				return true
			}
		}
		return false
	}
	pure := map[string]*Image{
		"纯色块": {W: 8, H: 8, Pix: bytes.Repeat([]byte{99}, 64)},
		"全黑":  {W: 8, H: 8, Pix: make([]uint8, 64)},
		"全白":  {W: 8, H: 8, Pix: bytes.Repeat([]byte{255}, 64)},
	}
	for name, img := range pure {
		out, err := Dither(img, 1, FloydSteinbergKernel())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// 平坦色不是“不动”：中间灰度会被抖动成相邻两档交替的图案，
		// 这正是误差扩散的本意。这里只要求输出全部落在色阶表上，
		// 且不引入第三种取值，同时均值不漂移超过半档。
		seen := map[uint8]bool{}
		sum := 0
		for i := range out.Pix {
			if !onPalette(out.Pix[i]) {
				t.Fatalf("%s: 输出 %d 不在色阶表上", name, out.Pix[i])
			}
			seen[out.Pix[i]] = true
			sum += int(out.Pix[i])
		}
		if len(seen) > 2 {
			t.Fatalf("%s: 平坦色出现 %d 种取值 %v", name, len(seen), seen)
		}
		mean := float64(sum) / float64(len(out.Pix))
		if math.Abs(mean-float64(img.Pix[0])) > 0.5*(255.0/float64(DitherLevelsDefault-1)) {
			t.Fatalf("%s: 抖动后均值 %.2f 偏离原值 %d 超过半档", name, mean, img.Pix[0])
		}
		if onPalette(img.Pix[0]) {
			for i := range out.Pix {
				if out.Pix[i] != img.Pix[0] {
					t.Fatalf("%s: 原值 %d 已在色阶上，却被改成 %d", name, img.Pix[0], out.Pix[i])
				}
			}
		}
	}
	small := map[string]*Image{
		"单像素": {W: 1, H: 1, Pix: []uint8{128}},
		"单行":  {W: 5, H: 1, Pix: []uint8{0, 60, 120, 180, 255}},
		"单列":  {W: 1, H: 5, Pix: []uint8{0, 60, 120, 180, 255}},
	}
	for name, img := range small {
		out, err := Dither(img, 1, FloydSteinbergKernel())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.W != img.W || out.H != img.H || len(out.Pix) != len(img.Pix) {
			t.Fatalf("%s: 画幅变了", name)
		}
		for _, v := range out.Pix {
			if !onPalette(v) {
				t.Fatalf("%s: 输出 %d 不在色阶表上", name, v)
			}
		}
	}
	// 全黑、全白本身就在色阶上，抖动必须逐像素不动。
	for _, v := range []uint8{0, 255} {
		img := &Image{W: 6, H: 4, Pix: bytes.Repeat([]byte{v}, 24)}
		out, err := Dither(img, 1, FloydSteinbergKernel())
		if err != nil {
			t.Fatal(err)
		}
		for i := range out.Pix {
			if out.Pix[i] != v {
				t.Fatalf("全 %d 图被改成 %d", v, out.Pix[i])
			}
		}
	}
}

func TestNearestLevelIndexRoundTrip(t *testing.T) {
	pal := ditherPalette(DitherLevelsDefault)
	for v := 0; v <= 255; v++ {
		idx := nearestLevelIndex(pal, float64(v))
		if idx < 0 || idx >= len(pal) {
			t.Fatalf("v=%d 下标越界 %d", v, idx)
		}
		// 返回的色阶必须不比相邻色阶更远。
		for j := range pal {
			if math.Abs(float64(pal[j])-float64(v)) < math.Abs(float64(pal[idx])-float64(v)) {
				t.Fatalf("v=%d 选中 %d，但 %d 更近", v, pal[idx], pal[j])
			}
		}
	}
}
