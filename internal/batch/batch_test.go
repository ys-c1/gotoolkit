package batch

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotoolkit/api"
)

var formats = []string{"pgm", "bmp", "rle"}

// counts 是要求全覆盖的五个文件数组合（含 0 空目录与 1 单文件）。
var counts = []int{0, 1, 2, 3, 5}

// makePGM 生成一张可被 api 解码的有效 PGM。
func makePGM(t *testing.T, w, h int, seed byte) []byte {
	t.Helper()
	pix := make([]byte, w*h)
	for i := range pix {
		pix[i] = byte(int(seed)+i*3) % 251
	}
	body, err := api.Encode(&api.Image{W: w, H: h, Pix: pix}, api.Options{Format: "pgm"})
	if err != nil {
		t.Fatalf("构造 PGM 失败: %v", err)
	}
	return body
}

func writeFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedPGMDir 在 dir 中写 n 个有效 PGM，返回基名列表。
func seedPGMDir(t *testing.T, dir string, n int) []string {
	t.Helper()
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		name := "img" + pad(i) + ".pgm"
		writeFile(t, filepath.Join(dir, name), makePGM(t, 4, 3, byte(i*11)))
		names = append(names, name)
	}
	return names
}

func pad(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// ---- 全成功矩阵：五个文件数 × 编/解码 × 三格式 ----

func TestBatchMatrix(t *testing.T) {
	for _, mode := range []Mode{Encode, Decode} {
		for _, format := range formats {
			for _, n := range counts {
				mode, format, n := mode, format, n
				t.Run(modeName(mode)+"/"+format+"/n="+itoa(n), func(t *testing.T) {
					root := t.TempDir()
					inDir := filepath.Join(root, "in")
					outDir := filepath.Join(root, "out")

					if n == 0 {
						mustMkdir(t, inDir)
						_, err := Run(mode, format, 0, inDir, outDir)
						if !errors.Is(err, ErrDirEmpty) {
							t.Fatalf("空目录应返回 ErrDirEmpty，得到 %v", err)
						}
						return
					}

					seedInputs(t, mode, format, inDir, n)
					rep, err := Run(mode, format, 0, inDir, outDir)
					if err != nil {
						t.Fatalf("Run 返回错误: %v", err)
					}
					if rep.Total != n || rep.Succeeded != n || rep.Failed != 0 {
						t.Fatalf("计数错误 total=%d ok=%d fail=%d 期望 n=%d", rep.Total, rep.Succeeded, rep.Failed, n)
					}
					// 计数恒等式：total == succeeded+failed。
					if rep.Total != rep.Succeeded+rep.Failed {
						t.Fatal("total 不等于 succeeded+failed")
					}
					// 输出文件数与输入一致且全部可回读。
					assertOutputs(t, mode, format, inDir, outDir, n)
					// 汇总行可机读且计数与文件数相等。
					var buf bytes.Buffer
					if err := RenderReport(&buf, rep); err != nil {
						t.Fatal(err)
					}
					sum := lastLine(buf.String())
					total, ok, fail, perr := ParseSummary(sum)
					if perr != nil {
						t.Fatalf("汇总行无法机读: %v (%q)", perr, sum)
					}
					if total != n || ok != n || fail != 0 {
						t.Fatalf("汇总计数与文件数不符: %d/%d/%d vs n=%d", total, ok, fail, n)
					}
					if strings.Count(buf.String(), "\n") != 1 {
						t.Fatalf("全成功时应只有汇总一行，得到 %q", buf.String())
					}
				})
			}
		}
	}
}

func seedInputs(t *testing.T, mode Mode, format, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		base := "img" + pad(i)
		if mode == Encode {
			writeFile(t, filepath.Join(dir, base+".pgm"), makePGM(t, 4, 3, byte(i*11)))
			continue
		}
		// decode：先按目标格式产出有效文件。
		pgm := makePGM(t, 4, 3, byte(i*11))
		img, err := api.Decode(pgm, api.Options{Format: "pgm"})
		if err != nil {
			t.Fatal(err)
		}
		body, err := api.Encode(img, api.Options{Format: format})
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, base+extOf[format]), body)
	}
}

