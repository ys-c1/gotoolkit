package codec

import (
	"bytes"
	"testing"
)

func TestPGMHeader(t *testing.T) {
	body, err := EncodePGM(2, 2, []uint8{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if string(body[:11]) != "P5\n2 2\n255\n" {
		t.Fatalf("头错误: %q", body[:11])
	}
	w, h, pix, err := DecodePGM(body)
	if err != nil || w != 2 || h != 2 || len(pix) != 4 {
		t.Fatal("回读失败")
	}
}

func TestBMPPadding(t *testing.T) {
	body, err := EncodeBMP(3, 2, []uint8{1, 2, 3, 4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 54+((3*3+3)&^3)*2 {
		t.Fatalf("长度错误: %d", len(body))
	}
}

func TestRLERuns(t *testing.T) {
	body, err := EncodeRLE(4, 1, []uint8{9, 9, 9, 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 8+4 {
		t.Fatalf("游程数错误: %d", len(body))
	}
}

// 掩码尾段：三格式写入后都能原样读回，且原载荷（无尾段部分）一字不差。
func TestMaskTrailerRoundTrip(t *testing.T) {
	const w, h = 5, 3
	pix := []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	alpha := make([]uint8, len(pix))
	for i := range alpha {
		alpha[i] = 255
	}
	alpha[2], alpha[7], alpha[14] = 0, 0, 0
	vals := []uint8{2, 7, 14}

	plain := func(f string) []byte {
		var b []byte
		var err error
		switch f {
		case "pgm":
			b, err = EncodePGM(w, h, pix)
		case "bmp":
			b, err = EncodeBMP(w, h, pix)
		default:
			b, err = EncodeRLE(w, h, pix)
		}
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	for _, f := range []string{"pgm", "bmp", "rle"} {
		var body []byte
		var gw, gh int
		var gotVals, gpix, galpha []uint8
		var err error
		switch f {
		case "pgm":
			body, err = EncodePGMMasked(w, h, pix, alpha, vals)
			if err != nil {
				t.Fatal(err)
			}
			gw, gh, gotVals, gpix, galpha, err = DecodePGMMasked(body)
		case "bmp":
			body, err = EncodeBMPMasked(w, h, pix, alpha, vals)
			if err != nil {
				t.Fatal(err)
			}
			gw, gh, gotVals, gpix, galpha, err = DecodeBMPMasked(body)
		default:
			body, err = EncodeRLEMasked(w, h, pix, alpha, vals)
			if err != nil {
				t.Fatal(err)
			}
			gw, gh, gotVals, gpix, galpha, err = DecodeRLEMasked(body)
		}
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if gw != w || gh != h {
			t.Fatalf("%s 尺寸不符: %dx%d", f, gw, gh)
		}
		if !bytes.Equal(gpix, pix) {
			t.Fatalf("%s 像素未原样还原", f)
		}
		if !bytes.Equal(galpha, alpha) {
			t.Fatalf("%s alpha 未还原", f)
		}
		if !bytes.Equal(gotVals, vals) {
			t.Fatalf("%s 掩码值未还原: %v", f, gotVals)
		}
		// 尾段之前的载荷必须与不带掩码的编码逐字节相同。
		p := plain(f)
		if !bytes.Equal(body[:len(p)], p) {
			t.Fatalf("%s 掩码改变了原始载荷", f)
		}
		if len(body) <= len(p) {
			t.Fatalf("%s 掩码后没有追加尾段", f)
		}
		// 无尾段时掩码解码必须报 ErrNoMask，而不是静默成功。
		if _, _, _, _, _, err := decodeMasked(f, p); err != ErrNoMask {
			t.Fatalf("%s 无尾段应报 ErrNoMask，实际 %v", f, err)
		}
	}
}

func decodeMasked(f string, data []byte) (int, int, []uint8, []uint8, []uint8, error) {
	switch f {
	case "pgm":
		return DecodePGMMasked(data)
	case "bmp":
		return DecodeBMPMasked(data)
	default:
		return DecodeRLEMasked(data)
	}
}

// 掩码值必须严格升序；重复或乱序都要拒绝。
func TestMaskTrailerRejects(t *testing.T) {
	const w, h = 4, 1
	pix := []uint8{0, 1, 2, 3}
	alpha := []uint8{0, 0, 255, 255}
	if _, err := EncodePGMMasked(w, h, pix, alpha, []uint8{0, 1}); err != nil {
		t.Fatal(err)
	}
	dup := buildMaskTrailer([]uint8{0, 0}, alpha) // 重复
	if _, _, err := parseMaskTrailer(dup, 0, w*h); err == nil {
		t.Fatal("重复掩码值应被拒绝")
	}
	desc := buildMaskTrailer([]uint8{2, 1}, alpha) // 乱序
	if _, _, err := parseMaskTrailer(desc, 0, w*h); err == nil {
		t.Fatal("乱序掩码值应被拒绝")
	}
	// 位图长度与画幅不符。
	short := buildMaskTrailer([]uint8{0}, alpha)
	if _, _, err := parseMaskTrailer(short, 0, w*h+100); err == nil {
		t.Fatal("位图长度不符应被拒绝")
	}
	// 填充位非零：nPix=3 时最后一字节的低 5 位必须为 0。
	alpha3 := []uint8{0, 0, 0}
	trail := buildMaskTrailer([]uint8{0}, alpha3)
	trail[len(trail)-1] |= 0x01 // 置一个填充位
	if _, _, err := parseMaskTrailer(trail, 0, 3); err == nil {
		t.Fatal("填充位非零应被拒绝")
	}
	// 掩码值个数为 0 是非法尾段。
	zero := append([]byte{}, maskMagic...)
	zero = append(zero, 0, 0, 0, 0, 1, 0x80)
	if _, _, err := parseMaskTrailer(zero, 0, 4); err == nil {
		t.Fatal("掩码值个数为 0 应被拒绝")
	}
}
