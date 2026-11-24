package codec

import "testing"

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

func TestRLEChunkedRoundTrip(t *testing.T) {
	pix := []uint8{9, 9, 9, 7, 7, 7, 7, 3, 3, 1}
	body, err := EncodeRLEChunked(5, 2, pix, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(body[:4]) != "GTR2" {
		t.Fatalf("魔数错误: %q", body[:4])
	}
	// 10 字节头 + 3 块 × (2 字节块头) + 游程 (9,9,9,7)(7,7,7,3)(3,1) → 6 对
	if len(body) != 10+3*2+6*2 {
		t.Fatalf("分块长度错误: %d", len(body))
	}
	w, h, got, err := DecodeRLE(body)
	if err != nil || w != 5 || h != 2 {
		t.Fatalf("回读失败: %v", err)
	}
	for i, v := range pix {
		if got[i] != v {
			t.Fatalf("像素 %d 不一致", i)
		}
	}
}

func TestRLEChunkZeroMatchesGTR1(t *testing.T) {
	pix := []uint8{5, 5, 6, 6, 6, 2}
	a, err := EncodeRLEChunked(3, 2, pix, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodeRLE(3, 2, pix)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("chunk=0 应与 GTR1 逐字节一致")
	}
	if _, _, _, err := DecodeRLE(a); err != nil {
		t.Fatal("GTR1 回读失败")
	}
}
