package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
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
			t.Fatalf("%s 与基准不逐字节一致", f)
		}
	}
}

// TestPGMDecodeZeroCopy 固定 PGM 的零拷贝契约：对齐后像素起点直接切片返回。
func TestPGMDecodeZeroCopy(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	// 在 body 里定位像素起点：最后一个 "255\n" 之后。
	idx := bytes.LastIndex(body, []byte("255\n")) + 4
	if &got.Pix[0] != &body[idx] {
		t.Fatalf("PGM 解码应零拷贝：Pix 起点 %p，body 光栅 %p", &got.Pix[0], &body[idx])
	}
	// 入参改动应同步可见（证明确实共享，而不是碰巧相等）。
	body[idx] = 0x5A
	if got.Pix[0] != 0x5A {
		t.Fatal("PGM Pix 与入参不共享底层数组")
	}
}

func TestDecodeLatencyPerFormat(t *testing.T) {
	// 每种格式跑 N 次取平均，作为「各一次」的读数来源。
	const w, h = 512, 512
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for i := range img.Pix {
		img.Pix[i] = uint8((i*37 + 11) & 0xff)
	}
	const iters = 200
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		for i := 0; i < iters; i++ {
			if _, err := Decode(body, Options{Format: f}); err != nil {
				t.Fatal(err)
			}
		}
		elapsed := time.Since(start)
		t.Logf("%s decode: %d iters, avg %.3f ms, %.1f MB/s", f, iters,
			float64(elapsed.Microseconds())/1000/float64(iters),
			float64(len(img.Pix)*iters)/(elapsed.Seconds()*1e6))
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
