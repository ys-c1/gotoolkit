package api

import (
	"bytes"
	"testing"
)

func stageByName(stages []Stage, name string) Stage {
	for _, st := range stages {
		if st.Name == name {
			return st
		}
	}
	return Stage{}
}

// TestPipeStagesEncode 校验 encode 管线四个环节的语义与最终产物。
func TestPipeStagesEncode(t *testing.T) {
	img := sample()
	raw, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}

	for _, gain := range []int{-50, 0, 50} {
		stages, err := PipeStages(raw, "encode", "bmp", gain)
		if err != nil {
			t.Fatalf("gain=%d: %v", gain, err)
		}
		if len(stages) != 4 {
			t.Fatalf("gain=%d: 应有4个环节，实际 %d", gain, len(stages))
		}
		s1, s2 := stageByName(stages, StagePGMDecode), stageByName(stages, StageGain)
		if !bytes.Equal(s1.Pix, img.Pix) {
			t.Fatalf("gain=%d: 环节1像素应等于解码原图", gain)
		}
		wantPix := img.Pix
		if gain != 0 {
			wantPix = applyGain(img, gain).Pix
		}
		if !bytes.Equal(s2.Pix, wantPix) {
			t.Fatalf("gain=%d: 环节2像素与增益结果不符", gain)
		}
		s3, s4 := stageByName(stages, StageFormatEnc), stageByName(stages, StageFileBytes)
		if !bytes.Equal(s3.Body, s4.Body) {
			t.Fatalf("gain=%d: 环节3/4字节应相同", gain)
		}
		// 环节4必须与直接调 Encode 的结果逐字节一致。
		direct, err := Encode(img, Options{Format: "bmp", Gain: gain})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(s4.Body, direct) {
			t.Fatalf("gain=%d: 环节4与直接 Encode 结果不一致", gain)
		}
	}
}

// TestPipeStagesDecode 校验 decode 管线：环节1是无增益解码，环节2是反向增益结果。
func TestPipeStagesDecode(t *testing.T) {
	img := sample()
	mid, err := Encode(img, Options{Format: "rle"})
	if err != nil {
		t.Fatal(err)
	}

	stages, err := PipeStages(mid, "decode", "rle", 50)
	if err != nil {
		t.Fatal(err)
	}
	s1, s2 := stageByName(stages, StagePGMDecode), stageByName(stages, StageGain)
	if !bytes.Equal(s1.Pix, img.Pix) {
		t.Fatal("环节1应为无增益格式解码的原图")
	}
	// api.Decode 内部施加 -gain。
	want := applyGain(img, -50).Pix
	if !bytes.Equal(s2.Pix, want) {
		t.Fatal("环节2应等于反向增益后的像素")
	}
	if bytes.Equal(s1.Pix, s2.Pix) {
		t.Fatal("gain 非 0 时环节1与环节2应当不同")
	}
	s4 := stageByName(stages, StageFileBytes)
	direct, err := Decode(mid, Options{Format: "rle", Gain: 50})
	if err != nil {
		t.Fatal(err)
	}
	directPGM, err := Encode(direct, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s4.Body, directPGM) {
		t.Fatal("decode 环节4应与直接 Decode+Encode 结果一致")
	}
}

// TestPipeStagesBadMode 未知模式返回错误。
func TestPipeStagesBadMode(t *testing.T) {
	if _, err := PipeStages(nil, "transcode", "pgm", 0); err == nil {
		t.Fatal("未知模式应报错")
	}
}
