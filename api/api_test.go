package api

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gotoolkit/internal/quant"
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
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
	if _, err := Decode([]byte("x"), Options{Format: "pgm", Bits: 9}); err == nil {
		t.Fatal("重建位深越界应报错")
	}
	if _, err := Decode([]byte("x"), Options{Format: "pgm", Bits: 4, Recon: "lloyd2"}); err == nil {
		t.Fatal("未知重建方式应报错")
	}
}

func TestDecodeReconstructKeepMidpointPath(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		// Bits=0 保持原有逐字节回读，不受重建逻辑影响。
		plain, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain.Pix, img.Pix) {
			t.Fatalf("%s: Bits=0 不应改动像素", f)
		}
		// 中点路径显式可用，且不迭代。
		if _, err := Decode(body, Options{Format: f, Bits: 3, Recon: ReconMidpoint}); err != nil {
			t.Fatalf("%s: 中点重建失败: %v", f, err)
		}
		// 条件期望路径在同一画面上应得到更低的 MSE。
		h := quant.FromPixels(img.Pix)
		mid, err := quant.Midpoint(3)
		if err != nil {
			t.Fatal(err)
		}
		got, err := quant.Optimal(h, 3, quant.DefaultTol, quant.DefaultMaxIter)
		if err != nil {
			t.Fatal(err)
		}
		if !(got.MSE < quant.Distortion(h, mid.Levels)) {
			t.Fatalf("%s: 条件期望 MSE 未低于中点", f)
		}
	}
}

func TestDecodeReconNotConvergedPropagates(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decode(body, Options{
		Format: "pgm", Bits: 4, Recon: ReconExpectation,
		ReconTol: 1e-12, ReconMaxIter: 1,
	})
	if !errors.Is(err, quant.ErrNotConverged) {
		t.Fatalf("迭代超限应上抛 ErrNotConverged, got %v", err)
	}
}
