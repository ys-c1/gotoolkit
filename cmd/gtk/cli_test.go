package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildCLI 编译 gtk 到临时目录，返回可执行路径。
func buildCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gtk")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 gtk 失败: %v\n%s", err, out)
	}
	return bin
}

// genPGM 写一张确定性 P5 图。
func genPGM(t *testing.T, path string, w, h int) {
	t.Helper()
	buf := make([]byte, 0, 15+w*h)
	buf = append(buf, []byte("P5\n")...)
	buf = append(buf, []byte(itoa(w)+" "+itoa(h)+"\n255\n")...)
	for i := 0; i < w*h; i++ {
		buf = append(buf, byte((i*37+11)%256))
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// TestCLIBudgetStress 进程级黑盒实验：
//  1. 用 GOMEMLIMIT+GOGC=off 把进程压到 ~4MiB 软上限，分块解码必须成功；
//  2. 同一大图走 tight 档输出，与不限预算的原路径输出逐字节相同；
//  3. 直接整图解码（显式大 band 行=全高）在 tight 档被拒绝并指明超限项。
func TestCLIBudgetStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要编译子进程的长测")
	}
	bin := buildCLI(t)
	work := t.TempDir()
	w, h := 400, 900 // 360000 像素，整图输出 360KB，PGM 流约 360KB
	src := filepath.Join(work, "in.pgm")
	genPGM(t, src, w, h)

	run := func(args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Env = append(os.Environ(),
			"GOMEMLIMIT=4MiB",
			"GOGC=off", // 关掉常规 GC 增长，让 GOMEMLIMIT 成为主导约束
		)
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		err := c.Run()
		return out.String(), err
	}

	// 1) 受限进程 + tight 预算：分块解码成功。
	chunkOut := filepath.Join(work, "chunk.pgm")
	log, err := run("decode", "--format", "pgm", "--in", src, "--out", chunkOut, "--mem-budget", "tight")
	if err != nil {
		t.Fatalf("受限进程分块解码应成功: %v\n%s", err, log)
	}
	if !bytes.Contains([]byte(log), []byte("path=chunked")) {
		t.Fatalf("日志未表明分块路径: %s", log)
	}

	// 2) 不限预算的原路径输出，两者逐字节相同（对照实验）。
	wholeOut := filepath.Join(work, "whole.pgm")
	log2, err := run("decode", "--format", "pgm", "--in", src, "--out", wholeOut)
	if err != nil {
		t.Fatalf("原路径解码失败: %v\n%s", err, log2)
	}
	if !bytes.Contains([]byte(log2), []byte("path=whole")) {
		t.Fatalf("日志未表明原路径: %s", log2)
	}
	a, _ := os.ReadFile(chunkOut)
	b, _ := os.ReadFile(wholeOut)
	if !bytes.Equal(a, b) {
		t.Fatal("受限分块输出与原路径输出不逐字节相同")
	}

	// 3) 显式要求全高单块（=整图缓冲）在 tight 档必须被结构化拒绝。
	rejectOut := filepath.Join(work, "reject.pgm")
	log3, err := run("decode", "--format", "pgm", "--in", src, "--out", rejectOut,
		"--mem-budget", "tight", "--band-rows", itoa(h))
	if err == nil {
		t.Fatalf("全高单块超 tight 档应被拒绝，日志: %s", log3)
	}
	if !bytes.Contains([]byte(log3), []byte("内存预算不足")) {
		t.Fatalf("拒绝信息未指明预算不足: %s", log3)
	}
	if !bytes.Contains([]byte(log3), []byte("分块工作集")) &&
		!bytes.Contains([]byte(log3), []byte("输出像素缓冲")) {
		t.Fatalf("拒绝信息未指明超限项: %s", log3)
	}
}
