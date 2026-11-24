package api_test

import (
	"bytes"
	"math"
	"testing"

	"gotoolkit/api"
)

func targetImage(n int) *api.Image {
	img := &api.Image{W: n, H: n, Pix: make([]uint8, n*n)}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.Pix[y*n+x] = uint8((x*5 + y*11 + (x*y)%31) % 256)
		}
	}
	return img
}

// 每个可达位深对应的精确体积点目标，都应被二分求出且实测落在点上。
func TestSolveQualityEveryDepthPointTarget(t *testing.T) {
	img := targetImage(64)
	for d := 1; d <= 8; d++ {
		size := api.BLKDepthSize(img.W, img.H, api.DefaultBlockSize, d)
		rep, err := api.SolveQuality(img, api.Options{
			BlockSize:      api.DefaultBlockSize,
			TargetMinBytes: size,
			TargetMaxBytes: size,
		})
		if err != nil {
			t.Fatalf("d=%d 求解失败: %v", d, err)
		}
		if !rep.InRange {
			t.Fatalf("d=%d 精确点目标 %d 应可达，得到 Q=%d B=%d", d, size, rep.Quality, rep.Bytes)
		}
		if rep.Depth != d || rep.Bytes != size {
			t.Fatalf("d=%d 应对齐: depth=%d bytes=%d want %d", d, rep.Depth, rep.Bytes, size)
		}
		if len(rep.Body) != rep.Bytes {
			t.Fatalf("报告 Bytes=%d 与 Body 长度 %d 不一致", rep.Bytes, len(rep.Body))
		}
		if pred := api.BLKPredictedSize(img.W, img.H, api.DefaultBlockSize, rep.Quality); pred != rep.Bytes {
			t.Fatalf("单调体积公式预测 %d != 实测 %d", pred, rep.Bytes)
		}
	}
}

func TestSolveQualityGapFallback(t *testing.T) {
	img := targetImage(128)
	b := api.DefaultBlockSize
	s2 := api.BLKDepthSize(128, 128, b, 2)
	s3 := api.BLKDepthSize(128, 128, b, 3)
	lo := s2 + (s3-s2)/3
	hi := s3 - (s3-s2)/3

	rep, err := api.SolveQuality(img, api.Options{TargetMinBytes: lo, TargetMaxBytes: hi})
	if err != nil {
		t.Fatal(err)
	}
	if rep.InRange {
		t.Fatal("位深空档窄区间应不可达")
	}
	if rep.GapBytes <= 0 || rep.ClosestBytes <= 0 {
		t.Fatal("区间外必须给出正差距与最接近体积")
	}
	if len(rep.Body) != rep.ClosestBytes {
		t.Fatalf("兜底 Body 长度 %d 应等于最接近实测 %d", len(rep.Body), rep.ClosestBytes)
	}
	if rep.Quality != rep.ClosestQuality {
		t.Fatal("采用 Q 必须等于最接近 Q")
	}
	if rep.GapPercent <= 0 || math.IsNaN(rep.GapPercent) {
		t.Fatalf("差距百分比异常: %v", rep.GapPercent)
	}
}

func TestSolveQualityRequiresTarget(t *testing.T) {
	img := targetImage(16)
	if _, err := api.SolveQuality(img, api.Options{}); err == nil {
		t.Fatal("未给目标区间应返回 ErrBadTarget")
	}
	if _, err := api.SolveQuality(nil, api.Options{TargetMinBytes: 1, TargetMaxBytes: 2}); err == nil {
		t.Fatal("nil 图像应报错")
	}
}

func TestSolveQualityHonorsPointTargetField(t *testing.T) {
	img := targetImage(32)
	rep, err := api.SolveQuality(img, api.Options{TargetBytes: 20000})
	if err != nil {
		t.Fatal(err)
	}
	// 点目标 [20000,20000] 几乎必然落在离散档外，但必须给出最接近值。
	if rep.MinBytes != 20000 || rep.MaxBytes != 20000 {
		t.Fatalf("TargetBytes 应展开为点区间，得到 [%d,%d]", rep.MinBytes, rep.MaxBytes)
	}
	if rep.Bytes != rep.ClosestBytes && !rep.InRange {
		t.Fatal("未命中时 Bytes 应为最接近实测")
	}
}

func TestBLKEncodeDecodeViaAPI(t *testing.T) {
	img := targetImage(40)
	body, err := api.Encode(img, api.Options{Format: "blk", Quality: 90, BlockSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	got, err := api.Decode(body, api.Options{Format: "blk"})
	if err != nil {
		t.Fatal(err)
	}
	if got.W != 40 || got.H != 40 || len(got.Pix) != 1600 {
		t.Fatalf("blk 解码画幅错误: %dx%d len=%d", got.W, got.H, len(got.Pix))
	}

	// 纯色图经公共 API 也必须无损往返。
	solid := &api.Image{W: 12, H: 12, Pix: bytes.Repeat([]byte{77}, 144)}
	b2, err := api.Encode(solid, api.Options{Format: "blk", Quality: 1})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := api.Decode(b2, api.Options{Format: "blk"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g2.Pix, solid.Pix) {
		t.Fatal("纯色 BLK 应无损往返")
	}
}

func TestOptionsValidationNewFields(t *testing.T) {
	img := targetImage(8)
	if _, err := api.Encode(img, api.Options{Format: "blk", Quality: 101}); err == nil {
		t.Fatal("Quality>100 应报参数越界")
	}
	if _, err := api.Encode(img, api.Options{Format: "blk", BlockSize: 999}); err == nil {
		t.Fatal("BlockSize 超上限应报错")
	}
	if _, err := api.Encode(img, api.Options{Format: "tga"}); err == nil {
		t.Fatal("未知格式仍应报错")
	}
}
