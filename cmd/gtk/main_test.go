package main

import (
	"os"
	"path/filepath"
	"testing"

	"gotoolkit/api"
)

// 构造一张 4x3 的 PGM 输入文件，供两个动词实测使用。
func writePGM(t *testing.T, dir, name string) string {
	t.Helper()
	pix := make([]uint8, 12)
	for i := range pix {
		pix[i] = uint8(i * 7)
	}
	body, err := api.Encode(&api.Image{W: 4, H: 3, Pix: pix},
		api.Options{Format: "pgm", Divisor: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCLIDivisorBothVerbs：encode/decode 两个动词都要实测 --divisor，
// 三种格式各跑一遍 encode -> decode 闭环。
func TestCLIDivisorBothVerbs(t *testing.T) {
	dir := t.TempDir()
	src := writePGM(t, dir, "src.pgm")

	for _, format := range []string{"pgm", "bmp", "rle"} {
		enc := filepath.Join(dir, "thumb."+format)
		if code := runEncode([]string{
			"--format", format, "--in", src, "--out", enc, "--divisor", "2",
		}); code != 0 {
			t.Fatalf("encode %s --divisor 2 退出码 %d", format, code)
		}
		// 缩略图画幅应为 ceil(4/2)xceil(3/2)=2x2。
		raw, err := os.ReadFile(enc)
		if err != nil {
			t.Fatal(err)
		}
		img, err := api.Decode(raw, api.Options{Format: format, Divisor: 1})
		if err != nil {
			t.Fatalf("%s 缩略图无法解回: %v", format, err)
		}
		if img.W != 2 || img.H != 2 {
			t.Fatalf("%s 缩略图画幅错误: %dx%d", format, img.W, img.H)
		}

		// decode 动词带 --divisor，输出 PGM 同样是 2x2。
		dec := filepath.Join(dir, "back-"+format+".pgm")
		if code := runDecode([]string{
			"--format", format, "--in", enc, "--out", dec, "--divisor", "2",
		}); code != 0 {
			t.Fatalf("decode %s --divisor 2 退出码 %d", format, code)
		}
		back, err := os.ReadFile(dec)
		if err != nil {
			t.Fatal(err)
		}
		pgmImg, err := api.Decode(back, api.Options{Format: "pgm", Divisor: 1})
		if err != nil {
			t.Fatal(err)
		}
		// encode 已缩到 2x2，再 d=2 应为 1x1。
		if pgmImg.W != 1 || pgmImg.H != 1 {
			t.Fatalf("%s decode 侧缩略图错误: %dx%d", format, pgmImg.W, pgmImg.H)
		}
	}

	// 不带 --divisor 时默认 1，输出尺寸保持 4x3。
	full := filepath.Join(dir, "full.pgm")
	if code := runEncode([]string{"--format", "pgm", "--in", src, "--out", full}); code != 0 {
		t.Fatalf("默认 encode 退出码 %d", code)
	}
	raw, _ := os.ReadFile(full)
	img, err := api.Decode(raw, api.Options{Format: "pgm", Divisor: 1})
	if err != nil {
		t.Fatal(err)
	}
	if img.W != 4 || img.H != 3 {
		t.Fatalf("默认 Divisor 应保持原画幅，实际 %dx%d", img.W, img.H)
	}
}

// TestCLIDivisorRejected：零与负数在两个动词上都要以非零退出码拦下。
func TestCLIDivisorRejected(t *testing.T) {
	dir := t.TempDir()
	src := writePGM(t, dir, "src.pgm")
	for _, d := range []string{"0", "-3"} {
		out := filepath.Join(dir, "x-"+d+".bin")
		if code := runEncode([]string{
			"--format", "pgm", "--in", src, "--out", out, "--divisor", d,
		}); code == 0 {
			t.Fatalf("encode --divisor %s 应被拦下", d)
		}
		if code := runDecode([]string{
			"--format", "pgm", "--in", src, "--out", out, "--divisor", d,
		}); code == 0 {
			t.Fatalf("decode --divisor %s 应被拦下", d)
		}
	}
}
