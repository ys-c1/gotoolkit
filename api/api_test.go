package api

import (
	"bytes"
	"errors"
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

func gradient(w, h int) *Image {
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := x * 255 / (w - 1)
			if (x/2+y/2)%3 == 0 {
				v += 40
				if v > 255 {
					v = 255
				}
			}
			img.Pix[y*w+x] = uint8(v)
		}
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

// TestAllOffIsByteIdentical 是全关时“基准三份逐字节相同”的硬约束：
// 全关输出必须等于不带任何低位深参数时的输出，也必须等于 golden。
func TestAllOffIsByteIdentical(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		bare, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		off, err := Encode(img, Options{Format: f, Force: ForceOff, Bits: 4})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bare, off) {
			t.Fatalf("%s: ForceOff+Bits 与裸 Options 不逐字节一致", f)
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
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
}

func TestBitDepthErrorCode(t *testing.T) {
	img := sample()
	for _, bits := range []int{-1, 9, 16} {
		_, err := Encode(img, Options{Format: "pgm", Bits: bits, Force: ForceOn})
		if !errors.Is(err, ErrCodeBitDepth) {
			t.Fatalf("Bits=%d 应报 E_LOWBIT_BITS，得到 %v", bits, err)
		}
		if !errors.Is(err, ErrCodeBitDepth) {
			t.Fatalf("错误码必须可用 errors.Is 命中")
		}
	}
}

func TestZeroKernelErrorCode(t *testing.T) {
	img := sample()
	_, err := Encode(img, Options{
		Format: "pgm", Bits: 4, Force: ForceDither,
		Kernel: "custom:0,0,0;0,0,0;0,0,0",
	})
	if !errors.Is(err, ErrCodeKernelZero) {
		t.Fatalf("全零核应报 E_LOWBIT_KERNEL_SUM，得到 %v", err)
	}
	// 错误码互斥：不得同时命中位深错误。
	if errors.Is(err, ErrCodeBitDepth) {
		t.Fatal("核错误不应命中位深错误码")
	}
}

func TestSharedBitsChangesAllThree(t *testing.T) {
	// 同一个 Bits 字段：只开 quant、只开 dither、只开 palette，三者都必须
	// 依赖 Bits 而变化，证明“共享同一参数字段”。
	img := gradient(16, 8)
	base := func(force Force, bits int) []byte {
		b, err := Encode(img, Options{Format: "pgm", Force: force, Bits: bits})
		if err != nil {
			t.Fatalf("%s bits=%d: %v", force, bits, err)
		}
		return b
	}
	for _, f := range []Force{ForceQuant, ForceDither, ForcePalette, ForceOn} {
		if bytes.Equal(base(f, 4), base(f, 3)) {
			t.Fatalf("%s: bits=4 与 bits=3 输出相同，Bits 没起作用", f)
		}
	}
}

func TestPaletteOnlyStillQuantizes(t *testing.T) {
	// 仅开 palette 时，量化口径也必须跟着 Bits 变（三处共享字段的联动要求）。
	img := gradient(16, 8)
	a := mustEncode(t, img, Options{Format: "pgm", Force: ForcePalette, Bits: 4})
	b := mustEncode(t, img, Options{Format: "pgm", Force: ForcePalette, Bits: 2})
	if bytes.Equal(a, b) {
		t.Fatal("仅调色板开启时输出未随 Bits 变化")
	}
}

func TestLowbitStateListsIndices(t *testing.T) {
	img := gradient(8, 8)
	st, err := LowbitState(img, Options{Format: "pgm", Force: ForceOn, Bits: 2})
	if err != nil {
		t.Fatal(err)
	}
	if st == nil {
		t.Fatal("开启时应返回状态")
	}
	if len(st.IndicesBefore) != 64 {
		t.Fatalf("缩略前索引数 %d", len(st.IndicesBefore))
	}
	if st.ThumbW != 2 || st.ThumbH != 2 {
		t.Fatalf("缩略尺寸 %dx%d", st.ThumbW, st.ThumbH)
	}
	if len(st.IndicesAfter) != 4 {
		t.Fatalf("缩略后索引数 %d", len(st.IndicesAfter))
	}
	for i, ix := range st.IndicesAfter {
		if int(ix) >= len(st.PaletteAfter) {
			t.Fatalf("第 %d 个索引 %d 超出缩略后调色板", i, ix)
		}
	}
	if st.IndexBits != 2 {
		t.Fatalf("索引位宽 %d != Bits 2", st.IndexBits)
	}
	off, err := LowbitState(img, Options{Format: "pgm", Force: ForceOff, Bits: 2})
	if err != nil {
		t.Fatal(err)
	}
	if off != nil {
		t.Fatal("全关时应返回 nil 状态")
	}
}

func TestDitherConstructiveNormalized(t *testing.T) {
	// 抖动核归一到 1：只开抖动时，量化电平应与纯量化同口径。
	img := gradient(16, 16)
	plain := mustEncode(t, img, Options{Format: "pgm", Force: ForceQuant, Bits: 3})
	dith := mustEncode(t, img, Options{Format: "pgm", Force: ForceDither, Bits: 3})
	if bytes.Equal(plain, dith) {
		t.Fatal("抖动应改变输出")
	}
}

func mustEncode(t *testing.T, img *Image, opt Options) []byte {
	t.Helper()
	b, err := Encode(img, opt)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
