// Package crosscheck 对拍命令行路径与库调用路径。
//
// 两条路径喂同一份参数、同一份输入：
//
//	命令行：build/gtk encode --format F --gain G --in in.pgm --out cli.bin
//	库调用：api.PipeStages(raw, "encode", F, G)（内部即 api.Decode/api.Encode）
//
// 三种格式（pgm/bmp/rle）乘三种增益（-50/0/+50）共九组，逐组逐字节比较。
// 若最终产物不一致，用分段直通把差异定位到第一个出现分叉的环节，并生成差异图。
package crosscheck

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotoolkit/api"
)

// 九组矩阵：3 格式 × 3 增益。
var (
	formats = []string{"pgm", "bmp", "rle"}
	gains   = []int{-50, 0, 50}
)

// groupNo 按“格式外层、增益内层”编号 1..9。
func groupNo(format string, gain int) int {
	for i, f := range formats {
		if f == format {
			for j, g := range gains {
				if g == gain {
					return i*len(gains) + j + 1
				}
			}
		}
	}
	return -1
}

// samplePGM 构造一份确定性输入：7×5，像素覆盖零、255、255 游程边界与高字节截断。
// 宽 7 使 BMP 出现行填充（7*3=21，对齐到 24），像素流里有一条长度 300 的等值游程，
// 使 RLE 触发 255 截断分块。
func samplePGM() []byte {
	w, h := 7, 5
	pix := make([]byte, w*h)
	for i := range pix {
		pix[i] = byte((i*37 + 11) % 256)
	}
	// 制造一条跨 255 边界的长游程（前 300 个像素同值），其余保持渐变。
	for i := 0; i < 300 && i < len(pix); i++ {
		pix[i] = 200
	}
	return append([]byte(fmt.Sprintf("P5\n%d %d\n255\n", w, h)), pix...)
}

// buildCLI 编译命令行二进制，返回路径。所有用例共享一次编译。
// 测试运行时工作目录是 crosscheck/，CLI 源码在模块根的 cmd/gtk，需先定位模块根。
func buildCLI(t *testing.T) string {
	t.Helper()
	root := moduleRoot(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "gtk")
	cmd := exec.Command("go", "build", "-o", bin, filepath.Join(root, "cmd", "gtk"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 CLI 失败: %v\n%s", err, out)
	}
	return bin
}

// moduleRoot 通过 go env GOMOD 定位模块根目录，避免依赖测试工作目录。
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("定位模块根失败: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	return filepath.Dir(mod)
}

// diffMapDir 返回差异图输出目录（模块根 artifacts/diffmaps）。
func diffMapDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(moduleRoot(t), "artifacts", "diffmaps")
}

// runCLI 以对拍模式跑一次 CLI：GTK_DUMP_DIR 让它把四个环节产物落盘。
// 返回退出码、stdout、stderr 与转储目录。
func runCLI(t *testing.T, bin string, args []string) (int, string, string, string) {
	t.Helper()
	dumpDir := t.TempDir()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "GTK_DUMP_DIR="+dumpDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	rc := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); !ok {
			t.Fatalf("启动 CLI 失败: %v", err)
		}
		rc = ee.ExitCode()
	}
	return rc, stdout.String(), stderr.String(), dumpDir
}

func asExitError(err error, ee **exec.ExitError) (ok bool) {
	*ee, ok = err.(*exec.ExitError)
	return
}

// stageFile 读取某环节的转储字节；不存在即视为该环节缺失。
func stageFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name+".bin"))
	if err != nil {
		return nil
	}
	return b
}

// compareStages 逐环节比较库侧产物与 CLI 转储，返回第一个分叉环节；全一致返回空串。
func compareStages(t *testing.T, libStages []api.Stage, dumpDir string) string {
	t.Helper()
	for _, st := range libStages {
		var want []byte
		if st.Body != nil {
			want = st.Body
		} else {
			want = st.Pix
		}
		got := stageFile(t, dumpDir, st.Name)
		if !bytes.Equal(got, want) {
			return st.Name
		}
	}
	return ""
}

// diffBytes 统计两份字节流的差异字节数（按较短长度对齐，长度差也计入）。
func diffBytes(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	d := 0
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			d++
		}
	}
	return d + abs(len(a)-len(b))
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// writeDiffMap 生成差异图：SVG 热力图 + 文本偏移清单，返回两个文件路径。
// 热力图把每个差异字节画成一个红色方格，X 为文件偏移，正常字节不画。
func writeDiffMap(t *testing.T, dir, tag string, a, b []byte) (svg, txt string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	type offset struct {
		idx      int
		cli, lib byte
	}
	var diffs []offset
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			diffs = append(diffs, offset{i, a[i], b[i]})
		}
	}
	// 文本清单：最多列前 50 个偏移，超出给汇总。
	var sb strings.Builder
	fmt.Fprintf(&sb, "差异图 %s\n", tag)
	fmt.Fprintf(&sb, "CLI 长度=%d 库长度=%d 差异字节=%d\n", len(a), len(b), diffBytes(a, b))
	for i, d := range diffs {
		if i >= 50 {
			fmt.Fprintf(&sb, "... 其余 %d 个偏移省略\n", len(diffs)-50)
			break
		}
		fmt.Fprintf(&sb, "偏移 %5d: CLI=0x%02x 库=0x%02x\n", d.idx, d.cli, d.lib)
	}
	if len(a) != len(b) {
		fmt.Fprintf(&sb, "长度不一致：尾部 %d 字节仅存在于较长一侧\n", abs(len(a)-len(b)))
	}
	txt = filepath.Join(dir, tag+"_diff.txt")
	if err := os.WriteFile(txt, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// SVG：每 16 字节一行，差异格红色，附偏移刻度。
	const cell = 14
	longest := len(a)
	if len(b) > longest {
		longest = len(b)
	}
	rows := longest/16 + 1
	svg = filepath.Join(dir, tag+"_diff.svg")
	var s strings.Builder
	fmt.Fprintf(&s, "<svg xmlns='http://www.w3.org/2000/svg' width='%d' height='%d' font-family='monospace' font-size='10'>\n",
		16*cell+80, rows*cell+40)
	fmt.Fprintf(&s, "<rect width='100%%' height='100%%' fill='white'/>\n")
	fmt.Fprintf(&s, "<text x='10' y='18'>%s 红格=差异字节 共%d个</text>\n", tag, len(diffs))
	diffSet := make(map[int]bool, len(diffs))
	for _, d := range diffs {
		diffSet[d.idx] = true
	}
	for idx := 0; idx < longest; idx++ {
		col, row := idx%16, idx/16
		x, y := 40+col*cell, 30+row*cell
		if diffSet[idx] {
			fmt.Fprintf(&s, "<rect x='%d' y='%d' width='%d' height='%d' fill='red'/>\n", x, y, cell-1, cell-1)
		}
	}
	s.WriteString("</svg>\n")
	if err := os.WriteFile(svg, []byte(s.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return svg, txt
}
