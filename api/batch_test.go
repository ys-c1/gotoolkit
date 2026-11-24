package api

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePGM(t *testing.T, dir, name string, w, h int, pix []uint8) {
	t.Helper()
	body, err := Encode(&Image{W: w, H: h, Pix: pix}, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func samplePix(n int) []uint8 {
	out := make([]uint8, n)
	for i := range out {
		out[i] = uint8(i * 7)
	}
	return out
}

// 三组输入（pgm/bmp/rle 输出）各配一条失败样例。
func TestRunBatchThreeFormatsEachWithFailure(t *testing.T) {
	for _, format := range []string{"pgm", "bmp", "rle"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			inDir := filepath.Join(root, "in")
			outDir := filepath.Join(root, "out")
			report := filepath.Join(root, "report.csv")
			if err := os.MkdirAll(inDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writePGM(t, inDir, "a.pgm", 4, 3, samplePix(12))
			writePGM(t, inDir, "b.pgm", 2, 2, []uint8{1, 9, 9, 2})
			// 失败样例：损坏的 PGM，必须在转码后立刻校验时被剔除。
			if err := os.WriteFile(filepath.Join(inDir, "bad.pgm"), []byte("not a pgm"), 0o644); err != nil {
				t.Fatal(err)
			}

			res, err := RunBatch(BatchOptions{InDir: inDir, OutDir: outDir, Format: format, Report: report})
			if err != nil {
				t.Fatalf("%s 批量失败: %v", format, err)
			}
			if len(res.Rows) != 2 {
				t.Fatalf("%s 报表行数 = %d，期望 2", format, len(res.Rows))
			}
			if len(res.Failed) != 1 || res.Failed[0].Name != "bad.pgm" {
				t.Fatalf("%s 失败清单不符: %+v", format, res.Failed)
			}
			for _, row := range res.Rows {
				if row.Name == "bad.pgm" {
					t.Fatalf("%s 失败文件进入了报表", format)
				}
				if row.Verdict != verdictPass {
					t.Fatalf("%s 校验结论 = %q", format, row.Verdict)
				}
				base := strings.TrimSuffix(row.Name, ".pgm")
				body, err := os.ReadFile(filepath.Join(outDir, base+"."+format))
				if err != nil {
					t.Fatalf("%s 输出缺失: %v", format, err)
				}
				if row.Bytes != len(body) {
					t.Fatalf("%s 字节数 %d != 实际 %d", format, row.Bytes, len(body))
				}
			}
		})
	}
}

// 报表字段顺序固定、不许省略。
func TestReportHeaderOrder(t *testing.T) {
	root := t.TempDir()
	inDir := filepath.Join(root, "in")
	outDir := filepath.Join(root, "out")
	report := filepath.Join(root, "r.csv")
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePGM(t, inDir, "a.pgm", 2, 2, []uint8{1, 2, 3, 4})
	if _, err := RunBatch(BatchOptions{InDir: inDir, OutDir: outDir, Report: report}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if lines[0] != "文件名,字节数,校验结论" {
		t.Fatalf("表头 = %q", lines[0])
	}
	if lines[1] != "a.pgm,15,通过" {
		t.Fatalf("数据行 = %q", lines[1])
	}
}

// 目录不合法给 ErrBadDir。
func TestRunBatchBadDir(t *testing.T) {
	root := t.TempDir()
	report := filepath.Join(root, "r.csv")
	_, err := RunBatch(BatchOptions{
		InDir:  filepath.Join(root, "nope"),
		OutDir: filepath.Join(root, "out"),
		Report: report,
	})
	if !errors.Is(err, ErrBadDir) {
		t.Fatalf("期望 ErrBadDir，得到 %v", err)
	}
}

// 报表写入失败给 ErrReportWrite。
func TestRunBatchReportWriteFails(t *testing.T) {
	root := t.TempDir()
	inDir := filepath.Join(root, "in")
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePGM(t, inDir, "a.pgm", 2, 2, []uint8{1, 2, 3, 4})
	_, err := RunBatch(BatchOptions{
		InDir:  inDir,
		OutDir: filepath.Join(root, "out"),
		Report: filepath.Join(root, "missing", "r.csv"),
	})
	if !errors.Is(err, ErrReportWrite) {
		t.Fatalf("期望 ErrReportWrite，得到 %v", err)
	}
}

// 报表读回校验失败要带行号与字段名。
func TestReportVerifyErrorHasLineAndField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.csv")
	if err := os.WriteFile(path, []byte("文件名,字节数,校验结论\na.pgm,15,通过\nb.pgm,x,通过\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := []BatchRow{
		{Name: "a.pgm", Bytes: 15, Verdict: "通过"},
		{Name: "b.pgm", Bytes: 20, Verdict: "通过"},
	}
	err := verifyReportFile(path, want)
	var ve *ReportVerifyError
	if !errors.As(err, &ve) {
		t.Fatalf("期望 ReportVerifyError，得到 %v", err)
	}
	if ve.Line != 3 || ve.Field != "字节数" {
		t.Fatalf("行号/字段 = %d/%q", ve.Line, ve.Field)
	}
}

// 报表读回后必须与内存版本逐字段对上。
func TestReportVerifyDetectsMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.csv")
	body := "文件名,字节数,校验结论\na.pgm,19,通过\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	want := []BatchRow{{Name: "a.pgm", Bytes: 19, Verdict: "失败"}}
	err := verifyReportFile(path, want)
	var ve *ReportVerifyError
	if !errors.As(err, &ve) || ve.Line != 2 || ve.Field != "校验结论" {
		t.Fatalf("期望第 2 行「校验结论」不一致，得到 %v", err)
	}
}

// 单文件模式结果与既有基准逐字节相同；批量输出也复用同一编码。
func TestSingleFileUnchangedAndBatchMatches(t *testing.T) {
	img := &Image{W: 4, H: 3, Pix: samplePix(12)}
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
			t.Fatalf("%s 单文件编码与基准不一致", f)
		}

		root := t.TempDir()
		inDir := filepath.Join(root, "in")
		outDir := filepath.Join(root, "out")
		if err := os.MkdirAll(inDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePGM(t, inDir, "a.pgm", 4, 3, samplePix(12))
		if _, err := RunBatch(BatchOptions{InDir: inDir, OutDir: outDir, Format: f, Report: filepath.Join(root, "r.csv")}); err != nil {
			t.Fatal(err)
		}
		batchOut, err := os.ReadFile(filepath.Join(outDir, "a."+f))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(batchOut, want) {
			t.Fatalf("%s 批量输出与单文件基准不一致", f)
		}
	}
}
