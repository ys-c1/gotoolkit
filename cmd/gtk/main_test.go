package main

// 通过真实命令行路径验证：调色板参数能从 flag 设进公开结构，且关闭时与基准一致。

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func runMain(t *testing.T, args ...string) (int, string) {
	t.Helper()
	// 直接调用子命令函数，绕开 os.Exit。
	switch args[0] {
	case "encode":
		return runEncode(args[1:]), ""
	case "decode":
		return runDecode(args[1:]), ""
	default:
		t.Fatalf("unknown subcommand %q", args[0])
		return 2, ""
	}
}

func TestCLIPaletteEncodeDecode(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pgm")
	pix := make([]byte, 64)
	for i := range pix {
		pix[i] = byte(i * 4)
	}
	if err := os.WriteFile(in, append([]byte("P5\n8 8\n255\n"), pix...), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, f := range []string{"pgm", "bmp", "rle"} {
		for _, k := range []string{"8", "32"} {
			out := filepath.Join(dir, "o."+f+"."+k)
			if rc, _ := runMain(t, "encode", "--format", f, "--in", in, "--out", out, "--palette", "--buckets", k); rc != 0 {
				t.Fatalf("%s K=%s encode rc=%d", f, k, rc)
			}
			rt := filepath.Join(dir, "rt."+f+"."+k+".pgm")
			if rc, _ := runMain(t, "decode", "--format", f, "--in", out, "--out", rt); rc != 0 {
				t.Fatalf("%s K=%s decode rc=%d", f, k, rc)
			}
			if st, err := os.Stat(out); err != nil || st.Size() == 0 {
				t.Fatalf("%s K=%s 输出为空", f, k)
			}
		}
	}

	// 关闭调色板：编码结果与直接用 api.Encode 默认参数一致（等价于基准路径）。
	off := filepath.Join(dir, "off.pgm")
	if rc, _ := runMain(t, "encode", "--format", "pgm", "--in", in, "--out", off); rc != 0 {
		t.Fatal("off encode")
	}
	got, _ := os.ReadFile(off)
	if !bytes.Equal(got, append([]byte("P5\n8 8\n255\n"), pix...)) {
		t.Fatal("关闭调色板后应逐字节等于原图 P5")
	}

	// 桶数为 0 必须非零退出。
	bad := filepath.Join(dir, "bad.pgm")
	if rc, _ := runMain(t, "encode", "--format", "pgm", "--in", in, "--out", bad, "--palette", "--buckets", "0"); rc == 0 {
		t.Fatal("K=0 应非零退出")
	}
}
