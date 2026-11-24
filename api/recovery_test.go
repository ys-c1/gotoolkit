package api

import (
	"bytes"
	"image/png"
	"testing"
)

func recoverySample() *Image {
	img := &Image{W: 8, H: 4, Pix: make([]uint8, 32)}
	for i := range img.Pix {
		img.Pix[i] = uint8((i*17 + 3) % 254) // 不含兜底标记 255
	}
	return img
}

func TestIntegrityManifestDetectsMergedRange(t *testing.T) {
	img := recoverySample()
	data, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := BuildIntegrityManifest(data, "pgm", 8)
	if err != nil {
		t.Fatal(err)
	}
	if ranges, err := m.Detect(data); err != nil || len(ranges) != 0 {
		t.Fatalf("完好文件不应检出坏块: ranges=%v err=%v", ranges, err)
	}

	corrupt := append([]byte(nil), data...)
	start, end := 10, 23 // 故意不按 8 字节块对齐
	for i := start; i < end; i++ {
		corrupt[i] = ^corrupt[i]
	}
	ranges, err := m.Detect(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 {
		t.Fatalf("相邻坏块应合并为一段，得到 %v", ranges)
	}
	// 块大小为 8：[8,16),[16,24) 合并成 [8,24)。
	if ranges[0] != (ByteRange{Start: 8, End: 24}) {
		t.Fatalf("检出区间错误: %+v", ranges[0])
	}
	exact, err := DiffByteRanges(data, corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 || exact[0] != (ByteRange{Start: start, End: end}) {
		t.Fatalf("真值区间错误: %v", exact)
	}
}

func TestRecoverPGMMarksOnlyBadPixelsAndKeepsRest(t *testing.T) {
	img := recoverySample()
	data, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	off := len("P5\n8 4\n255\n")
	bad := ByteRange{Start: off + 9, End: off + 14} // 5 个连续像素
	corrupt := append([]byte(nil), data...)
	for i := bad.Start; i < bad.End; i++ {
		corrupt[i] = ^corrupt[i]
	}

	rec, err := RecoverPGM(corrupt, []ByteRange{bad})
	if err != nil {
		t.Fatal(err)
	}
	if rec.MarkerPixels != 5 || rec.GoodPixels != 27 {
		t.Fatalf("标记/正常像素数错误: marker=%d good=%d", rec.MarkerPixels, rec.GoodPixels)
	}
	if len(rec.PixelRanges) != 1 || rec.PixelRanges[0].Start != 9 || rec.PixelRanges[0].End != 14 {
		t.Fatalf("像素区间映射错误: %+v", rec.PixelRanges)
	}
	if ok, at := rec.CompareGoodPixels(img); !ok {
		t.Fatalf("正常像素在 %d 处与原图不一致", at)
	}
	for i := 9; i < 14; i++ {
		if rec.Pix[i] != RecoveryMarker {
			t.Fatalf("像素 %d 应为哨兵 %d，实际 %d", i, RecoveryMarker, rec.Pix[i])
		}
	}
}

func TestIntactRecoveryHasNoMarkerAndRendersGrayPNG(t *testing.T) {
	img := recoverySample()
	data, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := RecoverPGM(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.MarkerPixels != 0 || !bytes.Equal(int16ToUint8(rec.Pix), img.Pix) {
		t.Fatal("完好文件恢复结果不应有标记且必须逐像素一致")
	}
	pngBytes, err := RenderMarkedPNG(rec, 2)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() != img.W*2 || im.Bounds().Dy() != img.H*2 {
		t.Fatalf("缩放尺寸错误: %v", im.Bounds())
	}
	for y := 0; y < im.Bounds().Dy(); y++ {
		for x := 0; x < im.Bounds().Dx(); x++ {
			r, g, b, _ := im.At(x, y).RGBA()
			if r != g || g != b {
				t.Fatalf("完好图必须全灰度，非灰像素 at %d,%d: %d,%d,%d", x, y, r, g, b)
			}
		}
	}
}

func TestRenderMarkerIsMagentaAndOutsideGray(t *testing.T) {
	rec := &RecoveredImage{W: 1, H: 1, Pix: []int16{RecoveryMarker}, MarkerPixels: 1}
	ppm, err := RenderMarkedPPM(rec)
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := []byte{255, 0, 255}
	if !bytes.HasSuffix(ppm, wantSuffix) {
		t.Fatalf("PPM 末尾应为品红 %v，实际 % x", wantSuffix, ppm)
	}
	pngBytes, err := RenderMarkedPNG(rec, 1)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := im.At(0, 0).RGBA()
	if r != 65535 || g != 0 || b != 65535 {
		t.Fatalf("标记颜色错误: r=%d g=%d b=%d", r, g, b)
	}
}

func TestByteRecoveryRatios(t *testing.T) {
	theory, measured, tg, mg := ByteRecoveryRatios(1000, 120, 120)
	if tg != 880 || mg != 880 || theory != 0.88 || measured != 0.88 {
		t.Fatalf("等长损坏比例错误: %v %v %d %d", theory, measured, tg, mg)
	}
	_, measured2, _, mg2 := ByteRecoveryRatios(1000, 120, 110)
	if mg2 != 890 || measured2 != 0.89 {
		t.Fatalf("实测变化字节比例错误: measured=%v good=%d", measured2, mg2)
	}
}

func int16ToUint8(in []int16) []uint8 {
	out := make([]uint8, len(in))
	for i, v := range in {
		out[i] = uint8(v)
	}
	return out
}
