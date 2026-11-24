package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func sample() *Image {
	img := &Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	return img
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s 尺寸不一致", f)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 像素不一致", f)
		}
	}
}

func TestGoldenBytes(t *testing.T) {
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
			t.Fatalf("%s 与原编码不逐字节一致", f)
		}
	}
}

func TestRejects(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "tga"}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gamma: 300}); err == nil {
		t.Fatal("伽马目标越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
}

// 伽马为零：增益后均值已在边界，或目标就是 255，反解不出正系数。
func TestGammaZero(t *testing.T) {
	black := &Image{W: 2, H: 2, Pix: []uint8{0, 0, 0, 0}}
	white := &Image{W: 2, H: 2, Pix: []uint8{255, 255, 255, 255}}
	cases := []struct {
		img *Image
		opt Options
	}{
		{black, Options{Format: "pgm", Gamma: 128}},    // 均值 0
		{white, Options{Format: "pgm", Gamma: 128}},    // 均值 255
		{sample(), Options{Format: "pgm", Gamma: 255}}, // 目标 255 → 系数 0
	}
	for i, c := range cases {
		_, err := Encode(c.img, c.opt)
		if !errors.Is(err, ErrGammaZero) {
			t.Fatalf("case %d: 期望 ErrGammaZero，得到 %v", i, err)
		}
		if _, err := Decode(mustEncode(t, c.img, c.opt.Format), c.opt); !errors.Is(err, ErrGammaZero) {
			t.Fatalf("case %d 解码侧: 期望 ErrGammaZero，得到 %v", i, err)
		}
	}
}

// 增益不得让任何像素溢出：高光图在 +100% 下全部钳到 255，绝不回绕。
func TestGainNoOverflow(t *testing.T) {
	img := &Image{W: 4, H: 1, Pix: []uint8{0, 128, 200, 255}}
	got, err := Encode(img, Options{Format: "pgm", Gain: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint8{0, 255, 255, 255}
	back, err := Decode(got, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Pix, want) {
		t.Fatalf("增益溢出未钳位: %v", back.Pix)
	}
	// 负增益同样不越界。
	got, err = Encode(img, Options{Format: "pgm", Gain: -100})
	if err != nil {
		t.Fatal(err)
	}
	back, err = Decode(got, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Pix, []uint8{0, 0, 0, 0}) {
		t.Fatalf("负增益未钳位: %v", back.Pix)
	}
}

// 掩码超范围：长度与画幅不符。
func TestMaskRange(t *testing.T) {
	img := sample()
	opt := Options{Format: "pgm", Mask: make([]uint8, 5)}
	_, err := Encode(img, opt)
	if !errors.Is(err, ErrMaskRange) {
		t.Fatalf("期望 ErrMaskRange，得到 %v", err)
	}
	if _, err := Decode(mustEncode(t, img, "pgm"), opt); !errors.Is(err, ErrMaskRange) {
		t.Fatalf("解码侧期望 ErrMaskRange，得到 %v", err)
	}
}

func mustEncode(t *testing.T, img *Image, format string) []byte {
	t.Helper()
	b, err := Encode(img, Options{Format: format})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 与 api 内部实现分离的参照亮度链路，顺序写死为 增益→伽马→掩码。
// 增益用显式 float→trunc 复刻文档里的“向零截断”，不依赖 Go 的整数除法细节。
func referenceChain(img *Image, opt Options) []uint8 {
	out := append([]uint8(nil), img.Pix...)
	if opt.Gain != 0 {
		for i, v := range out {
			x := int(v) + int(math.Trunc(float64(v)*float64(opt.Gain)/100))
			out[i] = uint8(max(0, min(255, x)))
		}
	}
	if opt.Gamma != 0 {
		var sum float64
		for _, v := range out {
			sum += float64(v)
		}
		mean := sum / float64(len(out))
		if mean > 0 && mean < 255 && opt.Gamma != 255 {
			exp := math.Log(opt.Gamma/255) / math.Log(mean/255)
			for i, v := range out {
				out[i] = uint8(max(0, min(255, int(math.Round(255*math.Pow(float64(v)/255, exp))))))
			}
		}
	}
	if len(opt.Mask) != 0 {
		for i, v := range out {
			out[i] = uint8((int(v)*int(opt.Mask[i]) + 127) / 255)
		}
	}
	return out
}

// 十二组：三种格式 × 四种开关组合，逐一验证编码侧与解码侧，
// 并把四项指标（字节数、平均亮度、与参照最大差、端点像素数）写成 artifacts/metrics.md。
func TestTwelveGroups(t *testing.T) {
	img := sample()
	mask := []uint8{0, 16, 32, 48, 64, 80, 96, 112, 128, 160, 192, 255}
	combos := []struct {
		name string
		opt  Options
	}{
		{"gain", Options{Gain: 25}},
		{"gamma", Options{Gamma: 160}},
		{"mask", Options{Mask: mask}},
		{"gain+gamma+mask", Options{Gain: 25, Gamma: 160, Mask: mask}},
	}
	formats := []string{"pgm", "bmp", "rle"}

	var md bytes.Buffer
	md.WriteString("# 十二组指标（组 = 格式 × 开关组合）\n\n")
	md.WriteString("由 `go test ./api -run TestTwelveGroups -v` 生成。`maxdiff` 为编码侧回读像素与独立参照链路的逐像素最大差，`endpoints` 为结果中 0 或 255 的像素数。\n\n")
	md.WriteString("| 组 | 格式 | 开关 | 字节数 | 平均亮度 | maxdiff | endpoints |\n")
	md.WriteString("|---:|:---:|:---:|---:|---:|---:|---:|\n")

	rows := 0
	for _, f := range formats {
		for _, c := range combos {
			opt := c.opt
			opt.Format = f
			want := referenceChain(img, opt)

			encBody := mustEncode(t, img, f)
			enc, err := Encode(img, opt)
			if err != nil {
				t.Fatalf("%s/%s 编码: %v", f, c.name, err)
			}
			encBack, err := Decode(enc, Options{Format: f})
			if err != nil {
				t.Fatalf("%s/%s 编码后回读: %v", f, c.name, err)
			}
			if !bytes.Equal(encBack.Pix, want) {
				t.Fatalf("%s/%s 编码侧像素不符: got %v want %v", f, c.name, encBack.Pix, want)
			}

			dec, err := Decode(encBody, opt)
			if err != nil {
				t.Fatalf("%s/%s 解码: %v", f, c.name, err)
			}
			if !bytes.Equal(dec.Pix, want) {
				t.Fatalf("%s/%s 解码侧像素不符: got %v want %v", f, c.name, dec.Pix, want)
			}

			rows++
			md.WriteString(fmt.Sprintf("| %02d | %s | %s | %d | %.2f | %d | %d |\n",
				rows, f, c.name, len(enc), meanOf(encBack.Pix), maxDiff(encBack.Pix, want), endpoints(encBack.Pix)))
			t.Logf("组%02d %-3s %-15s bytes=%3d mean=%6.2f maxdiff=%d endpoints=%d",
				rows, f, c.name, len(enc), meanOf(encBack.Pix), maxDiff(encBack.Pix, want), endpoints(encBack.Pix))
		}
	}
	if rows != 12 {
		t.Fatalf("应验十二组，实际 %d 组", rows)
	}
	if err := os.MkdirAll(filepath.Join("..", "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("..", "artifacts", "metrics.md"), md.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDefaultMatchesGolden 默认参数必须与仓库参考字节逐字节一致。
// 这里要求基准存在：基准缺失时应当失败而不是跳过，否则“逐字节一致”无从证明。
func TestDefaultMatchesGolden(t *testing.T) {
	img := sample()
	cases := []struct {
		format string
		opt    Options
	}{
		{"pgm", DefaultOptions()},
		{"bmp", Options{Format: "bmp"}},
		{"rle", Options{Format: "rle"}},
		{"pgm", Options{Format: "pgm", Gain: 0, Gamma: 0, Mask: nil}}, // 显式零值与默认等价
	}
	for _, c := range cases {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", c.format+".bin"))
		if err != nil {
			t.Fatalf("缺 golden %s: %v", c.format, err)
		}
		got, err := Encode(img, c.opt)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 默认参数与基准不逐字节一致", c.format)
		}
	}
}

// 顺序对照：同一组参数按两种顺序执行，结果必须不同。
// 顺序 A 走真实编解码链路（Encode 后回读），顺序 B 由同文件的 orderMaskFirst 生成；
// 两次结果各存一份到 artifacts/order/ 供逐字节比较，并打印两行对照数据。
func TestOrderDifference(t *testing.T) {
	img := sample()
	opt := Options{Format: "pgm", Gain: 40, Gamma: 150, Mask: []uint8{0, 20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220}}

	enc := mustEncodeOpt(t, img, opt) // 真实链路：gain → gamma → mask
	orderA, err := Decode(enc, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	orderB := orderMaskFirst(img, opt) // 对照：mask → gamma → gain
	if !bytes.Equal(orderA.Pix, referenceChain(img, opt)) {
		t.Fatal("正式顺序与独立参照不一致")
	}

	dir := filepath.Join("..", "artifacts", "order")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bodyA := pgmBody(img.W, img.H, orderA.Pix)
	bodyB := pgmBody(img.W, img.H, orderB)
	if err := os.WriteFile(filepath.Join(dir, "order_gain_gamma_mask.pgm"), bodyA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "order_mask_gamma_gain.pgm"), bodyB, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Logf("顺序A gain→gamma→mask: mean=%6.2f endpoints=%2d sha256=%s", meanOf(orderA.Pix), endpoints(orderA.Pix), sha(bodyA))
	t.Logf("顺序B mask→gamma→gain: mean=%6.2f endpoints=%2d sha256=%s", meanOf(orderB), endpoints(orderB), sha(bodyB))
	if bytes.Equal(orderA.Pix, orderB) {
		t.Fatal("两种顺序结果相同，对照无效")
	}
}

func mustEncodeOpt(t *testing.T, img *Image, opt Options) []byte {
	t.Helper()
	b, err := Encode(img, opt)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// orderMaskFirst 仅用于反证顺序敏感性：先掩码，再伽马，最后增益。
func orderMaskFirst(img *Image, opt Options) []uint8 {
	out := append([]uint8(nil), img.Pix...)
	for i := range out {
		out[i] = uint8((int(out[i])*int(opt.Mask[i]) + 127) / 255)
	}
	var sum float64
	for _, v := range out {
		sum += float64(v)
	}
	mean := sum / float64(len(out))
	exp := math.Log(opt.Gamma/255) / math.Log(mean/255)
	for i := range out {
		out[i] = uint8(max(0, min(255, int(math.Round(255*math.Pow(float64(out[i])/255, exp))))))
	}
	for i, v := range out {
		out[i] = uint8(max(0, min(255, int(v)+int(math.Trunc(float64(v)*float64(opt.Gain)/100)))))
	}
	return out
}

func pgmBody(w, h int, pix []uint8) []byte {
	return append([]byte(fmt.Sprintf("P5\n%d %d\n255\n", w, h)), pix...)
}

func meanOf(p []uint8) float64 {
	var s float64
	for _, v := range p {
		s += float64(v)
	}
	return s / float64(len(p))
}

func maxDiff(a, b []uint8) int {
	m := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		m = max(m, d)
	}
	return m
}

func endpoints(p []uint8) int {
	n := 0
	for _, v := range p {
		if v == 0 || v == 255 {
			n++
		}
	}
	return n
}
