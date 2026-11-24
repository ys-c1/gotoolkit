package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Regenerate 用 ref/ 独立参照重新生成三份基准，返回 格式 -> 新鲜字节。
//
// ref 是独立 module，必须在自己的目录里执行；它把结果写到 ../testdata/golden。
// 为了绝不碰到真实基准，这里在临时目录里用"文件级软链"镜像 ref 源码，并让
// ../testdata/golden 成为 tmp 内的普通目录。不能直接软链 ref 目录：chdir 会解析
// 到物理路径，../testdata/golden 就指回真基准了。
func Regenerate(refDir string) (map[string][]byte, error) {
	tmp, err := os.MkdirTemp("", "gtk-ref-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	refDir, err = filepath.Abs(refDir)
	if err != nil {
		return nil, err
	}
	link := filepath.Join(tmp, "ref")
	if err := mirrorFiles(refDir, link); err != nil {
		return nil, fmt.Errorf("无法镜像参照目录: %w", err)
	}
	outDir := filepath.Join(tmp, "testdata", "golden")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	exe := filepath.Join(tmp, "ref.bin")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Dir = link
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("构建 ref 失败: %w\n%s", err, out)
	}
	run := exec.Command(exe)
	run.Dir = link
	if out, err := run.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("运行 ref 失败: %w\n%s", err, out)
	}

	fresh := map[string][]byte{}
	for _, f := range formats {
		b, err := os.ReadFile(filepath.Join(outDir, f+".bin"))
		if err != nil {
			return nil, fmt.Errorf("ref 未生成 %s.bin: %w", f, err)
		}
		fresh[f] = b
	}
	return fresh, nil
}

// mirrorFiles 把 src 下的普通文件逐个软链到 dst，目录建成真目录。
func mirrorFiles(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return os.Symlink(path, target)
	})
}
