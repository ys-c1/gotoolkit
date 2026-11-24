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

func TestBLKRoundTrip(t *testing.T) {
	// 4×4 图，L=2，共 4 块；每块均值四舍五入后整块回填。
	pix := []uint8{
		10, 20, 100, 110,
		30, 40, 120, 130,
		50, 60, 200, 210,
		70, 80, 220, 230,
	}
	body, err := EncodeBLK(4, 4, 2, pix)
	if err != nil {
		t.Fatal(err)
	}
	if string(body[0:4]) != "GTB1" || body[8] != 2 {
		t.Fatalf("头错误: % x", body[:9])
	}
	// 四块均值：25, 115, 65, 215。
	wantMeans := []byte{25, 115, 65, 215}
	if string(body[9:]) != string(wantMeans) {
		t.Fatalf("块均值错误: %v", body[9:])
	}
	w, h, L, got, err := DecodeBLK(body)
	if err != nil {
		t.Fatal(err)
	}
	if w != 4 || h != 4 || L != 2 || len(got) != 16 {
		t.Fatalf("回读画幅错误: %dx%d L=%d n=%d", w, h, L, len(got))
	}
	want := []uint8{
		25, 25, 115, 115,
		25, 25, 115, 115,
		65, 65, 215, 215,
		65, 65, 215, 215,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("像素 %d: got %d want %d", i, got[i], want[i])
		}
	}
}

func TestBLKLenOneIsLossless(t *testing.T) {
	pix := []uint8{1, 2, 3, 4, 5, 6}
	body, err := EncodeBLK(3, 2, 1, pix)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, got, err := DecodeBLK(body)
	if err != nil {
		t.Fatal(err)
	}
	for i := range pix {
		if got[i] != pix[i] {
			t.Fatalf("L=1 应无损，像素 %d: %d != %d", i, got[i], pix[i])
		}
	}
}

func TestBLKRejectsNonDivisible(t *testing.T) {
	if _, err := EncodeBLK(5, 4, 2, make([]uint8, 20)); err == nil {
		t.Fatal("块长不整除宽时应报错")
	}
	if _, err := EncodeBLK(4, 5, 2, make([]uint8, 20)); err == nil {
		t.Fatal("块长不整除高时应报错")
	}
	body := []byte("GTB1")
	body = append(body, 5, 0, 4, 0, 2) // w=5,h=4,L=2
	body = append(body, make([]byte, 10)...)
	if _, _, _, _, err := DecodeBLK(body); err == nil {
		t.Fatal("解码端也应拒绝不整除画幅")
	}
}

func TestBLKDefaultBlockLen(t *testing.T) {
	if DefaultBlockLen != 8 {
		t.Fatalf("默认块长应为 8，got %d", DefaultBlockLen)
	}
}
