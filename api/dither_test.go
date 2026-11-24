package api

import (
	"bytes"
	"testing"
)

func TestDitherEntry(t *testing.T) {
	img := sample()
	for _, name := range []string{DitherBayer, DitherVoidCluster, DitherSpectral} {
		out, err := Dither(img, name)
		if err != nil {
			t.Fatalf("%s 抖动失败: %v", name, err)
		}
		if out.W != img.W || out.H != img.H || len(out.Pix) != len(img.Pix) {
			t.Fatalf("%s 画幅改变", name)
		}
		// 输入未被改写。
		if !bytes.Equal(img.Pix, sample().Pix) {
			t.Fatalf("%s 改写了输入图", name)
		}
		for _, v := range out.Pix {
			if v != 0 && v != 255 {
				t.Fatalf("%s 输出不是二值: %d", name, v)
			}
		}
	}
}

func TestDitherDeterministic(t *testing.T) {
	img := sample()
	a, _ := Dither(img, DitherSpectral)
	b, _ := Dither(img, DitherSpectral)
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("同输入两次抖动结果不一致")
	}
}

func TestDitherRejectsBadInput(t *testing.T) {
	if _, err := Dither(nil, DitherSpectral); err == nil {
		t.Fatal("nil 图应报错")
	}
	if _, err := Dither(&Image{W: 2, H: 2, Pix: make([]uint8, 3)}, DitherSpectral); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
	if _, err := Dither(sample(), "bogus"); err == nil {
		t.Fatal("非法矩阵名应报错")
	}
}

func TestDitherMatrixNames(t *testing.T) {
	if _, ok := DitherMatrix(DitherBayer); !ok {
		t.Fatal("bayer 矩阵应可取")
	}
	if _, ok := DitherMatrix("nope"); ok {
		t.Fatal("未知矩阵名 ok 应为 false")
	}
}

func TestEncodeWithDitherIsBinary(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "pgm", Dither: DitherSpectral})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range dec.Pix {
		if v != 0 && v != 255 {
			t.Fatalf("编码后不是二值: %d", v)
		}
	}
}

func TestDefaultOptionsNoDither(t *testing.T) {
	// 默认 Options 不带 Dither，编码结果必须与既有 golden 逐字节一致，
	// 即新增能力不改变原有行为。
	img := sample()
	body, err := Encode(img, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, plain) {
		t.Fatal("默认 Options 不应触发抖动或改变输出")
	}
}

func TestValidateDitherOption(t *testing.T) {
	for _, good := range []string{"", DitherOff, DitherBayer, DitherVoidCluster, DitherSpectral} {
		if err := (Options{Format: "pgm", Dither: good}).validate(); err != nil {
			t.Errorf("合法 Dither=%q 被拒: %v", good, err)
		}
	}
	if err := (Options{Format: "pgm", Dither: "weird"}).validate(); err == nil {
		t.Fatal("非法 Dither 应报错")
	}
}
