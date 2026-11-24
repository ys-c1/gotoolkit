package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotoolkit/api"
)

// writePGM 写一张 8 位 P5 图，作为 CLI 的输入。
func writePGM(t *testing.T, path string, img *api.Image) {
	t.Helper()
	body, err := api.Encode(img, api.Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func gradient(w, h int) *api.Image {
	img := &api.Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[y*w+x] = uint8(x * 255 / (w - 1))
		}
	}
	return img
}

func captureStdout(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), code
}

// TestCLIEncodeDitherAllFormats 走 encode 子命令：两档强度 × 三种格式，
// 逐项核对产物能被同格式解码回来、且关闭抖动时与基准逐字节一致。
func TestCLIEncodeDitherAllFormats(t *testing.T) {
	dir := t.TempDir()
	src := gradient(64, 16)
	in := filepath.Join(dir, "in.pgm")
	writePGM(t, in, src)

	for _, f := range []string{"pgm", "bmp", "rle"} {
		out := filepath.Join(dir, "plain."+f)
		if code := runEncode([]string{"--format", f, "--in", in, "--out", out}); code != 0 {
			t.Fatalf("%s 关闭抖动编码退出码 %d", f, code)
		}
		plain, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		want, err := api.Encode(src, api.Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain, want) {
			t.Fatalf("%s 关闭抖动时 CLI 产物与库直接编码不一致", f)
		}

		for _, strength := range []string{"0.5", "1"} {
			out := filepath.Join(dir, "d"+strength+"."+f)
			if code := runEncode([]string{"--format", f, "--in", in, "--out", out,
				"--dither-strength", strength, "--dither-kernel", "floyd-steinberg"}); code != 0 {
				t.Fatalf("%s 强度 %s 编码退出码 %d", f, strength, code)
			}
			body, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			img, err := api.Decode(body, api.Options{Format: f})
			if err != nil {
				t.Fatalf("%s 强度 %s 回读失败: %v", f, strength, err)
			}
			if img.W != src.W || img.H != src.H {
				t.Fatalf("%s 画幅变了", f)
			}
			// 抖动产物必须与库直接调用完全一致（CLI 只是参数的搬运工）。
			ref, err := api.Encode(src, api.Options{Format: f, DitherStrength: mustFloat(t, strength), DitherKernel: api.FloydSteinbergKernel()})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, ref) {
				t.Fatalf("%s 强度 %s 的 CLI 产物与库直接编码不一致", f, strength)
			}
		}
	}
}

func mustFloat(t *testing.T, s string) float64 {
	t.Helper()
	switch s {
	case "0.5":
		return 0.5
	case "1":
		return 1
	}
	t.Fatalf("未知强度 %q", s)
	return 0
}

// TestCLIDecodeBothFormats 走 decode 子命令：三格式输入都能解回 PGM，
// 且与原始样本逐字节一致（关闭抖动时）。
func TestCLIDecodeBothFormats(t *testing.T) {
	dir := t.TempDir()
	src := gradient(32, 8)
	inPGM := filepath.Join(dir, "in.pgm")
	writePGM(t, inPGM, src)
	wantPGM, err := os.ReadFile(inPGM)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		enc := filepath.Join(dir, "x."+f)
		if code := runEncode([]string{"--format", f, "--in", inPGM, "--out", enc}); code != 0 {
			t.Fatalf("%s 编码失败", f)
		}
		dec := filepath.Join(dir, "x."+f+".dec.pgm")
		if code := runDecode([]string{"--format", f, "--in", enc, "--out", dec}); code != 0 {
			t.Fatalf("%s 解码失败", f)
		}
		got, err := os.ReadFile(dec)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wantPGM) {
			t.Fatalf("%s 往返后与原始 PGM 不一致", f)
		}
	}
}

// TestCLIDecodeRejectsDitherStrength：decode 也接同一个公开字段，
// 但抖动不可逆，传非零强度要被明确拦下，而不是被静默忽略。
// TestCLIDitherLevels：--dither-levels 也是公开参数的一部分，
// 缺省 0 与显式 8 必须产出同样字节，越界值要被拒。
func TestCLIDitherLevels(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pgm")
	writePGM(t, in, gradient(32, 8))
	a := filepath.Join(dir, "a.pgm")
	b := filepath.Join(dir, "b.pgm")
	if code := runEncode([]string{"--format", "pgm", "--in", in, "--out", a, "--dither-strength", "1"}); code != 0 {
		t.Fatal("缺省色阶数编码失败")
	}
	if code := runEncode([]string{"--format", "pgm", "--in", in, "--out", b, "--dither-strength", "1", "--dither-levels", "8"}); code != 0 {
		t.Fatal("显式色阶数编码失败")
	}
	ba, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if !bytes.Equal(ba, bb) {
		t.Fatal("缺省色阶数与显式 8 的产物不一致")
	}
	for _, bad := range []string{"1", "257", "-3"} {
		if code := runEncode([]string{"--format", "pgm", "--in", in, "--out", a, "--dither-strength", "1", "--dither-levels", bad}); code == 0 {
			t.Fatalf("--dither-levels %s 应被拒", bad)
		}
	}
}

func TestCLIDecodeRejectsDitherStrength(t *testing.T) {
	dir := t.TempDir()
	src := gradient(16, 4)
	in := filepath.Join(dir, "in.pgm")
	writePGM(t, in, src)
	if code := runDecode([]string{"--format", "pgm", "--in", in,
		"--out", filepath.Join(dir, "o.pgm"), "--dither-strength", "1"}); code == 0 {
		t.Fatal("decode 传抖动强度应失败")
	}
}

func TestCLIRejectsBadDitherParams(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pgm")
	writePGM(t, in, gradient(8, 2))
	out := filepath.Join(dir, "o.pgm")
	cases := [][]string{
		{"--dither-strength", "-1", "--dither-kernel", "floyd-steinberg"},
		{"--dither-strength", "0.5", "--dither-kernel", "none"},
		{"--dither-strength", "0.5", "--dither-kernel", "no-such-kernel"},
	}
	for _, extra := range cases {
		args := append([]string{"--format", "pgm", "--in", in, "--out", out}, extra...)
		if code := runEncode(args); code == 0 {
			t.Fatalf("非法抖动参数应失败: %v", extra)
		}
	}
	// 强度为 0 时核留空是合法的（关闭抖动）。
	if code := runEncode([]string{"--format", "pgm", "--in", in, "--out", out, "--dither-kernel", "none"}); code != 0 {
		t.Fatal("强度 0 且无核应成功")
	}
}

func TestCLIKernelsPrintsWeightTable(t *testing.T) {
	text, code := captureStdout(t, runKernels)
	if code != 0 {
		t.Fatalf("kernels 退出码 %d", code)
	}
	for _, needle := range []string{
		"floyd-steinberg", "0.437500", "effectiveSum=1.000000",
		"atkinson", "effectiveSum=0.750000", "归一化会改变其经典行为",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("kernels 输出缺 %q:\n%s", needle, text)
		}
	}
}

func TestCLIEncodePrintKernel(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pgm")
	writePGM(t, in, gradient(16, 4))
	text, code := captureStdout(t, func() int {
		return runEncode([]string{"--format", "pgm", "--in", in,
			"--out", filepath.Join(dir, "o.pgm"),
			"--dither-strength", "1", "--print-kernel"})
	})
	if code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	if !strings.Contains(text, "effectiveSum=1.000000") {
		t.Fatalf("--print-kernel 未打印归一化权重表:\n%s", text)
	}
}
