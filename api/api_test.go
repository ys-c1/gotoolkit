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

// maskSample 覆盖两档边界掩码值 0 与 255，以及中间灰度。
func maskSample() *Image {
	return &Image{W: 4, H: 3, Pix: []uint8{
		0, 255, 10, 0,
		255, 77, 0, 200,
		255, 33, 90, 128,
	}}
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f, Mask: -1})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f, Mask: -1})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s 尺寸不一致", f)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 像素不一致", f)
		}
		if got.Alpha != nil {
			t.Fatalf("%s 掩码关闭时 Alpha 应为 nil", f)
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
		// 显式 Mask:-1：掩码关闭时必须与参照实现的原编码逐字节一致。
		got, err := Encode(img, Options{Format: f, Mask: -1})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 与原编码不逐字节一致", f)
		}
	}
}

// TestMaskRoundTrip 两档掩码值（0 与 255）×三格式：
// 解码后原始灰度零误差还原，且透明像素位置与掩码判定完全一致。
func TestMaskRoundTrip(t *testing.T) {
	for _, mask := range []int{0, 255} {
		for _, f := range []string{"pgm", "bmp", "rle"} {
			img := maskSample()
			body, err := Encode(img, Options{Format: f, Mask: mask})
			if err != nil {
				t.Fatalf("%s/mask=%d 编码失败: %v", f, mask, err)
			}
			got, err := Decode(body, Options{Format: f, Mask: mask})
			if err != nil {
				t.Fatalf("%s/mask=%d 解码失败: %v", f, mask, err)
			}
			// 还原误差为零：像素逐一相等。
			if !bytes.Equal(got.Pix, img.Pix) {
				t.Fatalf("%s/mask=%d 像素未零误差还原", f, mask)
			}
			if len(got.Alpha) != len(img.Pix) {
				t.Fatalf("%s/mask=%d Alpha 长度不符", f, mask)
			}
			wantTransparent := 0
			for i, v := range img.Pix {
				wantAlpha := uint8(255)
				if v == uint8(mask) {
					wantAlpha = 0
					wantTransparent++
				}
				if got.Alpha[i] != wantAlpha {
					t.Fatalf("%s/mask=%d 像素 %d 的 Alpha=%d，期望 %d", f, mask, i, got.Alpha[i], wantAlpha)
				}
			}
			if wantTransparent == 0 {
				t.Fatalf("样本在 mask=%d 下应至少有一个透明像素", mask)
			}
		}
	}
}

// TestMaskOffByteIdentical 掩码功能取下时，输出字节流与未启用掩码的版本完全一样。
func TestMaskOffByteIdentical(t *testing.T) {
	img := maskSample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		plain, err := Encode(img, Options{Format: f, Mask: -1})
		if err != nil {
			t.Fatal(err)
		}
		// DefaultOptions 的掩码缺省即 -1。
		off, err := Encode(img, Options{Format: f, Mask: DefaultOptions().Mask})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain, off) {
			t.Fatalf("%s 掩码关闭后与原编码不逐字节一致", f)
		}
		// 关闭掩码也能解码带尾段的流，且拿到纯像素。
		masked, err := Encode(img, Options{Format: f, Mask: 0})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(masked, Options{Format: f, Mask: -1})
		if err != nil {
			t.Fatalf("%s 掩码关闭解码带尾段流失败: %v", f, err)
		}
		if !bytes.Equal(got.Pix, img.Pix) || got.Alpha != nil {
			t.Fatalf("%s 掩码关闭解码结果不符", f)
		}
	}
}

// TestMaskValidation 非法掩码值必须被拦下，并说明合法取值。
func TestMaskValidation(t *testing.T) {
	img := sample()
	for _, bad := range []int{-2, 256, 1000} {
		_, err := Encode(img, Options{Format: "pgm", Mask: bad})
		if !errors.Is(err, ErrBadOption) {
			t.Fatalf("Mask=%d 应报 ErrBadOption，实际 %v", bad, err)
		}
		_, err = Decode([]byte("P5\n1 1\n255\n0"), Options{Format: "pgm", Mask: bad})
		if !errors.Is(err, ErrBadOption) {
			t.Fatalf("解码 Mask=%d 应报 ErrBadOption，实际 %v", bad, err)
		}
	}
}

// TestMaskMismatch 流中记录的掩码值与请求值不一致时必须报错。
func TestMaskMismatch(t *testing.T) {
	img := maskSample()
	body, err := Encode(img, Options{Format: "pgm", Mask: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(body, Options{Format: "pgm", Mask: 255}); !errors.Is(err, ErrMaskMismatch) {
		t.Fatalf("掩码不一致应报 ErrMaskMismatch，实际 %v", err)
	}
	// 无尾段的流在掩码开启时也必须报错。
	plain, err := Encode(img, Options{Format: "pgm", Mask: -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(plain, Options{Format: "pgm", Mask: 0}); err == nil {
		t.Fatal("无尾段流在掩码开启时应报错")
	}
}

// TestMaskBeforeGain 掩码与增益同时开启时以掩码为准（先掩码后增益）：
// 透明位图只由原始像素决定，增益后命中掩码的位置保持不变；
// 在不触发增益削顶的前提下，像素仍可零误差还原（削顶是 gain 自身的既有语义）。
func TestMaskBeforeGain(t *testing.T) {
	img := &Image{W: 4, H: 1, Pix: []uint8{0, 10, 20, 30}}
	const mask = 0
	// 增益 10 不会把任何样本削到 0/255，往返可逆。
	body, err := Encode(img, Options{Format: "rle", Gain: 10, Mask: mask})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "rle", Gain: 10, Mask: mask})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Pix, img.Pix) {
		t.Fatal("掩码+增益往返后像素未零误差还原")
	}
	// 关键顺序断言：即便增益把 0 之外的值整体抬高，透明集合仍只由原始像素决定。
	for i, v := range img.Pix {
		wantAlpha := uint8(255)
		if v == mask {
			wantAlpha = 0
		}
		if got.Alpha[i] != wantAlpha {
			t.Fatalf("像素 %d 透明判定被增益改变", i)
		}
	}
}

func TestRejects(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "tga", Mask: -1}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500, Mask: -1}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm", Mask: -1}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm", Mask: -1}); err == nil {
		t.Fatal("空数据应报错")
	}
}
