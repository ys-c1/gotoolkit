// roundtrip 对三种格式做往返校验并落盘报告：
//
//	go run ./cmd/roundtrip -out artifacts/roundtrip
//
// 输出：每格式一份 <format>.txt、合并的 report.txt、以及往返前后的两张图
// source.pgm / roundtrip.pgm。任一种格式不一致即以非零码退出。
package main

import (
	"flag"
	"fmt"
	"os"

	"gotoolkit/internal/roundtrip"
)

func main() {
	out := flag.String("out", "", "产物目录（为空则只打印报告）")
	flag.Parse()

	src := roundtrip.DefaultSample()
	suite, err := roundtrip.VerifyAll(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "往返校验失败:", err)
		os.Exit(1)
	}
	roundtrip.RenderSuite(os.Stdout, suite)

	if *out != "" {
		if err := roundtrip.WriteArtifacts(*out, src, suite); err != nil {
			fmt.Fprintln(os.Stderr, "写产物失败:", err)
			os.Exit(1)
		}
		fmt.Printf("\n产物目录: %s\n", *out)
	}

	if !suite.OK {
		os.Exit(1)
	}
}
