package api

import (
	"os"
	"path/filepath"
	"testing"

	"gotoolkit/internal/quant"
)

// gradient256 构造一条 0..255 的黑到白渐变（整段 uint8 定义域，不抽样）。
func gradient256(h int) *Image {
	img := &Image{W: 256, H: h, Pix: make([]uint8, 256*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < 256; x++ {
			img.Pix[y*256+x] = uint8(x)
		}
	}
	return img
}

// TestBitsGradientMonotoneRoundTrip 修后的量化器走完整 Encode->Decode 管线，
// 在 PGM 与 RLE（真正的压缩格式）上逐点检查：三档位深破坏点必须全为 0。
func TestBitsGradientMonotoneRoundTrip(t *testing.T) {
	for _, format := range []string{"pgm", "rle", "bmp"} {
		for _, bits := range []int{2, 3, 4} {
			src := gradient256(1)
			body, err := Encode(src, Options{Format: format, Bits: bits})
			if err != nil {
				t.Fatalf("%s bits=%d 编码失败: %v", format, bits, err)
			}
			got, err := Decode(body, Options{Format: format})
			if err != nil {
				t.Fatalf("%s bits=%d 解码失败: %v", format, bits, err)
			}
			if len(got.Pix) != 256 {
				t.Fatalf("%s bits=%d 像素长度 %d", format, bits, len(got.Pix))
			}
			breaks := quant.BreakIndices(got.Pix)
			if len(breaks) != 0 {
				head := breaks
				if len(head) > 5 {
					head = head[:5]
				}
				t.Fatalf("%s bits=%d 压缩重建后出现 %d 个破坏点，首批 %v",
					format, bits, len(breaks), head)
			}
			if w := quant.WorstDelta(got.Pix); w < 0 {
				t.Fatalf("%s bits=%d 最大反转量为 %d，不应为负", format, bits, w)
			}
		}
	}
}

// TestBitsRejectsOutOfRange 位深越界必须报 ErrBadOption。
func TestBitsRejectsOutOfRange(t *testing.T) {
	img := gradient256(1)
	for _, b := range []int{-1, 9, 16} {
		if _, err := Encode(img, Options{Format: "pgm", Bits: b}); err == nil {
			t.Fatalf("Bits=%d 应被拒绝", b)
		}
	}
}

// TestBitsZeroKeepsIdentity Bits=0 默认不量化，渐变逐点不变。
func TestBitsZeroKeepsIdentity(t *testing.T) {
	src := gradient256(1)
	body, err := Encode(src, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range src.Pix {
		if got.Pix[i] != src.Pix[i] {
			t.Fatalf("默认选项必须无损，像素 %d: %d != %d", i, got.Pix[i], src.Pix[i])
		}
	}
}

// TestPreservedBeforeSampleIsNonMonotone 留档的“修之前”样例必须仍然可解码、
// 仍然非单调（4bit 位反转表：7 个破坏点、最大反转量 -208）；
// 对应的修后样例必须零破坏。这样“样例说明的坏”与文件实际内容永远一致。
func TestPreservedBeforeSampleIsNonMonotone(t *testing.T) {
	before, err := readPGMFile(filepath.Join("..", "testdata", "monotonic", "before-bitreverse-b4.pgm"))
	if err != nil {
		t.Skipf("缺少留档样例: %v", err)
	}
	row := before.Pix[:before.W]
	if n := len(quant.BreakIndices(row)); n != 7 {
		t.Fatalf("留档修前样例应有 7 个破坏点，实为 %d", n)
	}
	if w := quant.WorstDelta(row); w != -208 {
		t.Fatalf("留档修前样例最大反转量应为 -208，实为 %d", w)
	}

	after, err := readPGMFile(filepath.Join("..", "testdata", "monotonic", "after-monotone-b4.pgm"))
	if err != nil {
		t.Skipf("缺少修后样例: %v", err)
	}
	if n := len(quant.BreakIndices(after.Pix[:after.W])); n != 0 {
		t.Fatalf("修后样例应零破坏点，实为 %d", n)
	}
}

func readPGMFile(p string) (*Image, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return Decode(raw, Options{Format: "pgm"})
}
