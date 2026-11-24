package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// 当前组合（jnt + none + BlockLen=0）必须与现有 rle golden 逐字节一致。
func TestJointCurrentComboMatchesGolden(t *testing.T) {
	img := sample()
	got, err := Encode(img, Options{Format: "jnt", Predict: "none", BlockLen: 0})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", "rle.bin"))
	if err != nil {
		t.Skipf("缺 golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("当前组合与 rle golden 不一致: got %d want %d", len(got), len(want))
	}
	// 默认零值 Predict/BlockLen 也应回落。
	got2, err := Encode(img, Options{Format: "jnt"})
	if err != nil || !bytes.Equal(got2, want) {
		t.Fatalf("零值选项应回落 GTR1, err=%v", err)
	}
}

func TestJointRoundTripViaAPI(t *testing.T) {
	img := sample()
	for _, opt := range []Options{
		{Format: "jnt", Predict: "left", BlockLen: 3},
		{Format: "jnt", Predict: "left", BlockLen: 0},
		{Format: "jnt", Predict: "none", BlockLen: 4},
	} {
		body, err := Encode(img, opt)
		if err != nil {
			t.Fatalf("%+v 编码失败: %v", opt, err)
		}
		dec, err := Decode(body, Options{Format: "jnt"})
		if err != nil {
			t.Fatalf("%+v 解码失败: %v", opt, err)
		}
		if dec.W != img.W || dec.H != img.H || !bytes.Equal(dec.Pix, img.Pix) {
			t.Fatalf("%+v 往返不一致", opt)
		}
	}
}

func TestJointBadOptions(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "jnt", Predict: "paeth"}); err == nil {
		t.Fatal("非法 Predict 应报错")
	}
	if _, err := Encode(img, Options{Format: "jnt", BlockLen: -1}); err == nil {
		t.Fatal("负 BlockLen 应报错")
	}
	if _, err := AnalyzeJoint(img, "left", -1); err == nil {
		t.Fatal("AnalyzeJoint 负块长应报错")
	}
	if _, err := AnalyzeJoint(nil, "left", 8); err == nil {
		t.Fatal("nil 图像应报错")
	}
}

func TestAnalyzeAndSolveJoint(t *testing.T) {
	img := sample()
	var reps []*JointReport
	for _, p := range []string{"none", "left"} {
		for _, b := range []int{0, 4} {
			r, err := AnalyzeJoint(img, p, b)
			if err != nil {
				t.Fatal(err)
			}
			reps = append(reps, r)
		}
	}
	if idx := SolveJoint(reps); idx < 0 || idx >= len(reps) {
		t.Fatalf("求解下标越界: %d", idx)
	}
}
