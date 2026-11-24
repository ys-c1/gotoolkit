package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

// 四档目标：用“目标每像素比特带”描述预算松紧，再换算成字节闭区间。
// 这样区间随输入图尺寸缩放，而不是写死字节数。从极紧到高画质排列。
type targetTier struct {
	Key   string
	Name  string
	BPPLo float64
	BPPHi float64
}

var targetTiers = []targetTier{
	{"T1", "极紧", 1.00, 1.60},
	{"T2", "紧凑", 2.00, 2.70},
	{"T3", "均衡", 3.50, 4.60},
	{"T4", "高画质", 5.40, 7.20},
}

// tierRecord 是每档落一行的原始 JSONL 记录。
type tierRecord struct {
	Tier         string          `json:"tier"`
	Name         string          `json:"name"`
	BPPRange     [2]float64      `json:"bpp_range"`
	Interval     [2]int          `json:"target_interval_bytes"`
	BlockSize    int             `json:"block_size"`
	W            int             `json:"w"`
	H            int             `json:"h"`
	Pixels       int             `json:"pixels"`
	BinaryQ      int             `json:"binary_quality"`
	BinaryDepth  int             `json:"binary_depth"`
	Quality      int             `json:"quality"`
	Depth        int             `json:"depth"`
	Bytes        int             `json:"measured_bytes"`
	MeasuredBPP  float64         `json:"measured_bpp"`
	InRange      bool            `json:"in_range"`
	Side         string          `json:"side"`
	GapBytes     int             `json:"gap_bytes"`
	GapPercent   float64         `json:"gap_percent"`
	ClosestQ     int             `json:"closest_quality"`
	ClosestBytes int             `json:"closest_bytes"`
	PSNRdB       *float64        `json:"psnr_db"` // +Inf（完全相同）编码为 null
	Steps        []api.SolveStep `json:"binary_steps"`
}

