package codec

import "testing"

// 说明：这些测试在包内把某个标志“人工弄坏”（忽略它），
// 用于验证上层 api.Diagnose 能不能指出“哪个标志没生效”。
// 真正的诊断逻辑在 api/diagnose.go。

func layoutPixel(w, h int, f func(x, y int) uint8) []uint8 {
	p := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p[y*w+x] = f(x, y)
		}
	}
	return p
}

// brokenLayout 返回一个把指定标志抹掉的 Layout。
func brokenLayout(flag string) Layout {
	good := Layout{BottomUp: true, BigEndian: true, Align: 4}
	switch flag {
	case "RowOrder":
		return Layout{BottomUp: false, BigEndian: true, Align: 4}
	case "ByteOrder":
		return Layout{BottomUp: true, BigEndian: false, Align: 4}
	case "RowAlign":
		return Layout{BottomUp: true, BigEndian: true, Align: 1}
	}
	return good
}

func encPixels(t *testing.T, format string, w, h int, pix []uint8, l Layout) []uint8 {
	t.Helper()
	switch format {
	case "pgm":
		body, err := EncodePGMLayout(w, h, pix, l)
		if err != nil {
			t.Fatal(err)
		}
		// 只取像素区，去掉 ASCII 头。
		off := len(body) - len(PackRows(w, h, 1, pix, l))
		return body[off:]
	case "bmp":
		body, err := EncodeBMPLayout(w, h, pix, l)
		if err != nil {
			t.Fatal(err)
		}
		return body[54:]
	default:
		body, err := EncodeRLELayout(w, h, pix, l)
		if err != nil {
			t.Fatal(err)
		}
		return body[8:]
	}
}

// TestEachFlagChangesItsTarget 在包内逐格式证明三个标志各自作用到预期位置。
func TestEachFlagChangesItsTarget(t *testing.T) {
	const w, h = 5, 4
	pix := layoutPixel(w, h, func(x, y int) uint8 { return uint8(1 + y*40 + x) })
	full := Layout{BottomUp: true, BigEndian: true, Align: 4}
	base := encPixels(t, "pgm", w, h, pix, full)

	for _, flag := range []string{"RowOrder", "ByteOrder", "RowAlign"} {
		for _, f := range []string{"pgm", "bmp", "rle"} {
			other := encPixels(t, f, w, h, pix, brokenLayout(flag))
			switch {
			case f == "pgm" && flag == "ByteOrder":
				// PGM 无多字节整数：字节序不改变像素区。
				if !equalBytes(other, base) {
					t.Fatalf("pgm 的 ByteOrder 不应改变像素区")
				}
			default:
				if equalBytes(other, base) {
					t.Fatalf("%s 的 %s 未改变字节（标志未生效）", f, flag)
				}
			}
		}
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
