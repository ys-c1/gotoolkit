package codec

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func samplePix(n int) []uint8 {
	pix := make([]uint8, n)
	for i := range pix {
		pix[i] = uint8(i * 7)
	}
	return pix
}

func TestPadForFormula(t *testing.T) {
	// p = (4 - off%4) % 4
	cases := []struct{ off, want int }{
		{0, 0}, {1, 3}, {2, 2}, {3, 1}, {4, 0},
		{54, 2}, {8, 0}, {12, 0}, {16, 0}, {17, 3},
	}
	for _, c := range cases {
		if got := padFor(c.off); got != c.want {
			t.Errorf("padFor(%d)=%d want %d", c.off, got, c.want)
		}
		if got := (c.off + padFor(c.off)) % AlignUnit; got != 0 {
			t.Errorf("off=%d: off+p mod %d = %d, want 0", c.off, AlignUnit, got)
		}
		if p := padFor(c.off); p < 0 || p >= AlignUnit {
			t.Errorf("padFor(%d)=%d not in [0,%d)", c.off, p, AlignUnit)
		}
	}
}

func TestPGMHeader(t *testing.T) {
	body, err := EncodePGM(2, 2, []uint8{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	// nominal "P5\n2 2\n255\n" = 11; aligned raster starts at 12 => one pad space.
	if !bytes.HasPrefix(body, []byte("P5\n 2 2\n255\n")) {
		t.Fatalf("头错误: %q", body[:12])
	}
	if off := pgmRasterOffset(body); off != 12 {
		t.Fatalf("像素起点=%d，应为 12", off)
	}
	w, h, pix, err := DecodePGM(body)
	if err != nil || w != 2 || h != 2 || len(pix) != 4 {
		t.Fatal("回读失败")
	}
	if want := []uint8{1, 2, 3, 4}; !bytes.Equal(pix, want) {
		t.Fatalf("像素=% x want % x", pix, want)
	}
}

// pgmRasterOffset 找出 P5 光栅起点：maxval 后的单个空白。
func pgmRasterOffset(b []byte) int {
	for i := 2; i+1 < len(b); i++ {
		if b[i] == '\n' && i+4 < len(b) && string(b[i+1:i+4]) == "255" && b[i+4] == '\n' {
			return i + 5
		}
	}
	return -1
}

func TestBMPPadding(t *testing.T) {
	body, err := EncodeBMP(3, 2, []uint8{1, 2, 3, 4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	off := int(binary.LittleEndian.Uint32(body[10:]))
	if off%AlignUnit != 0 {
		t.Fatalf("bfOffBits=%d 未对齐到 %d", off, AlignUnit)
	}
	if len(body) != off+((3*3+3)&^3)*2 {
		t.Fatalf("长度错误: %d (off=%d)", len(body), off)
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
