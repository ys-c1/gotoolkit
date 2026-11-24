package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestReportDeterministicAndStable 运行 lumareport，锁住两件事：
//  1. 两次运行逐字节一致（无时间戳）；
//  2. 关键数字不漂移（系数、三档上界、超限个数、头部差值）。
func TestReportDeterministicAndStable(t *testing.T) {
	dir := t.TempDir()
	out1 := filepath.Join(dir, "a")
	out2 := filepath.Join(dir, "b")
	for _, d := range []string{out1, out2} {
		cmd := exec.Command("go", "run", ".", "-out", d)
		if err := cmd.Run(); err != nil {
			t.Fatalf("运行 lumareport 失败: %v", err)
		}
	}
	b1, err := os.ReadFile(filepath.Join(out1, "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(filepath.Join(out2, "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("两次运行报告不一致")
	}
	must := []string{
		"kr=299000/446966=0.668955",
		"kg=587000/446966=1.313299",
		"kb=114000/446966=0.255053",
		"三档上界分别取档内逐级 Bound 的最大值：1 / 1 / 47 级",
		"共 59 个",
		"差值 BMP−PGM = (54−13) + 行补齐 16 + 2N 544 = 601 字节",
	}
	for _, m := range must {
		if !bytes.Contains(b1, []byte(m)) {
			t.Fatalf("报告缺少关键行: %q", m)
		}
	}
}
