// 命令 genbatchfixture 为批量端到端测试生成夹具目录，不参与正常构建产物。
// 用法：
//
//	genbatchfixture good   <dir> <mode> <format> <n>   生成 n 个有效输入
//	genbatchfixture corrupt <dir> <mode> <format> <name> 追加一个必然解码失败的坏文件
//
// mode = encode | decode。encode 生成 .pgm；decode 生成对应格式文件。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

func main() {
	if len(os.Args) < 2 {
		fail("用法: genbatchfixture good|corrupt ...")
	}
	switch os.Args[1] {
	case "good":
		if len(os.Args) != 6 {
			fail("用法: genbatchfixture good <dir> <mode> <format> <n>")
		}
		dir, mode, format, n := os.Args[2], os.Args[3], os.Args[4], atoi(os.Args[5])
		for i := 0; i < n; i++ {
			base := fmt.Sprintf("img%02d", i)
			body := buildGood(mode, format, i)
			if err := write(filepath.Join(dir, base+ext(mode, format)), body); err != nil {
				fail(err.Error())
			}
		}
	case "corrupt":
		if len(os.Args) != 6 {
			fail("用法: genbatchfixture corrupt <dir> <mode> <format> <name>")
		}
		dir, mode, format, name := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
		if err := write(filepath.Join(dir, name), buildCorrupt(mode, format)); err != nil {
			fail(err.Error())
		}
	default:
		fail("未知子命令: " + os.Args[1])
	}
}

func buildGood(mode, format string, i int) []byte {
	pix := make([]byte, 12)
	for k := range pix {
		pix[k] = byte((i*11 + k*3) % 251)
	}
	img := &api.Image{W: 4, H: 3, Pix: pix}
	pgm, err := api.Encode(img, api.Options{Format: "pgm"})
	if err != nil {
		fail(err.Error())
	}
	if mode == "encode" {
		return pgm
	}
	body, err := api.Encode(img, api.Options{Format: format})
	if err != nil {
		fail(err.Error())
	}
	return body
}

func buildCorrupt(mode, format string) []byte {
	if mode == "encode" {
		return []byte("XX\n4 3\n255\n000000000000")
	}
	switch format {
	case "pgm":
		return []byte("XX-not-a-pgm-stream")
	case "bmp":
		return []byte("XX-not-a-bmp-stream-padding")
	default:
		return []byte("XXXX-not-an-rle-stream-padding")
	}
}

func ext(mode, format string) string {
	if mode == "encode" {
		return ".pgm"
	}
	switch format {
	case "bmp":
		return ".bmp"
	case "rle":
		return ".rle"
	default:
		return ".pgm"
	}
}

func write(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			fail("非整数: " + s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}
