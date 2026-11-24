// Command predictbench 生成一组确定性的长渐变图，分别以「关闭预测」和
// 两档预测阶数编码 RLE，打印体积、压缩率与选中候选，便于核验。
//
// 用法：go run ./cmd/predictbench
// 图像只写到临时目录，不落进仓库。
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

type fixture struct {
	name string
	w, h int
	pix  func(x, y int) float64
}

func fixtures() []fixture {
	return []fixture{
		// 每行相同、行内线性递增：竖直方向完全平坦。
		{"linear-h 512x128", 512, 128, func(x, y int) float64 {
			return 255 * float64(x) / 511
		}},
		// 行内二次曲线：一阶差分仍在变，二阶差分恒定。
		{"quadratic-h 512x128", 512, 128, func(x, y int) float64 {
			u := float64(x) / 511
			return 255 * u * u
		}},
		// 行内三次曲线。
		{"cubic-h 512x128", 512, 128, func(x, y int) float64 {
			u := float64(x) / 511
			return 255 * u * u * u
		}},
		// 对角线性渐变：x、y 两个方向同时变。
		{"diagonal 512x128", 512, 128, func(x, y int) float64 {
			return 255 * (0.5*float64(x)/511 + 0.5*float64(y)/127)
		}},
		// 竖直渐变：整幅图每行是常量，逐行变化。
		{"vertical 1024x256", 1024, 256, func(x, y int) float64 {
			return 255 * float64(y) / 255
		}},
		// 线性渐变叠 ±2 的确定性伪噪声，代表真实扫描件。
		{"linear+noise 512x128", 512, 128, func(x, y int) float64 {
			s := uint32(x)*2654435761 + uint32(y)*40503
			s ^= s >> 13
			return 255*float64(x)/511 + float64(int(s%5)-2)
		}},
	}
}

func render(f fixture) *api.Image {
	img := &api.Image{W: f.w, H: f.h, Pix: make([]uint8, f.w*f.h)}
	for y := 0; y < f.h; y++ {
		for x := 0; x < f.w; x++ {
			v := f.pix(x, y)
			switch {
			case v < 0:
				v = 0
			case v > 255:
				v = 255
			}
			img.Pix[y*f.w+x] = uint8(math.Round(v))
		}
	}
	return img
}

func main() {
	dir, err := os.MkdirTemp("", "predictbench")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	tiers := []int{2, 4}
	fmt.Println("| 图像 | 原 GTR1 字节 | 档位 | 熵选候选 | 写出候选 | 预测后字节 | 压缩率 | 字节减少 |")
	fmt.Println("|---|---:|---|---|---|---:|---:|---:|")
	for _, f := range fixtures() {
		img := render(f)
		base, err := api.Encode(img, api.Options{Format: "rle"})
		if err != nil {
			panic(err)
		}
		// 落一份原始 PGM，便于用命令行复核两个子命令。
		pgm, err := api.Encode(img, api.Options{Format: "pgm"})
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.name+".pgm"), pgm, 0o644); err != nil {
			panic(err)
		}
		for _, tier := range tiers {
			body, rep, err := api.EncodeWithReport(img, api.Options{Format: "rle", PredictOrder: tier})
			if err != nil {
				panic(err)
			}
			ratio := 100 * (1 - float64(len(body))/float64(len(base)))
			fmt.Printf("| %s | %d | ≤%d | %s | %s | %d | %.2f%% | %d |\n",
				f.name, len(base), tier, rep.Selected, rep.Emitted, len(body), ratio, len(base)-len(body))
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.t%d.rle", f.name, tier)), body, 0o644); err != nil {
				panic(err)
			}
		}
	}
	fmt.Printf("\n临时目录：%s\n", dir)
}
