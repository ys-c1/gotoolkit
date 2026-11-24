package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// TestRasterAligned 对多种画幅验证三种格式的像素/游程起始偏移都对齐，
// 并且对齐后回读像素与源一致。
func TestRasterAligned(t *testing.T) {
	sizes := []struct{ w, h int }{
		{1, 1}, {2, 2}, {3, 2}, {4, 3}, {7, 1}, {10, 10},
		{255, 255}, {256, 256}, {999, 3}, {1000, 7},
	}
	for _, s := range sizes {
		pix := make([]uint8, s.w*s.h)
		for i := range pix {
			pix[i] = uint8((i*37 + 11) & 0xff)
		}

		pg, err := EncodePGM(s.w, s.h, pix)
		if err != nil {
			t.Fatal(err)
		}
		pgOff := pgmRasterOffset(pg)
		if pgOff%AlignUnit != 0 {
			t.Errorf("%dx%d PGM 像素起点 %d mod %d = %d", s.w, s.h, pgOff, AlignUnit, pgOff%AlignUnit)
		}

		bm, err := EncodeBMP(s.w, s.h, pix)
		if err != nil {
			t.Fatal(err)
		}
		bmOff := int(binary.LittleEndian.Uint32(bm[10:]))
		if bmOff%AlignUnit != 0 {
			t.Errorf("%dx%d BMP bfOffBits %d mod %d = %d", s.w, s.h, bmOff, AlignUnit, bmOff%AlignUnit)
		}

		rl, err := EncodeRLE(s.w, s.h, pix)
		if err != nil {
			t.Fatal(err)
		}
		if rleHeaderLen%AlignUnit != 0 {
			t.Errorf("%dx%d RLE 游程起点 %d 未对齐", s.w, s.h, rleHeaderLen)
		}

		for name, dec := range map[string]func([]byte) (int, int, []uint8, error){
			"pgm": DecodePGM, "bmp": DecodeBMP, "rle": DecodeRLE,
		} {
			body := map[string][]byte{"pgm": pg, "bmp": bm, "rle": rl}[name]
			_, _, got, err := dec(body)
			if err != nil {
				t.Fatalf("%dx%d %s 解码失败: %v", s.w, s.h, name, err)
			}
			if !bytes.Equal(got, pix) {
				t.Fatalf("%dx%d %s 像素不一致", s.w, s.h, name)
			}
		}
	}
}

// TestPaddingNeverLeaksIntoPixels 是对「填充字节不许进入像素数据」的独立验证：
// 对多种画幅，用一个不共享实现的最小规范读取器定位光栅起点，断言：
//  1. 规范读取器读到的 w*h 个字节与源像素逐字节相同（填充没有混入）；
//  2. 头部的填充字节个数恰好等于 padFor(L0)，且全部是空格、全部位于光栅起点之前；
//  3. 光栅区（off 之后）不包含任何填充字节。
func TestPaddingNeverLeaksIntoPixels(t *testing.T) {
	for w := 1; w <= 200; w++ {
		h := 3
		pix := make([]uint8, w*h)
		for i := range pix {
			// 像素值刻意避开 ' '（0x20）与 0x00，与填充字节可区分。
			pix[i] = uint8(1 + (i*13)%254)
			if pix[i] == ' ' {
				pix[i] = 0x21
			}
		}
		pg, err := EncodePGM(w, h, pix)
		if err != nil {
			t.Fatal(err)
		}
		off := strictPGMRaster(pg)
		if off < 0 {
			t.Fatalf("w=%d: 无合法 P5 光栅起点", w)
		}
		if off%AlignUnit != 0 {
			t.Fatalf("w=%d: 光栅起点 %d 未对齐", w, off)
		}
		if !bytes.Equal(pg[off:off+w*h], pix) {
			t.Fatalf("w=%d: 规范读取器读到的像素含填充/错位", w)
		}
		// 统计 "P5\n" 之后、尺寸数字之前插入的填充空格数。
		padCount := 0
		for i := len("P5\n"); i < len(pg) && pg[i] == ' '; i++ {
			padCount++
		}
		nominal := len("P5\n") + len(fmt.Sprintf("%d %d", w, h)) + len("\n255\n")
		if want := padFor(nominal); padCount != want {
			t.Fatalf("w=%d: 填充字节数=%d，公式给出 %d", w, padCount, want)
		}
		// 光栅区一个填充字节都不该有：每个字节都必须等于源像素。
		for i := 0; i < w*h; i++ {
			if pg[off+i] != pix[i] {
				t.Fatalf("w=%d: 光栅第 %d 字节=0x%02x，应为像素 0x%02x", w, i, pg[off+i], pix[i])
			}
		}
	}
}

// TestPaddingAfterSeparatorWouldLeak 是反向证据：同样的填充字节若放在
// maxval 之后（光栅之前），规范读取器会把它们当成像素——这正是本实现
// 不允许的放置方式，也是 PGM 选择「放在 \"P5\\n\" 之后」的原因。
func TestPaddingAfterSeparatorWouldLeak(t *testing.T) {
	const w, h = 4, 3
	pix := []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	// 错误做法：规范头部 "P5\n4 3\n255\n" (11) + 1 个填充 + 像素，光栅起点=12。
	bad := append([]byte("P5\n4 3\n255\n"), ' ')
	bad = append(bad, pix...)
	off := strictPGMRaster(bad) // = 12，恰好跳过那个空格？不——规范只吃一个空白
	// 规范读取器在 maxval 后只吃一个空白（索引 11 的 '\n'），光栅起点=12，
	// 于是索引 11 之后的那个空格成了第一个像素。
	if got := bad[off : off+w*h]; bytes.Equal(got, pix) {
		t.Fatalf("预期填充泄漏到像素，但读到的却正确：% x", got)
	} else {
		t.Logf("反向证据：放在分隔符后的 0x%02x 被读成首个像素；got[0]=0x%02x want 0x%02x",
			bad[11], got[0], pix[0])
	}
}

