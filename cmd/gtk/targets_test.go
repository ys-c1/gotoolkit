package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gotoolkit/api"
)

// 端到端：targets 子命令在内置图上应产出 T1..T4 四条记录且全部落入区间，
// 外加一条 gap 探针；进程返回 0。
func TestRunTargetsFourTiers(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec.jsonl")
	rc := runTargets([]string{"--record", rec, "--outdir", dir, "--no-color"})
	if rc != 0 {
		t.Fatalf("runTargets 返回 %d", rc)
	}

	f, err := os.Open(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	seen := map[string]bool{}
	inRange := map[string]bool{}
	var probes int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var row struct {
			Tier    string `json:"tier"`
			InRange bool   `json:"in_range"`
			Probe   string `json:"probe"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Probe != "" {
			probes++
			continue
		}
		if row.Tier != "" {
			seen[row.Tier] = true
			inRange[row.Tier] = row.InRange
		}
	}
	for _, k := range []string{"T1", "T2", "T3", "T4"} {
		if !seen[k] {
			t.Fatalf("缺少档位记录 %s", k)
		}
		if !inRange[k] {
			t.Fatalf("档位 %s 在内置 256 图上应落入目标区间", k)
		}
	}
	if probes != 1 {
		t.Fatalf("应有 1 条空档探针记录，实际 %d", probes)
	}
	for _, k := range []string{"tier1-T1.blk", "tier4-T4.blk", "tier1-T1.pgm"} {
		if _, err := os.Stat(filepath.Join(dir, k)); err != nil {
			t.Fatalf("产物 %s 缺失: %v", k, err)
		}
	}
}

// encode 带目标区间时应走反解路径并成功落盘；不带目标时行为不变。
func TestRunEncodeTargetAndDefault(t *testing.T) {
	dir := t.TempDir()
	src := &api.Image{W: 16, H: 16, Pix: make([]byte, 256)}
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	pgm, err := api.Encode(src, api.Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	inPath := filepath.Join(dir, "a.pgm")
	if err := os.WriteFile(inPath, pgm, 0o644); err != nil {
		t.Fatal(err)
	}

	targetOut := filepath.Join(dir, "a.blk")
	if rc := runEncode([]string{
		"--format", "blk", "--in", inPath, "--out", targetOut,
		"--target-min", "2000", "--target-max", "9000",
	}); rc != 0 {
		t.Fatalf("目标反解 encode 返回 %d", rc)
	}
	if st, err := os.Stat(targetOut); err != nil || st.Size() == 0 {
		t.Fatalf("目标编码产物异常: %v size=%d", err, func() int64 {
			if st != nil {
				return st.Size()
			}
			return -1
		}())
	}

	// 不带目标：传统 rle 路径仍可用。
	rleOut := filepath.Join(dir, "a.rle")
	if rc := runEncode([]string{"--format", "rle", "--in", inPath, "--out", rleOut}); rc != 0 {
		t.Fatalf("默认 encode 返回 %d", rc)
	}
	if _, err := os.Stat(rleOut); err != nil {
		t.Fatal(err)
	}
}
