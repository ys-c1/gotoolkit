package api

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func sample() *Image {
	img := &Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	return img
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s 尺寸不一致", f)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 像素不一致", f)
		}
	}
}

func TestGoldenBytes(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 与原编码不逐字节一致", f)
		}
	}
}

func TestRejects(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "tga"}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
}

// 率失真关闭时，三格式输出必须与仓库基准逐字节一致。
func TestRDDisabledMatchesGolden(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		// 显式带着 EnableRD=false 的零值参数字段，确保新字段不影响旧路径。
		got, err := Encode(img, Options{Format: f, EnableRD: false, TargetBPP: 0, Step: 0})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 关闭 RD 后与基准不一致", f)
		}
	}
}

// 十二组逐条对账：3 格式 × 4 档目标，缺一组不通过。
func TestRDTwelveGroups(t *testing.T) {
	img := sample()
	targets := []float64{0.5, 1.0, 2.0, 4.0}
	seen := map[string]bool{}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		for _, bpp := range targets {
			report, err := EncodeRD(img, Options{Format: f, EnableRD: true, TargetBPP: bpp})
			if err != nil {
				t.Fatalf("%s bpp=%.1f 不应报错: %v", f, bpp, err)
			}
			if report.Format != f || report.TargetBPP != bpp {
				t.Fatalf("%s bpp=%.1f 报告字段错位", f, bpp)
			}
			if report.ActualBits != len(report.Body)*8 || report.Bytes != len(report.Body) {
				t.Fatalf("%s bpp=%.1f 字节/比特对账不一致", f, bpp)
			}
			if report.Step < 1 || report.Step > 255 {
				t.Fatalf("%s bpp=%.1f 步长越界 %d", f, bpp, report.Step)
			}
			if report.Reachable && report.GapBits != 0 {
				t.Fatalf("%s bpp=%.1f 可达但差距非零", f, bpp)
			}
			if !report.Reachable && report.GapBits == 0 {
				t.Fatalf("%s bpp=%.1f 差距为零却标不可达", f, bpp)
			}
			seen[f+":"+fmt.Sprint(bpp)] = true
		}
	}
	if len(seen) != 12 {
		t.Fatalf("十二组对账缺组，实际 %d 组", len(seen))
	}
}

// 非法目标值与步长为零各对应一个错误码。
func TestRDErrorCodes(t *testing.T) {
	img := sample()
	badTargets := []float64{0, -1, math.NaN(), math.Inf(1)}
	for _, bpp := range badTargets {
		if _, err := EncodeRD(img, Options{Format: "pgm", EnableRD: true, TargetBPP: bpp}); !errors.Is(err, ErrBadTarget) {
			t.Fatalf("TargetBPP=%v 应报 ErrBadTarget，实际 %v", bpp, err)
		}
	}
	// 极端高目标：反解步长为 0。
	if _, err := EncodeRD(img, Options{Format: "pgm", EnableRD: true, TargetBPP: 32.0}); !errors.Is(err, ErrZeroStep) {
		t.Fatalf("极端目标应报 ErrZeroStep，实际 %v", err)
	}
	// 手动步长越界走参数错误；步长 0 在自动模式下不直接是用户输入，但负值非法。
	if _, err := EncodeRD(img, Options{Format: "pgm", EnableRD: true, TargetBPP: 1.0, Step: -1}); !errors.Is(err, ErrBadOption) {
		t.Fatalf("负步长应报 ErrBadOption，实际 %v", err)
	}
	if _, err := EncodeRD(img, Options{Format: "pgm", EnableRD: true, TargetBPP: 1.0, Step: 256}); !errors.Is(err, ErrBadOption) {
		t.Fatalf("步长 256 应报 ErrBadOption，实际 %v", err)
	}
}

// 目标不可达时不报错，报告最接近值与差距。
func TestRDUnreachableReportsClosest(t *testing.T) {
	img := sample()
	// PGM 体积由画幅定死（头 + 12 字节），低目标必然不可达。
	report, err := EncodeRD(img, Options{Format: "pgm", EnableRD: true, TargetBPP: 0.01})
	if err != nil {
		t.Fatalf("不可达不应返回错误: %v", err)
	}
	if report.Reachable {
		t.Fatal("PGM 固定体积下超低目标应不可达")
	}
	if report.GapBits <= 0 || report.GapRatio <= 0 {
		t.Fatal("不可达时差距应为正")
	}
	// PGM 各步长字节完全相同，平局按最小失真取最小步长。
	if report.Step != 1 {
		t.Fatalf("固定体积平局应取最小步长 1，实际 %d", report.Step)
	}
	if report.Bytes != len(report.Body) {
		t.Fatal("报告字节数与实际输出不一致")
	}
}

// 手动步长路径：指定步长时不搜索，报告步长等于指定值，且编码可解。
func TestRDManualStep(t *testing.T) {
	img := sample()
	report, err := EncodeRD(img, Options{Format: "rle", EnableRD: true, TargetBPP: 2.0, Step: 8})
	if err != nil {
		t.Fatal(err)
	}
	if report.Step != 8 || report.InitialStep != 8 {
		t.Fatalf("手动步长未生效: step=%d init=%d", report.Step, report.InitialStep)
	}
	got, err := Decode(report.Body, Options{Format: "rle"})
	if err != nil {
		t.Fatal(err)
	}
	if got.W != img.W || got.H != img.H || len(got.Pix) != len(img.Pix) {
		t.Fatal("RD 编码后解码画幅不一致")
	}
}

// 四档目标实际/目标并列的数值合理性：目标升高，实际体积单调不减（RLE 可变体积）。
func TestRDMonotonicRLE(t *testing.T) {
	img := sample()
	var lastBits int
	for _, bpp := range []float64{0.5, 1.0, 2.0, 4.0} {
		report, err := EncodeRD(img, Options{Format: "rle", EnableRD: true, TargetBPP: bpp})
		if err != nil {
			t.Fatal(err)
		}
		if report.ActualBits < lastBits {
			t.Fatalf("bpp=%.1f 实际比特 %d 小于前档 %d", bpp, report.ActualBits, lastBits)
		}
		lastBits = report.ActualBits
	}
}
