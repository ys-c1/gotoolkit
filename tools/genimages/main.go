// genimages 把 internal/testimg 的三张图写成 P5 PGM，供复现实验与文档使用。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gotoolkit/internal/testimg"
)

func main() {
	dir := "testdata/images"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	imgs := testimg.All()
	names := make([]string, 0, len(imgs))
	for name := range imgs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		head := fmt.Sprintf("P5\n%d %d\n255\n", testimg.Size, testimg.Size)
		body := append([]byte(head), imgs[name]...)
		path := filepath.Join(dir, name+".pgm")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			panic(err)
		}
		fmt.Printf("%s %d bytes\n", path, len(body))
	}
}
