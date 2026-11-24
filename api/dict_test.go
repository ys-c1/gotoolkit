package api

import (
	"bytes"
	"math"
	"testing"
)

func TestDictRoundTrip(t *testing.T) {
	// 含 0xFF 真实差分、孤立点、全均匀等多种情形。
	pix := []byte{0, 1, 2, 3, 255, 255, 255, 7, 0, 0, 200, 55, 55, 55, 90, 1}
	img := &Image{W: 8, H: 2, Pix: pix}
	body, ov, err := EncodeDictWithStats(img)
	if err != nil {
		t.Fatal(err)
	}
	// 开销对账。
	if got := ov.HeaderBytes + ov.DictBytes + ov.BitstreamBytes; got != ov.TotalBytes {
		t.Fatalf("开销对账失败: %d != %d", got, ov.TotalBytes)
	}
	if ov.HeaderBytes+ov.DictBytes+ov.BitstreamBytes != len(body) {
		t.Fatalf("统计体积与实际不一致")
	}
	if ov.PayloadBits != ov.DictCodeBits+ov.EscapeBits {
		t.Fatalf("载荷 bit 拆分不平: %d != %d+%d",
			ov.PayloadBits, ov.DictCodeBits, ov.EscapeBits)
	}
	got, err := Decode(body, Options{Format: "dict"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Pix, pix) {
		t.Fatalf("dict 往返像素不一致\n got %v\nwant %v", got.Pix, pix)
	}
}

func TestEntropy(t *testing.T) {
	// 两个等概率符号 -> 1 bit/px，2 种符号。
	h, distinct, err := Entropy([]byte{0, 255, 0, 255})
	if err != nil {
		t.Fatal(err)
	}
	if distinct != 2 || math.Abs(h-1.0) > 1e-12 {
		t.Fatalf("等概率两符号熵错误: h=%f d=%d", h, distinct)
	}
	// 常量图 -> 0 bit/px，1 种符号。
	h, distinct, _ = Entropy(make([]byte, 100))
	if distinct != 1 || h != 0 {
		t.Fatalf("常量图熵错误: h=%f d=%d", h, distinct)
	}
	if _, _, err := Entropy(nil); err == nil {
		t.Fatal("空图应报错")
	}
}

func TestDictRejectsTruncated(t *testing.T) {
	img := &Image{W: 4, H: 1, Pix: []byte{1, 2, 3, 4}}
	body, _, err := EncodeDictWithStats(img)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(body[:len(body)-2], Options{Format: "dict"}); err == nil {
		t.Fatal("截断流应被拒绝")
	}
	if _, err := Decode(append([]byte("XXXX"), body[4:]...), Options{Format: "dict"}); err == nil {
		t.Fatal("魔数错误应被拒绝")
	}
}

func TestDictFormatInOptions(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "dict"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "dict"})
	if err != nil || !bytes.Equal(got.Pix, img.Pix) {
		t.Fatal("Options dict 往返失败")
	}
}