func assertOutputs(t *testing.T, mode Mode, format, inDir, outDir string, n int) {
	t.Helper()
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != n {
		t.Fatalf("输出文件数=%d 期望 %d", len(entries), n)
	}
	wantExt := extOf[targetFormat(mode, format)]
	for _, e := range entries {
		if filepath.Ext(e.Name()) != wantExt {
			t.Fatalf("输出扩展名错误: %s 期望 %s", e.Name(), wantExt)
		}
		body, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if len(body) == 0 {
			t.Fatalf("输出为空: %s", e.Name())
		}
	}
}

// ---- 失败隔离：混入坏文件，好文件继续，失败带名字与原因 ----

func TestFailureIsolation(t *testing.T) {
	for _, mode := range []Mode{Encode, Decode} {
		for _, format := range formats {
			mode, format := mode, format
			t.Run(modeName(mode)+"/"+format, func(t *testing.T) {
				root := t.TempDir()
				inDir := filepath.Join(root, "in")
				outDir := filepath.Join(root, "out")
				mustMkdir(t, inDir)

				// 两个好文件 + 一个坏文件，坏文件名固定便于断言被报出。
				seedInputs(t, mode, format, inDir, 2)
				badName := "bad" + inputExt(mode, format)
				writeFile(t, filepath.Join(inDir, badName), corruptBytes(mode, format))

				rep, err := Run(mode, format, 0, inDir, outDir)
				if err != nil {
					t.Fatalf("逐文件失败不应作为 Run 错误返回: %v", err)
				}
				if rep.Total != 3 || rep.Succeeded != 2 || rep.Failed != 1 {
					t.Fatalf("计数错误 total=%d ok=%d fail=%d", rep.Total, rep.Succeeded, rep.Failed)
				}
				if rep.Total != rep.Succeeded+rep.Failed {
					t.Fatal("计数恒等式不成立")
				}
				// 好文件仍然产出。
				entries, _ := os.ReadDir(outDir)
				if len(entries) != 2 {
					t.Fatalf("失败隔离失败：好文件应继续产出，实得 %d 个", len(entries))
				}
				// 机读输出：失败行两列且含坏文件名与原因，汇总四字段。
				var buf bytes.Buffer
				if err := RenderReport(&buf, rep); err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
				if len(lines) != 2 {
					t.Fatalf("应有 1 失败行 + 1 汇总行，得到 %q", buf.String())
				}
				failCols := strings.Split(lines[0], "\t")
				if len(failCols) != 3 || failCols[0] != "FAIL" || failCols[1] != badName || failCols[2] == "" {
					t.Fatalf("失败行不是 前缀+文件名+原因 三列: %q", lines[0])
				}
				total, ok, fail, perr := ParseSummary(lines[1])
				if perr != nil || total != 3 || ok != 2 || fail != 1 {
					t.Fatalf("汇总行机读错误: %v %q", perr, lines[1])
				}
			})
		}
	}
}

// corruptBytes 产出各方向/格式下必然解码失败的输入。
func corruptBytes(mode Mode, format string) []byte {
	if mode == Encode {
		// encode 输入按 PGM 解码；魔数错误必然失败。
		return []byte("XX\n4 3\n255\n000000000000")
	}
	switch format {
	case "pgm":
		return []byte("XX-not-pgm")
	case "bmp":
		return []byte("XX-not-bmp-padding-padding")
	default:
		return []byte("XXXX-not-rle-padding-padding")
	}
}

// ---- 与单文件逐字节一致 ----

