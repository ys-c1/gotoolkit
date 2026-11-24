package main

import (
	"strings"
	"testing"

	"gotoolkit/api"
)

func gradientImg(w, h int) *api.Image {
	img := &api.Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := x * 255 / (w - 1)
			if (x/2+y/2)%3 == 0 {
				v += 40
				if v > 255 {
					v = 255
				}
			}
			img.Pix[y*w+x] = uint8(v)
		}
	}
	return img
}

// TestMatrixCoversAllCombos 锁定矩阵规模：3 格式 × 2 子命令 × 3 开关态 = 18。
func TestMatrixCoversAllCombos(t *testing.T) {
	tiers, err := defaultTiers(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiers) != 2 {
		t.Fatalf("默认应有两档，得到 %d", len(tiers))
	}
	rep, err := buildMatrix(gradientImg(32, 24), tiers, "floyd-steinberg")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 18*2 {
		t.Fatalf("两档共应 36 行，得到 %d", len(rep.Rows))
	}
	seen := map[string]bool{}
	for _, r := range rep.Rows {
		seen[r.Format+"/"+r.Subcommand+"/"+r.Force] = true
	}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		for _, s := range []string{"encode", "decode"} {
			for _, fo := range []string{"off", "on", "palette"} {
				if !seen[f+"/"+s+"/"+fo] {
					t.Fatalf("缺组合 %s/%s/%s", f, s, fo)
				}
			}
		}
	}
	if !rep.AllOffIdentical {
		t.Fatal("全关行必须与基准逐字节相同")
	}
}

// TestMatrixAllOffIdentical 直接检查：每档中 force=off 的行 PSNR 为 inf、误差 0。
func TestMatrixAllOffIdentical(t *testing.T) {
	tiers, _ := defaultTiers(0)
	rep, err := buildMatrix(gradientImg(16, 16), tiers, "floyd-steinberg")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Rows {
		if r.Force != "off" {
			continue
		}
		if r.PSNRvsAllOff != "inf" || r.MaxErrvsAllOf != 0 {
			t.Fatalf("组合 %d (%s/%s) 全关却与基准不同", r.No, r.Format, r.Subcommand)
		}
	}
}

func TestErrorCodesDocumented(t *testing.T) {
	img := gradientImg(8, 8)
	_, err := api.Encode(img, api.Options{Format: "pgm", Bits: 9, Force: api.ForceOn})
	if err == nil || !strings.Contains(err.Error(), "E_LOWBIT_BITS") {
		t.Fatalf("位深越界错误码缺失: %v", err)
	}
	_, err = api.Encode(img, api.Options{
		Format: "pgm", Bits: 4, Force: api.ForceDither,
		Kernel: "custom:0,0,0;0,0,0;0,0,0",
	})
	if err == nil || !strings.Contains(err.Error(), "E_LOWBIT_KERNEL_SUM") {
		t.Fatalf("零核错误码缺失: %v", err)
	}
}
