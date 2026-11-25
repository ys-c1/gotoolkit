// 独立实验程序：在三张结构不同的确定性灰度图上扫描单游程上限，
// 用生产编码器 codec.EncodeRLEWithCap 得到贪心体积，并用独立实现的
// 分段 DP（穷举每个极大同值段的所有 1..cap 切块）求最优体积与强制断点数。
// 结果写到 artifacts/rlecap/sweep.json，供绘图与零误差核对。
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

	"gotoolkit/internal/codec"
)

type sample struct {
	Name    string
	Desc    string
	W, H    int
	Pix     []uint8
	SegLens []int
}

// fromRuns 用 (值, 长度) 游程规格构造像素流与段长。
func fromRuns(name, desc string, w, h int, runs [][2]int) sample {
	pix := make([]uint8, 0, w*h)
	lens := make([]int, 0, len(runs))
	for _, r := range runs {
		v, n := r[0], r[1]
		for k := 0; k < n; k++ {
			pix = append(pix, uint8(v))
		}
		lens = append(lens, n)
	}
	if len(pix) != w*h {
		panic(fmt.Sprintf("%s 像素数 %d != %d", name, len(pix), w*h))
	}
	return sample{Name: name, Desc: desc, W: w, H: h, Pix: pix, SegLens: lens}
}

func buildImages() []sample {
	const n = 256 // 16x16

	// 块状图：若干中等长度同值块，段长错落，上限收紧时会在块内反复断开。
	block := fromRuns("块状图", "中等长度同值块，段长错落", 16, 16, [][2]int{
		{10, 40}, {20, 18}, {30, 27}, {40, 9}, {50, 33},
		{60, 15}, {70, 22}, {80, 6}, {90, 29}, {100, 17},
		{110, 24}, {120, 16},
	})

	// 长游程图：极少、极长的同值段，上限越紧强制断点越多，最能体现断开代价。
	flat := fromRuns("长游程图", "少而长的同值段，对上限最敏感", 16, 16, [][2]int{
		{0, 71}, {128, 83}, {200, 64}, {64, 38},
	})

	// 噪声图：小字母表逐像素采样，天然游程多而短，段内很少需要因上限断开。
	rng := rand.New(rand.NewSource(20261126))
	noisePix := make([]uint8, n)
	for i := range noisePix {
		noisePix[i] = uint8(rng.Intn(4))
	}
	noiseLens := []int{}
	for i := 0; i < n; {
		j := i
		for j < n && noisePix[j] == noisePix[i] {
			j++
		}
		noiseLens = append(noiseLens, j-i)
		i = j
	}
	noise := sample{Name: "噪声图", Desc: "小字母表逐像素采样，游程多而短", W: 16, H: 16, Pix: noisePix, SegLens: noiseLens}

	return []sample{block, flat, noise}
}

// optimalTokens 独立 DP：每个极大同值段枚举所有合法切块长度取最小游程数。
func optimalTokens(segLens []int, maxRun int) int {
	total := 0
	for _, L := range segLens {
		dp := make([]int, L+1)
		for l := 1; l <= L; l++ {
			dp[l] = l
			for k := 1; k <= maxRun && k <= l; k++ {
				if 1+dp[l-k] < dp[l] {
					dp[l] = 1 + dp[l-k]
				}
			}
		}
		total += dp[L]
	}
	return total
}

func maxInt(xs []int) int {
	m := 0
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

type imageMeta struct {
	Name        string `json:"name"`
	Desc        string `json:"desc"`
	W           int    `json:"w"`
	H           int    `json:"h"`
	Pixels      int    `json:"pixels"`
	NaturalRuns int    `json:"naturalRuns"`
	MaxSegLen   int    `json:"maxSegLen"`
}

type row struct {
	Image        string `json:"image"`
	Cap          int    `json:"cap"`
	GreedyBytes  int    `json:"greedyBytes"`
	OptBytes     int    `json:"optBytes"`
	DiffBytes    int    `json:"diffBytes"`
	GreedyTokens int    `json:"greedyTokens"`
	OptTokens    int    `json:"optTokens"`
	ForcedBreaks int    `json:"forcedBreaks"`
}

type report struct {
	Header int         `json:"headerBytes"`
	Caps   []int       `json:"caps"`
	Images []imageMeta `json:"images"`
	Rows   []row       `json:"rows"`
}

func main() {
	caps := []int{1, 2, 3, 4, 5, 8, 16, 32, 64, 128, 255}
	imgs := buildImages()

	rep := report{Header: 8, Caps: caps}
	for _, im := range imgs {
		rep.Images = append(rep.Images, imageMeta{
			Name: im.Name, Desc: im.Desc, W: im.W, H: im.H, Pixels: len(im.Pix),
			NaturalRuns: len(im.SegLens), MaxSegLen: maxInt(im.SegLens),
		})
		for _, c := range caps {
			body, err := codec.EncodeRLEWithCap(im.W, im.H, im.Pix, c)
			if err != nil {
				panic(err)
			}
			greedyTokens := (len(body) - 8) / 2
			optTokens := optimalTokens(im.SegLens, c)
			rep.Rows = append(rep.Rows, row{
				Image:        im.Name,
				Cap:          c,
				GreedyBytes:  len(body),
				OptBytes:     8 + 2*optTokens,
				DiffBytes:    len(body) - (8 + 2*optTokens),
				GreedyTokens: greedyTokens,
				OptTokens:    optTokens,
				ForcedBreaks: optTokens - len(im.SegLens),
			})
		}
	}

	outDir := filepath.Join("artifacts", "rlecap")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}
	path := filepath.Join(outDir, "sweep.json")
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		panic(err)
	}

	// 控制台同时给出零误差核对与 cap=2 退化体积。
	worst := 0
	for _, r := range rep.Rows {
		if r.DiffBytes > worst {
			worst = r.DiffBytes
		}
	}
	fmt.Printf("写出 %s，%d 组数据，贪心-最优最大体积差 = %d 字节（判据 <1）\n",
		path, len(rep.Rows), worst)
	for _, im := range imgs {
		for _, r := range rep.Rows {
			if r.Image == im.Name && r.Cap == 2 {
				fmt.Printf("cap=2 %-6s 体积=%3d 字节 游程=%3d 强制断点=%3d\n",
					im.Name, r.GreedyBytes, r.GreedyTokens, r.ForcedBreaks)
			}
		}
	}
}
