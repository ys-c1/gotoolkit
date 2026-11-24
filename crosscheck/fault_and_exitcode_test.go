package crosscheck

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gotoolkit/api"
)

// TestLocalizationWithInjectedFault 故障注入自检：
// 正常情况下九组全一致，无法证明“分段直通 + 差异图”真能抓住分叉。
// 这里手动把 CLI 转储出来的某个环节字节改坏，模拟该环节产生差异，
// 验证 compareStages 能把差异定位到被注入的环节，writeDiffMap 能产出差异图。
func TestLocalizationWithInjectedFault(t *testing.T) {
	bin := buildCLI(t)
	input := samplePGM()
	root := t.TempDir()
	inPath := filepath.Join(root, "in.pgm")
	if err := os.WriteFile(inPath, input, 0o644); err != nil {
		t.Fatal(err)
	}

	libStages, err := api.PipeStages(input, "encode", "rle", 50)
	if err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(root, "cli.bin")
	rc, _, stderr, dumpDir := runCLI(t, bin,
		[]string{"encode", "--format", "rle", "--gain", "50", "--in", inPath, "--out", outPath})
	if rc != 0 {
		t.Fatalf("CLI 运行失败 rc=%d stderr=%s", rc, stderr)
	}

	// 注入故障：篡改环节3（格式编码）转储的第 3 个字节。
	faultStage := api.StageFormatEnc
	faultFile := filepath.Join(dumpDir, faultStage+".bin")
	body, err := os.ReadFile(faultFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) <= 3 {
		t.Fatal("环节3产物过短，无法注入")
	}
	body[3] ^= 0xFF
	if err := os.WriteFile(faultFile, body, 0o644); err != nil {
		t.Fatal(err)
	}

	got := compareStages(t, libStages, dumpDir)
	if got != faultStage {
		t.Fatalf("定位失败: 期望首个分叉环节=%s，实际=%s", faultStage, got)
	}

	// 差异图必须落盘且非空，且要真实包含被注入的偏移 3。
	// 注意：被篡改的是转储环节文件，CLI 当初写盘的输出仍然正确；
	// 因此这里按同一故障（偏移3翻转）构造“坏输出”来检验差异图生成路径。
	cliOut, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	libOut := libStages[len(libStages)-1].Body
	faulty := append([]byte(nil), cliOut...)
	faulty[3] ^= 0xFF
	if diffBytes(faulty, libOut) != 1 {
		t.Fatalf("注入故障后应恰好 1 个差异字节，实际 %d", diffBytes(faulty, libOut))
	}
	svg, txt := writeDiffMap(t, diffMapDir(t), "injected", faulty, libOut)
	for _, p := range []string{svg, txt} {
		info, err := os.Stat(p)
		if err != nil || info.Size() == 0 {
			t.Fatalf("差异图未生成或为空: %s", p)
		}
	}
	list, err := os.ReadFile(txt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(list, []byte("偏移     3")) {
		t.Fatalf("差异清单未包含注入偏移 3:\n%s", list)
	}
	t.Logf("故障注入自检通过：分叉定位在 %s，差异图 %s %s", got, svg, txt)
}

// TestExitCodeMapping 命令行退出码与库返回值的对应关系。
//
// 库（api 包）以 (值, error) 返回，CLI 把错误映射成进程退出码：
//
//	库返回 nil error            -> CLI 退出码 0（成功，产物已写盘）
//	flag 解析失败/缺 --in/--out -> CLI 退出码 2（用法错误，库根本未被调用）
//	库返回非 nil error（读盘后） -> CLI 退出码 1（运行失败，含 ErrBadSize、
//	                               ErrUnknownFormat、ErrBadOption、ErrTruncated 等）
//
// 本表逐行验证该映射；库侧的 error 类型在此一并断言，保证文档与代码一致。
func TestExitCodeMapping(t *testing.T) {
	bin := buildCLI(t)
	root := t.TempDir()
	inPath := filepath.Join(root, "in.pgm")
	if err := os.WriteFile(inPath, samplePGM(), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(root, "out.bin")

	type row struct {
		name    string
		args    []string
		wantRC  int
		libErr  error // 对应库调用应当返回的错误；nil 表示成功
		libCall func() error
	}

	// 库侧等价调用，错误类型与 CLI 场景一一对应。
	img := mustDecodePGM(t, samplePGM())
	rows := []row{
		{
			name:   "成功：库 nil error ↔ CLI 0",
			args:   []string{"encode", "--format", "bmp", "--in", inPath, "--out", outPath},
			wantRC: 0,
			libCall: func() error {
				_, e := api.Encode(img, api.Options{Format: "bmp"})
				return e
			},
		},
		{
			name:    "用法错误：未知 flag ↔ CLI 2（库未被调用）",
			args:    []string{"encode", "--bogus", "--in", inPath, "--out", outPath},
			wantRC:  2,
			libCall: func() error { return nil }, // 此路径库不会被调用
		},
		{
			name:    "用法错误：缺少 --out ↔ CLI 2（库未被调用）",
			args:    []string{"encode", "--format", "bmp", "--in", inPath},
			wantRC:  2,
			libCall: func() error { return nil },
		},
		{
			name:   "运行失败：未知格式 ErrUnknownFormat ↔ CLI 1",
			args:   []string{"encode", "--format", "tga", "--in", inPath, "--out", outPath},
			wantRC: 1,
			libCall: func() error {
				_, e := api.Encode(img, api.Options{Format: "tga"})
				return e
			},
			libErr: api.ErrUnknownFormat,
		},
		{
			name:   "运行失败：增益越界 ErrBadOption ↔ CLI 1",
			args:   []string{"encode", "--format", "pgm", "--gain", "500", "--in", inPath, "--out", outPath},
			wantRC: 1,
			libCall: func() error {
				_, e := api.Encode(img, api.Options{Format: "pgm", Gain: 500})
				return e
			},
			libErr: api.ErrBadOption,
		},
		{
			name:   "运行失败：空输入 ErrTruncated ↔ CLI 1",
			args:   []string{"decode", "--format", "pgm", "--in", filepath.Join(root, "empty.bin"), "--out", outPath},
			wantRC: 1,
			libCall: func() error {
				_, e := api.Decode(nil, api.Options{Format: "pgm"})
				return e
			},
			libErr: api.ErrTruncated,
		},
		{
			name:    "运行失败：输入文件不存在（读盘错误）↔ CLI 1",
			args:    []string{"encode", "--format", "pgm", "--in", filepath.Join(root, "no_such.pgm"), "--out", outPath},
			wantRC:  1,
			libCall: func() error { return nil }, // 库前的 IO 错误，无对应库 error
		},
	}
	if err := os.WriteFile(filepath.Join(root, "empty.bin"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			rc, _, stderr, _ := runCLI(t, bin, r.args)
			if rc != r.wantRC {
				t.Fatalf("退出码: 期望 %d，实际 %d，stderr=%s", r.wantRC, rc, stderr)
			}
			libErr := r.libCall()
			if r.libErr != nil {
				if libErr == nil {
					t.Fatalf("库侧应返回错误，实际 nil")
				}
				if !errors.Is(libErr, r.libErr) {
					t.Fatalf("库错误类型: 期望包裹 %v，实际 %v", r.libErr, libErr)
				}
			} else if r.wantRC == 1 && libErr != nil {
				t.Fatalf("IO 类失败库侧不应有对应 error，实际 %v", libErr)
			}
			t.Logf("OK rc=%d 库error=%v | %s", rc, libErr, r.name)
		})
	}
}
