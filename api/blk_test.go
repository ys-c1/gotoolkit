package api

import (
	"bytes"
	"testing"
)

// TestBLKSameImageThreeBlockSizes 缝宽为零必须用同一张图的三档块长验：
// 这里只构造一次 64×64 渐变图，三档块长 4/8/16 共用。
func TestBLKSameImageThreeBlockSizes(t *testing.T) {
	img := NewGradientImage(64, 64) // 全函数只此一张图
	sizes := []int{4, 8, 16}

	// 先确认三档都能整除画幅，尾块逻辑不干扰结论。
	for _, bs := range sizes {
		if 64%bs != 0 {
			t.Fatalf("块长 %d 不能整除 64", bs)
		}
	}

	for _, bs := range sizes {
		// 不预测：缝宽随块长变大，理论上横向梯度 2 × 块长。
		flat, err := Encode(img, Options{Format: "blk", BlockSize: bs, Predict: false})
		if err != nil {
			t.Fatal(err)
		}
		imgFlat, err := Decode(flat, Options{Format: "blk"})
		if err != nil {
			t.Fatal(err)
		}
		sf := MeasureSeam(imgFlat, bs)
		if sf.Max != 2*bs {
			t.Fatalf("bs=%d 不预测缝宽=%d，期望 %d", bs, sf.Max, 2*bs)
		}

		// 预测：同一张图、同一块长，缝宽必须为零。
		pred, err := Encode(img, Options{Format: "blk", BlockSize: bs, Predict: true})
		if err != nil {
			t.Fatal(err)
		}
		imgPred, err := Decode(pred, Options{Format: "blk"})
		if err != nil {
			t.Fatal(err)
		}
		sp := MeasureSeam(imgPred, bs)
		if sp.Max != 0 || sp.Vertical != 0 || sp.Horizontal != 0 {
			t.Fatalf("bs=%d 预测缝宽应为零，实际 %+v", bs, sp)
		}
	}
}

// TestBLKToggleBackUnchanged 不解预测的路径留着，切回去结果不变。
func TestBLKToggleBackUnchanged(t *testing.T) {
	img := NewGradientImage(24, 18)
	bs := 5 // 带尾块，顺带覆盖非整除画幅

	flat, err := Encode(img, Options{Format: "blk", BlockSize: bs, Predict: false})
	if err != nil {
		t.Fatal(err)
	}
	// 直流负载逐字节一致：仅 predict 开关字节不同。
	pred, err := Encode(img, Options{Format: "blk", BlockSize: bs, Predict: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(flat) != len(pred) {
		t.Fatalf("流长度不同: %d != %d", len(flat), len(pred))
	}
	for i := range flat {
		if i == 9 {
			continue
		}
		if flat[i] != pred[i] {
			t.Fatalf("下标 %d 负载不一致", i)
		}
	}

	// 切回不预测路径，两次解码结果逐像素不变。
	a, err := Decode(flat, Options{Format: "blk"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Decode(flat, Options{Format: "blk"})
	if err != nil {
		t.Fatal(err)
	}
	if a.W != b.W || a.H != b.H || !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("不预测路径不具确定性")
	}
}

func TestBLKOptionValidation(t *testing.T) {
	img := NewGradientImage(8, 8)
	if _, err := Encode(img, Options{Format: "blk", BlockSize: 65}); err == nil {
		t.Fatal("块长越界应报错")
	}
	if _, err := Encode(img, Options{Format: "blk", BlockSize: -1}); err == nil {
		t.Fatal("负块长应报错")
	}
	// 默认块长应当可编码。
	if _, err := Encode(img, Options{Format: "blk"}); err != nil {
		t.Fatalf("默认块长编码失败: %v", err)
	}
}

func TestBLKRoundTripTwoPaths(t *testing.T) {
	img := NewGradientImage(20, 12)
	for _, predict := range []bool{false, true} {
		body, err := Encode(img, Options{Format: "blk", BlockSize: 6, Predict: predict})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(body, Options{Format: "blk"})
		if err != nil {
			t.Fatal(err)
		}
		if got.W != img.W || got.H != img.H || len(got.Pix) != len(img.Pix) {
			t.Fatalf("predict=%v 画幅不一致", predict)
		}
	}
}
