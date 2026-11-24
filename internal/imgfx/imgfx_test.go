package imgfx

import "testing"

func TestThumbnailDivOneIsIdentity(t *testing.T) {
	pix := []uint8{1, 2, 3, 4, 5, 6}
	w, h, out, err := Thumbnail(3, 2, pix, 1)
	if err != nil {
		t.Fatal(err)
	}
	if w != 3 || h != 2 || len(out) != 6 {
		t.Fatal("div=1 画幅不应变化")
	}
	for i := range pix {
		if out[i] != pix[i] {
			t.Fatalf("div=1 像素应不变, %d != %d", out[i], pix[i])
		}
	}
}

func TestThumbnailAreaAverage(t *testing.T) {
	// 2×2 整块平均：(1+2+5+6)/4=3.5->4（四舍五入），(3+4+7+8)/4=5.5->6。
	pix := []uint8{
		1, 2, 3, 4,
		5, 6, 7, 8,
	}
	w, h, out, err := Thumbnail(4, 2, pix, 2)
	if err != nil {
		t.Fatal(err)
	}
	if w != 2 || h != 1 || out[0] != 4 || out[1] != 6 {
		t.Fatalf("面积平均错误: %+v", out)
	}
}

func TestThumbnailEdgeBlock(t *testing.T) {
	// 5 宽 / div=3 => 两块，边缘块宽 2。
	pix := []uint8{
		10, 20, 30, 40, 50,
		60, 70, 80, 90, 100,
	}
	w, h, out, err := Thumbnail(5, 2, pix, 3)
	if err != nil {
		t.Fatal(err)
	}
	if w != 2 || h != 1 || len(out) != 2 {
		t.Fatalf("边缘分块画幅错误: %dx%d", w, h)
	}
	// 第一块 3×2 六像素和 270，平均 45；第二块 2×2 和 280，平均 70。
	if out[0] != 45 || out[1] != 70 {
		t.Fatalf("边缘块平均错误: %+v", out)
	}
}

func TestThumbnailRejectsBadDiv(t *testing.T) {
	if _, _, _, err := Thumbnail(2, 2, []uint8{0, 0, 0, 0}, 0); err != ErrBadDiv {
		t.Fatalf("div=0 应返回 ErrBadDiv, 实际 %v", err)
	}
	if _, _, _, err := Thumbnail(2, 2, []uint8{0, 0, 0, 0}, -2); err != ErrBadDiv {
		t.Fatalf("负 div 应返回 ErrBadDiv, 实际 %v", err)
	}
}

func TestIndexPaletteEndpoints(t *testing.T) {
	res, err := IndexImage(2, 1, []uint8{0, 255}, 4, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Palette[0] != 0 || res.Palette[3] != 255 {
		t.Fatalf("调色板端点错误: %+v", res.Palette)
	}
	if res.Index[0] != 0 || res.Index[1] != 3 {
		t.Fatalf("端点映射错误: %+v", res.Index)
	}
	if len(res.Index) != 2 {
		t.Fatal("索引必须每像素一项，不去重")
	}
	if res.Quant[0] != 0 || res.Quant[1] != 255 {
		t.Fatal("回填灰度错误")
	}
}

func TestIndexRejectsZeroBuckets(t *testing.T) {
	if _, err := IndexImage(1, 1, []uint8{0}, 0, false, 1); err != ErrZeroBuckets {
		t.Fatalf("桶数为零应返回 ErrZeroBuckets, 实际 %v", err)
	}
}

func TestDitherChangesQuantButKeepsLengths(t *testing.T) {
	// 渐变图上抖动会把中间灰度打散到相邻桶。
	pix := make([]uint8, 16)
	for i := range pix {
		pix[i] = uint8(i * 17)
	}
	plain, err := IndexImage(4, 4, pix, 4, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	dit, err := IndexImage(4, 4, pix, 4, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	same := true
	for i := range plain.Quant {
		if plain.Quant[i] != dit.Quant[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("抖动应改变至少一个量化结果")
	}
	if len(dit.Index) != 16 || len(dit.Quant) != 16 || len(dit.Palette) != 4 {
		t.Fatal("抖动不得改变索引/像素数量")
	}

	// 同一图、除数 3 时抖动更弱：结果可以不同，但所有值仍必须落在调色板内。
	weak, err := IndexImage(4, 4, pix, 4, true, 3)
	if err != nil {
		t.Fatal(err)
	}
	inPalette := func(v uint8) bool {
		for _, p := range weak.Palette {
			if p == v {
				return true
			}
		}
		return false
	}
	for _, v := range weak.Quant {
		if !inPalette(v) {
			t.Fatalf("抖动结果 %d 超出调色板", v)
		}
	}
}
