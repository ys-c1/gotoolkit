package codec

import (
	"errors"
	"testing"
)

// 基准字节：与 testdata/golden 同源的 4x3 图。
func goldenPGM() []byte {
	b, err := EncodePGM(4, 3, []uint8{0, 7, 14, 21, 28, 35, 42, 49, 56, 63, 70, 77})
	if err != nil {
		panic(err)
	}
	return b
}

func goldenBMP() []byte {
	b, err := EncodeBMP(4, 3, []uint8{0, 7, 14, 21, 28, 35, 42, 49, 56, 63, 70, 77})
	if err != nil {
		panic(err)
	}
	return b
}

func goldenRLE() []byte {
	b, err := EncodeRLE(4, 3, []uint8{0, 7, 14, 21, 28, 35, 42, 49, 56, 63, 70, 77})
	if err != nil {
		panic(err)
	}
	return b
}

func TestDecodeErrorOffsets(t *testing.T) {
	pgm := goldenPGM()
	bmp := goldenBMP()
	rle := goldenRLE()

	mut := func(b []byte, off int, v byte) []byte {
		c := make([]byte, len(b))
		copy(c, b)
		c[off] = v
		return c
	}

	cases := []struct {
		name    string
		decode  func([]byte) (int, int, []uint8, error)
		data    []byte
		code    int
		offset  int
		wantGot int // -1 = EOF
	}{
		{"pgm 魔数第0字节", DecodePGM, mut(pgm, 0, 'Q'), CodePGMMagicP, 0, int('Q')},
		{"pgm 魔数第1字节", DecodePGM, mut(pgm, 1, '6'), CodePGMMagic5, 1, int('6')},
		{"pgm maxval 末位", DecodePGM, mut(pgm, 9, '4'), CodePGMMaxVal, 9, int('4')},
		{"pgm 像素截断", DecodePGM, pgm[:len(pgm)-1], CodePGMPixShort, len(pgm) - 1, -1},
		{"pgm 宽为零", DecodePGM, mut(pgm, 3, '0'), CodePGMWidth, 3, int('0')},
		{"bmp 魔数第1字节", DecodeBMP, mut(bmp, 1, 'N'), CodeBMPMagic, 1, int('N')},
		{"bmp bpp 低字节", DecodeBMP, mut(bmp, 28, 0x20), CodeBMPBpp, 28, 0x20},
		{"bmp bpp 高字节", DecodeBMP, mut(bmp, 29, 0x01), CodeBMPBpp, 29, 0x01},
		{"bmp 像素截断", DecodeBMP, bmp[:len(bmp)-1], CodeBMPPixShort, len(bmp) - 1, -1},
		{"rle 魔数第2字节", DecodeRLE, mut(rle, 2, 'X'), CodeRLEMagic, 2, int('X')},
		{"rle 游程超限", DecodeRLE, mut(rle, 8, 13), CodeRLEOverrun, 8, 13},
		{"rle 奇数尾字节", DecodeRLE, append(append([]byte{}, rle...), 0xAB), CodeRLEOddTail, len(rle), 0xAB},
		{"rle 像素不足", DecodeRLE, rle[:len(rle)-2], CodeRLEPixShort, len(rle) - 2, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, _, err := c.decode(c.data)
			if err == nil {
				t.Fatal("应报错")
			}
			var de *DecodeError
			if !errors.As(err, &de) {
				t.Fatalf("错误类型应为 *DecodeError，实际 %T", err)
			}
			if de.Code != c.code {
				t.Fatalf("错误码: 期望 %d，实际 %d (%v)", c.code, de.Code, de)
			}
			if de.Offset != c.offset {
				t.Fatalf("偏移: 期望 %d，实际 %d (%v)", c.offset, de.Offset, de)
			}
			if de.Got != c.wantGot {
				t.Fatalf("实际字节: 期望 %d，实际 %d (%v)", c.wantGot, de.Got, de)
			}
		})
	}
}
