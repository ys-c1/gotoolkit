// chromareport 生成三档验证彩图，做 4:2:0 降采样后分别用双线性与自适应重建，
// 打印边缘像素平均/最大色差对照表，并把各档参考图与两种重建图写成 P6 PPM。
//
// 用法：go run ./cmd/chromareport [--dir 输出目录]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/api"
	"gotoolkit/internal/chroma"
	"gotoolkit/internal/codec"
)

func main() {
	dir := flag.String("dir", filepath.Join("artifacts", "chroma"), "PPM 产物输出目录")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		panic(err)
	}

	fmt.Println("色度 4:2:0 重建色差对照（仅统计亮度 Sobel 边缘像素）")
	fmt.Println("色差单位为 8 位色度量级；CbCr=色度平面欧氏距离，RGB=重建后 RGB 欧氏距离。")
	fmt.Println()
	header := fmt.Sprintf("%-12s %8s | %-21s | %-21s | %-9s",
		"档位", "边缘像素", "双线性 平均/最大", "自适应 平均/最大", "均值降幅")
	fmt.Println(header)
	fmt.Println(repeat("-", len(header)+12))

	for i, tier := range chroma.Tiers() {
		refRGB := chroma.GenTierImage(i)
		ref := &api.ColorImage{W: refRGB.W, H: refRGB.H, Pix: refRGB.Pix}

		sub, err := chroma.Subsample(refRGB)
		if err != nil {
			panic(err)
		}
		biRGB, err := chroma.Reconstruct(sub, chroma.ModeBilinear)
		if err != nil {
			panic(err)
		}
		adRGB, err := chroma.Reconstruct(sub, chroma.ModeAdaptive)
		if err != nil {
			panic(err)
		}
		bi := &api.ColorImage{W: biRGB.W, H: biRGB.H, Pix: biRGB.Pix}
		ad := &api.ColorImage{W: adRGB.W, H: adRGB.H, Pix: adRGB.Pix}

		mb := chroma.ColorDiff(refRGB, sub, biRGB)
		ma := chroma.ColorDiff(refRGB, sub, adRGB)
		drop := 100 * (mb.CbCrMean - ma.CbCrMean) / mb.CbCrMean

		fmt.Printf("%-12s %8d | CbCr %6.2f / %6.2f | CbCr %6.2f / %6.2f | %6.1f%%\n",
			tier.Name, mb.EdgePixels, mb.CbCrMean, mb.CbCrMax, ma.CbCrMean, ma.CbCrMax, drop)
		fmt.Printf("%-12s %8s | RGB  %6.2f / %6.2f | RGB  %6.2f / %6.2f |\n\n",
			"", "", mb.RGBMean, mb.RGBMax, ma.RGBMean, ma.RGBMax)

		writePPM(*dir, fmt.Sprintf("tier%d_ref.ppm", i), ref)
		writePPM(*dir, fmt.Sprintf("tier%d_bilinear.ppm", i), bi)
		writePPM(*dir, fmt.Sprintf("tier%d_adaptive.ppm", i), ad)
	}
	fmt.Println("PPM 产物已写入", *dir)
}

func writePPM(dir, name string, img *api.ColorImage) {
	body, err := codec.EncodePPM(img.W, img.H, img.Pix)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		panic(err)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
