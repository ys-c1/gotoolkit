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
