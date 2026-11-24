package api

import "testing"

// TestQuantBiasAPIFourClasses 从唯一对外入口复跑四类图 × 三档位深验收。
func TestQuantBiasAPIFourClasses(t *testing.T) {
	imgs := SampleQuantImages(64, DefaultQuantSeed) // 小尺寸也应稳定通过
	rep, err := BuildQuantBiasReport(imgs, []int{2, 3, 4}, DefaultQuantSeed)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.AllPass {
		t.Fatalf("API 偏置验收未全部通过: %+v", rep.ClassVerdicts)
	}
	if rep.Threshold != QuantBiasTolerance || QuantBiasTolerance != 0.05 {
		t.Fatalf("阈值必须固定为 0.05")
	}
}

func TestAnalyzeQuantBiasRejects(t *testing.T) {
	if _, err := AnalyzeQuantBias(nil, 2, "x"); err == nil {
		t.Fatal("nil 图应报错")
	}
	bad := &Image{W: 2, H: 2, Pix: []byte{1, 2, 3}}
	if _, err := AnalyzeQuantBias(bad, 2, "x"); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
	img := &Image{W: 1, H: 1, Pix: []byte{10}}
	if _, err := AnalyzeQuantBias(img, 9, "x"); err == nil {
		t.Fatal("位深越界应报错")
	}
}