// strictPGMRaster 是一个不共享实现的最小规范读取器：解析 P5 头，
// maxval 后吃掉恰好一个空白，返回光栅起点。
func strictPGMRaster(b []byte) int {
	if len(b) < 2 || b[0] != 'P' || b[1] != '5' {
		return -1
	}
	i := 2
	tok := func() (int, bool) {
		for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
			i++
		}
		s := i
		v := 0
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			v = v*10 + int(b[i]-'0')
			i++
		}
		return v, i > s
	}
	if _, ok := tok(); !ok {
		return -1
	}
	if _, ok := tok(); !ok {
		return -1
	}
	if _, ok := tok(); !ok {
		return -1
	}
	if i >= len(b) || !isSpace(b[i]) {
		return -1
	}
	return i + 1
}

// TestBMPPaddingBytesAreZero 验证 BMP 的填充位于 54 与 bfOffBits 之间且全为 0。
func TestBMPPaddingBytesAreZero(t *testing.T) {
	for w := 1; w <= 8; w++ {
		pix := make([]uint8, w*2)
		for i := range pix {
			pix[i] = 0xEE
		}
		b, err := EncodeBMP(w, 2, pix)
		if err != nil {
			t.Fatal(err)
		}
		off := int(binary.LittleEndian.Uint32(b[10:]))
		if off < 54 || off > 54+AlignUnit-1 {
			t.Fatalf("w=%d bfOffBits=%d 超出 [54,57]", w, off)
		}
		for i := 54; i < off; i++ {
			if b[i] != 0 {
				t.Fatalf("w=%d 填充字节 b[%d]=0x%02x 非零", w, i, b[i])
			}
		}
		// 真正的像素列（每行前 3w 字节）不应出现 0；
		// 每行末尾的 rowSize-3w 字节是 BMP 既有的行填充，按规范就是 0，不算越界。
		rowSize := (w*3 + 3) &^ 3
		for y := 0; y < 2; y++ {
			base := off + y*rowSize
			for x := 0; x < w*3; x++ {
				if b[base+x] != 0xEE {
					t.Fatalf("w=%d 行%d 像素字节 b[%d]=0x%02x，掺杂了填充", w, y, base+x, b[base+x])
				}
			}
		}
	}
}

// TestRLEHeaderUnchanged 验证 RLE 的 8 字节头后没有新增字节。
func TestRLEHeaderUnchanged(t *testing.T) {
	r, err := EncodeRLE(4, 1, []uint8{9, 9, 9, 7})
	if err != nil {
		t.Fatal(err)
	}
	if string(r[:4]) != "GTR1" || len(r) != 12 {
		t.Fatalf("RLE 头/长度异常: %q len=%d", r[:4], len(r))
	}
	if padFor(rleHeaderLen) != 0 {
		t.Fatalf("RLE 填充=%d，应为 0", padFor(rleHeaderLen))
	}
}

// TestPGMZeroCopyReadBack 验证 DecodePGM 返回的是入参的切片而非副本。
func TestPGMZeroCopyReadBack(t *testing.T) {
	pix := []uint8{1, 2, 3, 4, 5, 6}
	body, _ := EncodePGM(3, 2, pix)
	_, _, got, err := DecodePGM(body)
	if err != nil {
		t.Fatal(err)
	}
	off := pgmRasterOffset(body)
	if &got[0] != &body[off] {
		t.Fatalf("DecodePGM 返回了副本 (got[0] 地址 %p, body 光栅 %p)", &got[0], &body[off])
	}
	got[0] = 0x7F
	if body[off] != 0x7F {
		t.Fatal("返回切片与 data 不共享底层数组")
	}
}

func TestEvidenceReport(t *testing.T) {
	t.Logf("AlignUnit=%d", AlignUnit)
	type row struct {
		w, h   int
		pgmOff int
		pgmPad int
		bmpOff int
		bmpPad int
		rleOff int
		rlePad int
	}
	for _, s := range []struct{ w, h int }{{4, 3}, {2, 2}, {3, 2}, {255, 255}, {1000, 7}} {
		pix := make([]uint8, s.w*s.h)
		pg, _ := EncodePGM(s.w, s.h, pix)
		bm, _ := EncodeBMP(s.w, s.h, pix)
		rl, _ := EncodeRLE(s.w, s.h, pix)
		pgOff := pgmRasterOffset(pg)
		bmOff := int(binary.LittleEndian.Uint32(bm[10:]))
		_ = rl
		t.Logf("%dx%d | PGM off=%d pad=%d | BMP off=%d pad=%d | RLE off=%d pad=%d",
			s.w, s.h, pgOff, pgOff-8-len(fmt.Sprintf("%d %d", s.w, s.h)),
			bmOff, bmOff-54, rleHeaderLen, padFor(rleHeaderLen))
	}
}
