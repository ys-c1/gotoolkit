// 命令 guard 是基准漂移守卫的入口。
// 返回码只有两种：0 表示基准与实现一致，1 表示不一致或守卫自身无法完成。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gotoolkit/internal/guard"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	refDir := fs.String("ref", "", "独立参照目录，默认 <root>/ref")
	goldenDir := fs.String("golden", "", "只读基准目录，默认 <root>/testdata/golden")
	outDir := fs.String("out", "", "输出目录，默认 <root>/artifacts/drift-guard")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "定位仓库根失败:", err)
		return 1
	}
	if *refDir == "" {
		*refDir = filepath.Join(root, "ref")
	}
	if *goldenDir == "" {
		*goldenDir = filepath.Join(root, "testdata", "golden")
	}
	if *outDir == "" {
		*outDir = filepath.Join(root, "artifacts", "drift-guard")
	}

	rep, err := guard.Check(*refDir, *goldenDir, *outDir)
	if rep != nil {
		if werr := guard.WriteReport(os.Stdout, rep); werr != nil {
			fmt.Fprintln(os.Stderr, "写报告失败:", werr)
			return 1
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "guard:", err)
		return 1
	}
	return 0
}

// findRoot 从当前目录向上找 module gotoolkit 的 go.mod。
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil &&
			strings.HasPrefix(string(data), "module gotoolkit\n") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 module gotoolkit 的 go.mod")
		}
		dir = parent
	}
}
