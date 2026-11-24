package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseThresholds(t *testing.T) {
	v, err := parseThresholds("4,7")
	if err != nil || len(v) != 2 || v[0] != 4 || v[1] != 7 {
		t.Fatalf("解析失败: %v %v", v, err)
	}
	if _, err := parseThresholds("999"); err == nil {
		t.Fatal("越界阈值应报错")
	}
	if _, err := parseThresholds("abc"); err == nil {
		t.Fatal("非法阈值应报错")
	}
}

func TestRunBenchWritesConsistentReport(t *testing.T) {
	dir := t.TempDir()
	code := runBench([]string{"--thresholds", "4,7", "--out", dir})
	if code != 0 {
		t.Fatalf("runBench 退出码 %d", code)
	}
	body, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	if err != nil {
		t.Fatalf("report.txt 未写出: %v", err)
	}
	txt := string(body)

	if n := strings.Count(txt, "【阈值结论"); n != 10 {
		t.Fatalf("两个阈值×五档应有 10 行阈值结论，实际 %d", n)
	}
	if n := strings.Count(txt, "·画幅："); n != 10 {
		t.Fatalf("每档每次运行应有独立画幅行，共 10，实际 %d", n)
	}
	if n := strings.Count(txt, "·实际体积（格式=体积，成对）："); n != 10 {
		t.Fatalf("应有 10 行成对实际体积，实际 %d", n)
	}
	if !strings.Contains(txt, "“格式+体积”对一致 5/5") {
		t.Fatal("报告必须给出 5/5 格式+体积对一致结论")
	}
	if !strings.Contains(txt, "τ=4") || !strings.Contains(txt, "τ=7") {
		t.Fatal("必须写清阈值与结论的对应关系")
	}
	// 五档原图与两档出图目录必须存在。
	for _, p := range []string{"orig", "t4", "t7", "report.json"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("缺少产物 %s: %v", p, err)
		}
	}
	// 抽查选中格式与体积成对落盘。
	if fi, err := os.Stat(filepath.Join(dir, "t4", "tier2_32x32_rle.rle")); err != nil || fi.Size() != 940 {
		t.Fatalf("档2 rle 码流体积应为 940, err=%v size=%v", err, fi)
	}
}

func TestRunBenchSingleThreshold(t *testing.T) {
	dir := t.TempDir()
	if code := runBench([]string{"--threshold", "8", "--out", dir, "--no-images"}); code != 0 {
		t.Fatalf("单阈值退出码 %d", code)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "report.") {
			t.Fatal("单阈值不应写双阈值 report 汇总文件")
		}
	}
}
