package codec

import (
	"bytes"
	"testing"
)

// TestMaskedRoundTrip 三格式带掩码尾段的编解码往返：像素与 alpha 都精确还原。
func TestMaskedRoundTrip(t *testing.T) {
	w, h := 4, 3
	pix := []uint8{0, 255, 10, 0, 255, 77, 0, 200, 255, 33, 90, 128}
	alpha := make([]uint8, len(pix))
	for i, v := range pix {
		if v == 0 {
			alpha[i] = 0
		} else {
			alpha[i] = 255
		}
	}
	cases := []struct {
		name   string
		encode func() ([]byte, error)
		decode func([]byte) (int, int, uint8, []uint8, []uint8, error)
	}{
		{"pgm", func() ([]byte, error) { return EncodePGMMasked(w, h, pix, alpha, 0) }, DecodePGMMasked},
		{"bmp", func() ([]byte, error) { return EncodeBMPMasked(w, h, pix, alpha, 0) }, DecodeBMPMasked},
		{"rle", func() ([]byte, error) { return EncodeRLEMasked(w, h, pix, alpha, 0) }, DecodeRLEMasked},
	}
	for _, c := range cases {
		body, err := c.encode()
		if err != nil {
			t.Fatalf("%s 编码失败: %v", c.name, err)
		}
		gw, gh, stored, gpix, galpha, err := c.decode(body)
		if err != nil {
			t.Fatalf("%s 解码失败: %v", c.name, err)
		}
		if gw != w || gh != h || stored != 0 {
			t.Fatalf("%s 头信息不符", c.name)
		}
		if !bytes.Equal(gpix, pix) {
			t.Fatalf("%s 像素未零误差还原", c.name)
		}
		if !bytes.Equal(galpha, alpha) {
			t.Fatalf("%s alpha 未还原", c.name)
		}
	}
}

// TestMaskTrailerBitPacking 位图按高位在前打包，非 8 倍数时填充位必须为 0。
func TestMaskTrailerBitPacking(t *testing.T) {
	alpha := []uint8{0, 255, 255, 0, 255, 255, 255, 255, 0} // 9 像素 -> 2 字节
	tr := buildMaskTrailer(42, alpha)
	if string(tr[:4]) != "GTMK" || tr[4] != 42 {
		t.Fatal("尾段头错误")
	}
	if tr[5] != 2 || tr[6] != 0 {
		t.Fatalf("位图长度错误: %d", tr[5])
	}
	bits := tr[7:]
	if bits[0] != 0b10010000 || bits[1] != 0b10000000 {
		t.Fatalf("位图打包错误: %08b %08b", bits[0], bits[1])
	}
	got, a, err := parseMaskTrailer(append([]byte("payload"), tr...), 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 || !bytes.Equal(a, alpha) {
		t.Fatal("尾段解析错误")
	}
}

// TestDecodeRLEStopsAtMask 掩码关闭时基础 RLE 解码在像素凑满后应正常跳过 GTMK 尾段。
func TestDecodeRLEStopsAtMask(t *testing.T) {
	pix := []uint8{9, 9, 9, 7}
	alpha := []uint8{255, 255, 255, 0}
	body, err := EncodeRLEMasked(4, 1, pix, alpha, 7)
	if err != nil {
		t.Fatal(err)
	}
	w, h, got, err := DecodeRLE(body)
	if err != nil {
		t.Fatal(err)
	}
	if w != 4 || h != 1 || !bytes.Equal(got, pix) {
		t.Fatal("带尾段 RLE 的无掩码解码失败")
	}
}

// TestRLERunBytesMimicMagic 回归：合法游程的 (计数,值) 字节恰好拼出 "GTMK"，
// 基础解码不得把像素数据误判成尾段魔数。
func TestRLERunBytesMimicMagic(t *testing.T) {
	// 游程 1：71('G') 个 84('T')；游程 2：77('M') 个 75('K') -> 字节 47 54 4d 4b = "GTMK"。
	pix := make([]uint8, 0, 148)
	for k := 0; k < 71; k++ {
		pix = append(pix, 84)
	}
	for k := 0; k < 77; k++ {
		pix = append(pix, 75)
	}
	plain, err := EncodeRLE(148, 1, pix)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, got, err := DecodeRLE(plain); err != nil || !bytes.Equal(got, pix) {
		t.Fatalf("含 GTMK 字节的合法游程被误判: err=%v", err)
	}
	// 同图再加真尾段：掩码路径必须区分「像素里的 GTMK」与真尾段。
	alpha := make([]uint8, 148)
	for i, v := range pix {
		if v == 84 {
			alpha[i] = 0
		} else {
			alpha[i] = 255
		}
	}
	masked, err := EncodeRLEMasked(148, 1, pix, alpha, 84)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, stored, got, gotAlpha, err := DecodeRLEMasked(masked); err != nil || stored != 84 ||
		!bytes.Equal(got, pix) || !bytes.Equal(gotAlpha, alpha) {
		t.Fatalf("带尾段且含 GTMK 字节的流解码失败: err=%v", err)
	}
}
