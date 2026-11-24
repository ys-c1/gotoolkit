package guard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "ref", "main.go")); err != nil {
		t.Skipf("缺参照目录: %v", err)
	}
	return root
}

func TestCompareBytes(t *testing.T) {
	cases := []struct {
		name     string
		golden   []byte
		fresh    []byte
		equal    bool
		first    int
		expected byte
		actual   byte
	}{
		{"相同", []byte{1, 2, 3}, []byte{1, 2, 3}, true, -1, 0, 0},
		{"首个差异", []byte{1, 2, 3}, []byte{1, 9, 3}, false, 1, 2, 9},
		{"期望侧更短", []byte{1, 2}, []byte{1, 2, 3}, false, 2, 0, 3},
		{"实际侧更短", []byte{1, 2, 3}, []byte{1, 2}, false, 2, 3, 0},
		{"空对空", nil, nil, true, -1, 0, 0},
		{"空对非空", nil, []byte{7}, false, 0, 0, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := CompareBytes(c.golden, c.fresh)
			if v.Equal != c.equal || v.FirstDiff != c.first {
				t.Fatalf("Equal=%v FirstDiff=%d，期望 %v/%d", v.Equal, v.FirstDiff, c.equal, c.first)
			}
			if !c.equal && (v.Expected != c.expected || v.Actual != c.actual) {
				t.Fatalf("差异值 %d/%d，期望 %d/%d", v.Expected, v.Actual, c.expected, c.actual)
			}
		})
	}
}

func TestPixelCompare(t *testing.T) {
	a := []byte{0, 1, 2, 3, 4, 5}
	b := []byte{0, 9, 2, 3, 8, 5}
	d := PixelCompare(3, 2, a, 3, 2, b)
	if d.Equal || d.Count != 2 || d.FirstX != 1 || d.FirstY != 0 {
		t.Fatalf("差异统计错误: %+v", d)
	}
	if d.FirstGolden != 1 || d.FirstRef != 9 || d.Total != 6 {
		t.Fatalf("首个差异或总数错误: %+v", d)
	}
	if c := d.Changes(); len(c) != 2 || c[0].Count != 1 {
		t.Fatalf("替换聚合错误: %+v", c)
	}
	d2 := PixelCompare(2, 2, []byte{1, 2, 3, 4}, 3, 2, []byte{1, 2, 9, 3, 4, 9})
	if !d2.SizeDiffer || d2.Count != 0 || d2.Equal {
		t.Fatalf("画幅差异处理错误: %+v", d2)
	}
}

// 干净仓库：基准对照与实现对照都必须过。
func TestGuardCleanRepo(t *testing.T) {
	root := repoRoot(t)
	before := goldenBodies(t, filepath.Join(root, "testdata", "golden"))
	out := t.TempDir()

	rep, err := Check(filepath.Join(root, "ref"), filepath.Join(root, "testdata", "golden"), out)
	if err != nil {
		t.Fatalf("干净仓库应通过: %v", err)
	}
	if len(rep.Drift) != 0 {
		t.Fatalf("不应有漂移: %v", rep.Drift)
	}
	for _, r := range rep.Results {
		if len(r.Pairs) != 2 {
			t.Fatalf("%s 应有两组对照", r.Format)
		}
		for _, p := range r.Pairs {
			if !p.Equal() {
				t.Fatalf("%s/%s 应对齐", r.Format, p.Name)
			}
			if p.Dump == nil || !p.Dump.Diff.Equal {
				t.Fatalf("%s/%s 像素应一致", r.Format, p.Name)
			}
			if _, err := os.Stat(p.Dump.SidePath); err != nil {
				t.Fatalf("%s/%s 并排图不存在: %v", r.Format, p.Name, err)
			}
		}
	}
	var report bytes.Buffer
	if err := WriteReport(&report, rep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"字节: 一致", "像素: 一致", "结论:"} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("报告缺 %q:\n%s", want, report.String())
		}
	}
	if after := goldenBodies(t, filepath.Join(root, "testdata", "golden")); !equalMaps(before, after) {
		t.Fatal("守卫改动了只读基准")
	}
}