func runTargets(args []string) int {
	fs := flag.NewFlagSet("targets", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM；留空使用内置 256x256 标准测试图")
	block := fs.Int("block-size", api.DefaultBlockSize, "分块边长 B")
	recordPath := fs.String("record", filepath.Join("artifacts", "target-records.jsonl"), "每档一行的 JSONL 原始记录")
	outDir := fs.String("outdir", "artifacts", "把每档采用参数的 .blk 与解码 .pgm 写到该目录；留空不写")
	noColor := fs.Bool("no-color", false, "关闭 ✓/✗ 等着色与符号强调")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var img *api.Image
	var srcDesc string
	if *in != "" {
		raw, err := os.ReadFile(*in)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取失败:", err)
			return 1
		}
		img, err = api.Decode(raw, api.Options{Format: "pgm"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "解码 PGM 失败:", err)
			return 1
		}
		srcDesc = *in
	} else {
		img = demoImage(256)
		srcDesc = "内置确定性测试图(256x256)"
	}

	color := !*noColor && os.Getenv("NO_COLOR") == ""
	mark := func(ok bool) string {
		if ok {
			if color {
				return "\x1b[32m✓ 落入区间\x1b[0m"
			}
			return "落入区间"
		}
		if color {
			return "\x1b[31m✗ 区间外→取最接近\x1b[0m"
		}
		return "区间外→取最接近"
	}

	if err := os.MkdirAll(filepath.Dir(*recordPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "记录目录创建失败:", err)
		return 1
	}
	rf, err := os.Create(*recordPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "记录文件创建失败:", err)
		return 1
	}
	defer rf.Close()
	enc := json.NewEncoder(rf)

	w, h, b := img.W, img.H, *block
	pixels := w * h
	nb := ((w + b - 1) / b) * ((h + b - 1) / b)

	fmt.Println("按目标体积反解质量参数（BLK 分块位深，体积关于 Q 严格单调）")
	fmt.Printf("图源=%s  画幅=%dx%d  像素=%d  分块 B=%d  块数 N=%d\n", srcDesc, w, h, pixels, b, nb)
	fmt.Println("单调关系: d(Q)=ceil(8Q/100)，Q↑→d↑；S(Q)=10+2N+Σ_j ceil(k_j·d(Q)/8)，故 S 关于 Q 单调不减。")
	if w%b == 0 && h%b == 0 {
		c0 := 10 + nb*2
		c1 := nb * b * b / 8 // 每提升 1 位深增加的字节数
		fmt.Printf("整除化简: S(Q)=10+%d·(2+B²·d/8)=%d+%d·d(Q) 字节（本图 B=%d）。\n", nb, c0, c1, b)
	}
	fmt.Printf("预算式子: 每块预算 b_blk=(T−10)/N；d*=floor(8(b_blk−2)/B²)；" +
		"整除时 d*=8(T−10)/(W·H)−16/B²；反求块尺寸 B≤floor(sqrt(8(b_blk−2)/d))。\n")
	fmt.Println("缓冲复用: BlockDecoder 仅分配一块容量 B×B 的中间 []uint8，各块覆写同一底层数组并立即拷入整图，详见 README。")
	fmt.Println()

	var recs []tierRecord
	allIn := true
	for i, t := range targetTiers {
		lo := int(math.Round(t.BPPLo * float64(pixels) / 8))
		hi := int(math.Round(t.BPPHi * float64(pixels) / 8))

		rep, err := api.SolveQuality(img, api.Options{
			Format:         "blk",
			BlockSize:      b,
			TargetMinBytes: lo,
			TargetMaxBytes: hi,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "第%d档求解失败: %v\n", i+1, err)
			return 1
		}
		rep.Tier = t.Key

		fmt.Printf("== 第%d档 [%s·%s] 目标区间 [%d,%d] B（%.2f~%.2f bpp）==\n",
			i+1, t.Key, t.Name, lo, hi, t.BPPLo, t.BPPHi)
		fmt.Println("   二分过程（窗口 lo..hi，mid 与实测体积并列）:")
		for _, s := range rep.Steps {
			action := "S<下界 → lo=mid+1"
			if s.Bytes >= lo {
				action = "S≥下界 → hi=mid-1"
			}
			fmt.Printf("     #%d 窗口[%3d,%3d] mid=%3d d=%d 实测=%6d B  %s\n",
				s.N, s.Lo, s.Hi, s.Mid, s.Depth, s.Bytes, action)
		}

		psnr := "∞(相同)"
		if !math.IsInf(rep.PSNR, 1) {
			psnr = fmt.Sprintf("%.2f dB", rep.PSNR)
		}
		// 关键对账行：解出的参数 Q 与实测体积在同一行。
		fmt.Printf("   ★ 第%d档[%s] 解出参数 Q=%d（位深 d=%d） | 实测体积=%d B | 目标区间 [%d,%d] B | %s | PSNR=%s\n",
			i+1, t.Name, rep.Quality, rep.Depth, rep.Bytes, lo, hi, mark(rep.InRange), psnr)
		if !rep.InRange {
			allIn = false
			fmt.Printf("     └ 区间外说明: 实测 %d B 相对上限/下界差距 %d B（%.2f%%）；"+
				"全质量域最接近参数 Q=%d（d=%d），其实测体积 %d B。\n",
				rep.Bytes, rep.GapBytes, rep.GapPercent, rep.ClosestQuality, rep.ClosestDepth, rep.ClosestBytes)
		}
		fmt.Println()

		rec := tierRecord{
			Tier: t.Key, Name: t.Name, BPPRange: [2]float64{t.BPPLo, t.BPPHi},
			Interval: [2]int{lo, hi}, BlockSize: b, W: w, H: h, Pixels: pixels,
			BinaryQ: rep.BinaryQuality, BinaryDepth: rep.BinaryDepth,
			Quality: rep.Quality, Depth: rep.Depth, Bytes: rep.Bytes,
			MeasuredBPP: 8 * float64(rep.Bytes) / float64(pixels),
			InRange:     rep.InRange, Side: rep.Side, GapBytes: rep.GapBytes,
			GapPercent: rep.GapPercent, ClosestQ: rep.ClosestQuality, ClosestBytes: rep.ClosestBytes,
			PSNRdB: finitePSNR(rep.PSNR), Steps: rep.Steps,
		}
		recs = append(recs, rec)
		_ = enc.Encode(rec)

		if *outDir != "" {
			if err := os.MkdirAll(*outDir, 0o755); err != nil {
				fmt.Fprintln(os.Stderr, "输出目录创建失败:", err)
				return 1
			}
			base := filepath.Join(*outDir, fmt.Sprintf("tier%d-%s", i+1, t.Key))
			if err := os.WriteFile(base+".blk", rep.Body, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "写 .blk 失败:", err)
				return 1
			}
			dec, derr := api.Decode(rep.Body, api.Options{Format: "blk"})
			if derr == nil {
				if pgm, perr := api.Encode(dec, api.Options{Format: "pgm"}); perr == nil {
					_ = os.WriteFile(base+".pgm", pgm, 0o644)
				}
			}
		}
	}

	// 四档区间 vs 实测逐档对照（Q 与实测体积同一行）。
	fmt.Println("四档目标区间与实测值逐档对照:")
	fmt.Println("档位 | bpp带 | 目标区间[lo,hi] B | 二分Q(d) | 采用Q(d) | 实测体积 B | 实测bpp | 判定 | 差距B/%")
	fmt.Println("-----|-------|-------------------|----------|----------|-----------|---------|------|--------")
	for _, r := range recs {
		verdict := "落入"
		if !r.InRange {
			verdict = "区间外"
		}
		gap := "0"
		if !r.InRange {
			gap = fmt.Sprintf("%d / %.1f%%", r.GapBytes, r.GapPercent)
		}
		fmt.Printf("%s%s | %.2f~%.2f | [%d,%d] | Q=%d(d%d) | Q=%d(d%d) | %d | %.3f | %s | %s\n",
			r.Tier, r.Name, r.BPPRange[0], r.BPPRange[1], r.Interval[0], r.Interval[1],
			r.BinaryQ, r.BinaryDepth, r.Quality, r.Depth, r.Bytes, r.MeasuredBPP, verdict, gap)
	}
	fmt.Println()

	// 额外一个落在相邻位深“空档”里的窄区间，确定性演示区间外→最接近→差多少。
	runGapProbe(img, b, enc)

	inCount := 0
	for _, r := range recs {
		if r.InRange {
			inCount++
		}
	}
	fmt.Printf("四档对账: %d/4 档实测体积落入各自目标区间；原始记录已写入 %s。\n", inCount, *recordPath)
	if !allIn {
		fmt.Println("（存在区间外档位：已按要求给出最接近参数与差距，这是离散位深下的正常兜底，不算求解错误。）")
	}
	return 0
}