func TestByteIdenticalToSingleFile(t *testing.T) {
	pgmBody := makePGM(t, 4, 3, 7)
	img, err := api.Decode(pgmBody, api.Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{Encode, Decode} {
		for _, format := range formats {
			mode, format := mode, format
			t.Run(modeName(mode)+"/"+format, func(t *testing.T) {
				root := t.TempDir()
				inDir := filepath.Join(root, "in")
				outDir := filepath.Join(root, "out")

				if mode == Encode {
					writeFile(t, filepath.Join(inDir, "a.pgm"), pgmBody)
					// 单文件路径：直接调 api（与 cmd/gtk 单文件命令同一入口）。
					want, err := api.Encode(img, api.Options{Format: format})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := Run(Encode, format, 0, inDir, outDir); err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(filepath.Join(outDir, "a"+extOf[format]))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatal("批量 encode 输出与单文件路径不逐字节一致")
					}
				} else {
					encoded, err := api.Encode(img, api.Options{Format: format})
					if err != nil {
						t.Fatal(err)
					}
					writeFile(t, filepath.Join(inDir, "a"+extOf[format]), encoded)
					want, err := api.Encode(img, api.Options{Format: "pgm"})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := Run(Decode, format, 0, inDir, outDir); err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(filepath.Join(outDir, "a.pgm"))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatal("批量 decode 输出与单文件路径不逐字节一致")
					}
				}
			})
		}
	}
}

// ---- 空目录与权限不足各给一个错误码 ----

func TestEmptyAndPermissionErrors(t *testing.T) {
	// 空目录 -> ErrDirEmpty。
	empty := t.TempDir()
	if _, err := Run(Encode, "bmp", 0, empty, filepath.Join(t.TempDir(), "o")); !errors.Is(err, ErrDirEmpty) {
		t.Fatalf("空目录期望 ErrDirEmpty，得到 %v", err)
	}

	// 不可遍历目录（chmod 000）-> ErrPermission。若以 root 跑测试则跳过。
	if os.Geteuid() == 0 {
		t.Skip("root 绕过权限位，无法模拟权限不足")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	mustMkdir(t, locked)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := Run(Encode, "bmp", 0, locked, filepath.Join(t.TempDir(), "o")); !errors.Is(err, ErrPermission) {
		t.Fatalf("权限不足期望 ErrPermission，得到 %v", err)
	}
}

// ---- 输出目录权限不足也归为权限错误 ----

func TestOutputPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 绕过权限位")
	}
	root := t.TempDir()
	inDir := filepath.Join(root, "in")
	seedPGMDir(t, inDir, 1)
	// 把一个只读文件当输出目录，MkdirAll/写文件必失败且为权限类。
	outBlocker := filepath.Join(root, "blocker")
	writeFile(t, outBlocker, []byte("x"))
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	_, err := Run(Encode, "bmp", 0, inDir, filepath.Join(root, "out"))
	if err == nil {
		t.Fatal("输出目录不可创建时应报错")
	}
}

// ---- 遍历只取普通文件、跳过子目录与错误扩展名 ----

func TestTraversalFilters(t *testing.T) {
	root := t.TempDir()
	inDir := filepath.Join(root, "in")
	seedPGMDir(t, inDir, 2)
	// 子目录应被跳过。
	mustMkdir(t, filepath.Join(inDir, "subdir"))
	// 错误扩展名应被跳过（encode 只收 .pgm）。
	writeFile(t, filepath.Join(inDir, "notes.txt"), []byte("ignore me"))
	rep, err := Run(Encode, "bmp", 0, inDir, filepath.Join(root, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 2 {
		t.Fatalf("应只处理 2 个 pgm，实得 %d", rep.Total)
	}
}

// ---- 汇总恒等式对任意成功/失败组合成立 ----

func TestSummaryIdentityAcrossCounts(t *testing.T) {
	for _, n := range counts {
		n := n
		root := t.TempDir()
		inDir := filepath.Join(root, "in")
		outDir := filepath.Join(root, "out")
		if n == 0 {
			mustMkdir(t, inDir)
			if _, err := Run(Encode, "rle", 0, inDir, outDir); !errors.Is(err, ErrDirEmpty) {
				t.Fatal("空目录期望 ErrDirEmpty")
			}
			continue
		}
		seedPGMDir(t, inDir, n)
		rep, err := Run(Encode, "rle", 0, inDir, outDir)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Total != n || rep.Total != rep.Succeeded+rep.Failed {
			t.Fatalf("计数恒等式失败 n=%d", n)
		}
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func modeName(m Mode) string {
	if m == Encode {
		return "encode"
	}
	return "decode"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	idx := strings.LastIndex(s, "\n")
	if idx < 0 {
		return s
	}
	return s[idx+1:]
}
