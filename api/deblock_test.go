package api

import (
	"bytes"
	"testing"
)

// 滤波关闭（零值 Options）时，解码产物必须与显式关闭参数逐字节一致，
// 且编码 golden 不受新参数影响（golden 已在 api_test.go 覆盖）。
func TestDeblockOffLeavesProductUnchanged(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Decode(body, Options{Format: "pgm", BlockSize: 8, DeblockStrength: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("DeblockStrength=0 时解码像素发生变化")
	}

	// 强度>0 且给了块长才应改变图像。
	c, err := Decode(body, Options{Format: "pgm", BlockSize: 2, DeblockStrength: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Pix, c.Pix) {
		t.Fatal("开启去块滤波后像素应发生变化")
	}
}

func TestDeblockOptionValidation(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "pgm", DeblockStrength: 1.5}); err == nil {
		t.Fatal("强度越界应报错")
	}
	body, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(body, Options{Format: "pgm", DeblockStrength: -0.1}); err == nil {
		t.Fatal("负强度应报错")
	}
	if _, err := Decode(body, Options{Format: "pgm", BlockSize: -4}); err == nil {
		t.Fatal("负块长应报错")
	}
}

func TestRunDeblockStudy(t *testing.T) {
	rep, err := RunDeblockStudy("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 3 {
		t.Fatalf("应有三档，实际 %d", len(rep.Rows))
	}
	if !rep.OffSameAll || rep.Agg.JRatio >= 1 || rep.Agg.DRatio > 1 {
		t.Fatalf("对照结论不成立: off=%v JRatio=%.4f DRatio=%.4f",
			rep.OffSameAll, rep.Agg.JRatio, rep.Agg.DRatio)
	}
	if OptimalDeblockAlpha(1, 1) != 0.5 {
		t.Fatal("闭式解 1/(1+1) 应为 0.5")
	}
}