// runGapProbe 用 d=2 与 d=3 两个可达体积之间的空档构造窄区间，
// 必然不可达（空档足够宽时），用来公开展示“区间外→最接近→差多少”。
func runGapProbe(img *api.Image, b int, enc *json.Encoder) {
	w, h := img.W, img.H
	s2 := api.BLKDepthSize(w, h, b, 2)
	s3 := api.BLKDepthSize(w, h, b, 3)
	if s3-s2 < 6 {
		fmt.Println("兜底探针: 图像/分块太小，相邻位深空档不足，跳过。")
		return
	}
	lo := s2 + (s3-s2)/3
	hi := s3 - (s3-s2)/3
	rep, err := api.SolveQuality(img, api.Options{
		Format: "blk", BlockSize: b, TargetMinBytes: lo, TargetMaxBytes: hi,
	})
	if err != nil {
		fmt.Println("兜底探针求解失败:", err)
		return
	}
	fmt.Println("区间外兜底探针（故意把窄区间放进 d=2 与 d=3 的体积空档）:")
	if rep.BinaryQuality >= 0 {
		fmt.Printf("  窄区间 [%d,%d] B；二分 lower_bound Q=%d(d%d) 自身体积=%d B，越过上限 %d B；\n",
			lo, hi, rep.BinaryQuality, rep.BinaryDepth, rep.BinaryBytes, rep.BinaryBytes-hi)
	} else {
		fmt.Printf("  窄区间 [%d,%d] B；最高质量仍低于下界；\n", lo, hi)
	}
	fmt.Printf("  → 最接近参数 Q=%d(d%d)，实测体积 %d B，位于区间%s，差距 %d B（%.2f%%）。\n\n",
		rep.ClosestQuality, rep.ClosestDepth, rep.ClosestBytes,
		sideZh(rep.Side), rep.GapBytes, rep.GapPercent)

	probe := map[string]any{
		"probe":                 "gap_between_depth2_depth3",
		"target_interval_bytes": [2]int{lo, hi},
		"binary_quality":        rep.BinaryQuality,
		"binary_bytes":          rep.BinaryBytes,
		"quality":               rep.Quality, "depth": rep.Depth,
		"measured_bytes": rep.Bytes, "in_range": rep.InRange,
		"closest_quality": rep.ClosestQuality, "closest_bytes": rep.ClosestBytes,
		"gap_bytes": rep.GapBytes, "gap_percent": rep.GapPercent, "side": rep.Side,
	}
	_ = enc.Encode(probe)
}

// finitePSNR 把 +Inf/NaN 的 PSNR 转成 null，使纯色无损档也能写进 JSONL。
func finitePSNR(v float64) *float64 {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return nil
	}
	return &v
}

func sideZh(side string) string {
	switch side {
	case "below_min":
		return "下界之下（体积偏小）"
	case "above_max":
		return "上界之上（体积偏大）"
	default:
		return "内"
	}
}

// demoImage 生成确定性、含渐变/条纹/色块/伪噪声的灰度图，
// 让 PSNR 与块效应有代表性；BLK 体积只取决于画幅/B/位深，与像素内容无关。
func demoImage(n int) *api.Image {
	pix := make([]uint8, n*n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			v := 0.5*float64(x) + 0.35*float64(y) +
				28*math.Sin(float64(x)/9.0) + 20*math.Cos(float64(y)/13.0)
			if (x/32+y/32)%2 == 0 {
				v += 34
			}
			if x > n/4 && x < 3*n/4 && y > n/3 && y < 2*n/3 {
				v = 220 - 0.4*float64(x)
			}
			h := uint32(x)*73856093 ^ uint32(y)*19349663
			h ^= h >> 13
			h *= 1274126177
			v += float64(int(h%31) - 15)
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			pix[y*n+x] = uint8(v)
		}
	}
	return &api.Image{W: n, H: n, Pix: pix}
}
