package crosscheck

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gotoolkit/api"
)

// TestNineGroups 三格式 × 三增益共九组，命令行与库调用逐字节对拍。
//
// 结论表（组号 | 一致与否 | 差异字节数）由本测试逐组打印到测试日志；
// 任一组不一致即测试失败，并把定位环节与差异图路径一并报出。
func TestNineGroups(t *testing.T) {
	bin := buildCLI(t)
	input := samplePGM()
	inPath := filepath.Join(t.TempDir(), "in.pgm")
	if err := os.WriteFile(inPath, input, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Logf("%-4s %-6s %-6s %-8s %-10s %s", "组号", "格式", "增益", "一致", "差异字节", "首个分叉环节")
	for _, f := range formats {
		for _, g := range gains {
			no := groupNo(f, g)
			t.Run(f+"/gain"+itoa(g), func(t *testing.T) {
				// 库调用路径：与 CLI 相同的输入、相同的参数。
				libStages, err := api.PipeStages(input, "encode", f, g)
				if err != nil {
					t.Fatalf("组%d 库调用返回错误: %v", no, err)
				}
				libOut := libStages[len(libStages)-1].Body

				// 命令行路径：同一二进制、同一参数，转储各环节。
				outPath := filepath.Join(t.TempDir(), "cli.bin")
				rc, _, stderr, dumpDir := runCLI(t, bin,
					[]string{"encode", "--format", f, "--gain", itoa(g), "--in", inPath, "--out", outPath})
				if rc != 0 {
					t.Fatalf("组%d CLI 退出码=%d stderr=%s", no, rc, stderr)
				}
				cliOut, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatal(err)
				}

				n := diffBytes(cliOut, libOut)
				match := n == 0
				firstStage := ""
				if !match {
					// 分段直通：找出第一个分叉的环节。
					firstStage = compareStages(t, libStages, dumpDir)
					svg, txt := writeDiffMap(t,
						diffMapDir(t),
						"group"+itoa(no)+"_"+f+"_g"+itoa(g), cliOut, libOut)
					t.Errorf("组%d 不一致，首个分叉环节=%s，差异字节=%d，差异图: %s %s",
						no, firstStage, n, svg, txt)
				}
				t.Logf("%-4d %-6s %-6d %-8t %-10d %s", no, f, g, match, n, firstStage)

				// 每个环节都必须一致：最终产物一致时分段直通也必须全绿。
				if s := compareStages(t, libStages, dumpDir); s != "" {
					t.Fatalf("组%d 最终产物相同但环节 %s 分叉，管线存在隐藏差异", no, s)
				}
				if !bytes.Equal(cliOut, libOut) {
					t.Fatalf("组%d 最终产物逐字节不一致", no)
				}
			})
		}
	}
}

// TestDecodePathNineGroups 对 decode 子命令同样跑九组，保证两条路径在反向增益下也一致。
func TestDecodePathNineGroups(t *testing.T) {
	bin := buildCLI(t)
	input := samplePGM()
	root := t.TempDir()
	inPath := filepath.Join(root, "in.pgm")
	if err := os.WriteFile(inPath, input, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Logf("%-4s %-6s %-6s %-8s %-10s %s", "组号", "格式", "增益", "一致", "差异字节", "首个分叉环节")
	for _, f := range formats {
		// 先得到各格式的中间产物（gain=0 编码）。
		mid, err := api.Encode(mustDecodePGM(t, input), api.Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		midPath := filepath.Join(root, "mid_"+f+".bin")
		if err := os.WriteFile(midPath, mid, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, g := range gains {
			no := groupNo(f, g)
			t.Run(f+"/gain"+itoa(g), func(t *testing.T) {
				libStages, err := api.PipeStages(mid, "decode", f, g)
				if err != nil {
					t.Fatalf("组%d 库调用返回错误: %v", no, err)
				}
				libOut := libStages[len(libStages)-1].Body

				outPath := filepath.Join(t.TempDir(), "cli.pgm")
				rc, _, stderr, dumpDir := runCLI(t, bin,
					[]string{"decode", "--format", f, "--gain", itoa(g), "--in", midPath, "--out", outPath})
				if rc != 0 {
					t.Fatalf("组%d CLI 退出码=%d stderr=%s", no, rc, stderr)
				}
				cliOut, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatal(err)
				}

				n := diffBytes(cliOut, libOut)
				firstStage := ""
				if n != 0 {
					firstStage = compareStages(t, libStages, dumpDir)
					svg, txt := writeDiffMap(t,
						diffMapDir(t),
						"decode_group"+itoa(no)+"_"+f+"_g"+itoa(g), cliOut, libOut)
					t.Errorf("组%d 不一致，首个分叉环节=%s，差异字节=%d，差异图: %s %s",
						no, firstStage, n, svg, txt)
				}
				t.Logf("%-4d %-6s %-6d %-8t %-10d %s", no, f, g, n == 0, n, firstStage)
				if s := compareStages(t, libStages, dumpDir); s != "" {
					t.Fatalf("组%d 环节 %s 分叉", no, s)
				}
				if !bytes.Equal(cliOut, libOut) {
					t.Fatalf("组%d 最终产物逐字节不一致", no)
				}
			})
		}
	}
}

func mustDecodePGM(t *testing.T, raw []byte) *api.Image {
	t.Helper()
	img, err := api.Decode(raw, api.Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func itoa(x int) string {
	if x == 0 {
		return "0"
	}
	neg := x < 0
	if neg {
		x = -x
	}
	var b [20]byte
	i := len(b)
	for x > 0 {
		i--
		b[i] = byte('0' + x%10)
		x /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