// 实现漂移：基准没动，但实现产出了不同字节，守卫必须抓到并给出像素差异。
func TestGuardImplDrift(t *testing.T) {
	root := repoRoot(t)
	broken := func(format string) ([]byte, error) {
		b, err := EncodeSample(format)
		if err != nil {
			return nil, err
		}
		if format == "pgm" {
			b[12] ^= 0x5a
		}
		return b, nil
	}
	rep, err := CheckWith(filepath.Join(root, "ref"), filepath.Join(root, "testdata", "golden"), t.TempDir(), broken)
	if err == nil {
		t.Fatal("实现漂移必须非零返回")
	}
	var out bytes.Buffer
	if err := WriteReport(&out, rep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[实现]", "首个差异 @ 12", "像素: 1/12 格不同"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("报告缺 %q:\n%s", want, out.String())
		}
	}
	// 基准对照不应被实现带偏。
	for _, r := range rep.Results {
		if r.Pairs[0].Name == "基准" && !r.Pairs[0].Equal() {
			t.Fatalf("基准对照不应失败: %s", r.Format)
		}
	}
}

// 三类坏样本：截断、篡改、空文件。
func TestGuardBadSamples(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		name   string
		break_ func(t *testing.T, dir string)
		want   string
	}{
		{"截断", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "bmp.bin")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b[:len(b)-7], 0o644); err != nil {
				t.Fatal(err)
			}
		}, "期望侧在该偏移已结束"},
		{"篡改", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "pgm.bin")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			b[12] ^= 0x55
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "像素:"},
		{"空文件", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "rle.bin"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "期望侧在该偏移已结束"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := t.TempDir()
			copyGoldens(t, filepath.Join(root, "testdata", "golden"), g)
			c.break_(t, g)
			rep, err := Check(filepath.Join(root, "ref"), g, t.TempDir())
			if err == nil {
				t.Fatal("坏样本必须非零返回")
			}
			var out bytes.Buffer
			if err := WriteReport(&out, rep); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{c.want, "漂移"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("报告缺 %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestGuardMissingGolden(t *testing.T) {
	root := repoRoot(t)
	if _, err := Check(filepath.Join(root, "ref"), t.TempDir(), ""); err == nil {
		t.Fatal("缺基准必须非零返回")
	}
}

// 实现编码器出错也只能是非零，不能被当成"跳过"。
func TestGuardEncoderError(t *testing.T) {
	root := repoRoot(t)
	_, err := CheckWith(filepath.Join(root, "ref"), filepath.Join(root, "testdata", "golden"), "", func(string) ([]byte, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("实现编码失败必须非零返回")
	}
}

func TestReportBothParts(t *testing.T) {
	v := BytesVerdict{GoldenLen: 3, FreshLen: 4, FirstDiff: 2, Expected: 2, Actual: 3}
	d := &Dump{
		SidePath: "side.png", DiffPath: "diff.png",
		Diff: &PixelDiff{
			GoldenW: 2, GoldenH: 2, FreshW: 2, FreshH: 2,
			Total: 4, Count: 1, FirstX: 0, FirstY: 0, FirstIndex: 0,
			FirstGolden: 1, FirstRef: 2,
			Values: map[uint16]int{1<<8 | 2: 1},
		},
	}
	p := &Pair{Name: "基准", Bytes: v, Dump: d}
	rep := &Report{GoldenDir: "g", RefDir: "r", Drift: []string{"pgm(基准)"},
		Results: []*FileResult{{Format: "pgm", Pairs: []*Pair{p}}}}
	var b strings.Builder
	if err := WriteReport(&b, rep); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"字节: 不一致", "首个差异 @ 2", "像素:", "并排 side.png", "结论: 漂移"} {
		if !strings.Contains(out, want) {
			t.Fatalf("报告缺 %q:\n%s", want, out)
		}
	}
}

func copyGoldens(t *testing.T, src, dst string) {
	t.Helper()
	for _, f := range formats {
		b, err := os.ReadFile(filepath.Join(src, f+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, f+".bin"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func goldenBodies(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, f := range formats {
		b, err := os.ReadFile(filepath.Join(dir, f+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		m[f] = string(b)
	}
	return m
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
