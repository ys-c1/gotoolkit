// 分析命令：对三份样本 PGM 做信息熵下界计算与 PGM<->RLE 互转校验。
// 只通过 api 包访问编解码能力，不直接引用 internal/codec，保持分层约束。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

const (
	W = 64
	H = 64
	N = W * H
)

type sample struct {
	Name string
	Desc string
	Pix  []uint8
}

// xorshift32：确定性伪随机，固定种子保证噪声样本可复跑。
type xorshift32 struct{ state uint32 }

func (r *xorshift32) next() uint32 {
	x := r.state
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.state = x
	return x
}

func genSamples() []sample {
	flat := make([]uint8, N)
	for i := range flat {
		flat[i] = 128 // 单符号
	}

	band := make([]uint8, N)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			band[y*W+x] = uint8(4 * y) // 每一行一个游程，共 64 个游程
		}
	}

	rng := &xorshift32{state: 0x20261125}
	noise := make([]uint8, N)
	for i := range noise {
		noise[i] = uint8(rng.next() % 256) // 近似均匀白噪声
	}

	return []sample{
		{"flat", "全平图，全部像素=128（单符号）", flat},
		{"band", "水平条纹，pix=4y，64 个长度 64 的游程", band},
		{"noise", "固定种子 xorshift32 白噪声，符号近均匀", noise},
	}
}

// entropyBits 返回像素符号分布的一阶信息熵（bit/符号）。
// 这是把像素当作独立同分布符号时的无记忆熵下界；
// 真实存在空间相关时可达更紧的下界，band 样本会体现这一点。
func entropyBits(pix []uint8) float64 {
	var freq [256]int
	for _, v := range pix {
		freq[v]++
	}
	h := 0.0
	n := float64(len(pix))
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// countRuns 统计按当前 GTR1 规则（计数封顶 255）切出的游程数。
func countRuns(pix []uint8) int {
	runs, i := 0, 0
	for i < len(pix) {
		j := i
		for j < len(pix) && pix[j] == pix[i] && j-i < 255 {
			j++
		}
		runs++
		i = j
	}
	return runs
}

// bisectMaxRuns 二分：在所有“游程数恰好为 r 的 GTR1 编码”里，求满足
// 8+2r <= boundBytes 的最大 r（单调谓词，真前缀）。
// 搜索区间固定为 [0, N]，返回 (最大可行 r, 下界是否可被任一合法编码满足, 迭代步数, 轨迹)。
type traceStep struct {
	Lo, Hi, Mid int
	MidSize     int
	Feasible    bool
}

func bisectMaxRuns(boundBytes int) (int, bool, int, []traceStep) {
	lo, hi := 0, N // r 取值域 [0, N]；N 是每像素一游程的最坏情况
	steps := make([]traceStep, 0)
	for lo < hi {
		mid := lo + (hi-lo+1)/2 // 上中位数，保证收敛
		size := 8 + 2*mid
		ok := size <= boundBytes
		steps = append(steps, traceStep{lo, hi, mid, size, ok})
		if ok {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	reachable := 8+2*lo <= boundBytes // 区间内是否存在任何可行 r
	return lo, reachable, len(steps), steps
}

type report struct {
	Name              string  `json:"name"`
	Desc              string  `json:"desc"`
	Symbols           int     `json:"symbols"`
	EntropyBps        float64 `json:"entropy_bits_per_symbol"`
	BoundBytes        float64 `json:"entropy_bound_bytes"`
	RLEBytes          int     `json:"rle_bytes"`
	PGMBytes          int     `json:"pgm_bytes"`
	DiffBytes         float64 `json:"rle_minus_bound_bytes"`
	Runs              int     `json:"rle_runs"`
	RoundTripOK       bool    `json:"roundtrip_byte_identical"`
	ActualWithinBound bool    `json:"actual_rle_within_entropy_bound"`
	Bisect            struct {
		IntervalLo  int         `json:"interval_lo"`
		IntervalHi  int         `json:"interval_hi"`
		Steps       int         `json:"steps"`
		MaxFeasible int         `json:"max_feasible_runs"`
		Reachable   bool        `json:"bound_reachable"`
		Trace       []traceStep `json:"trace"`
	} `json:"bisect"`
	Files struct {
		PGMIn  string `json:"pgm_before"`
		RLE    string `json:"rle"`
		PGMOut string `json:"pgm_after"`
	} `json:"files"`
	Waste string `json:"waste_note"`
}

func main() {
	outDir := flag.String("outdir", "artifacts/roundtrip", "互转文件与报告输出目录")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "建目录失败:", err)
		os.Exit(1)
	}

	samples := genSamples()
	reports := make([]report, 0, len(samples))

	fmt.Println("================ PGM <-> RLE 互转与信息熵对照 ================")
	fmt.Printf("画幅 %dx%d，像素数 N=%d；RLE 格式：8 字节 GTR1 头 + 每游程 2 字节(计数,值)，计数封顶 255\n", W, H, N)
	fmt.Println("熵下界 B = N·H(X)/8（字节，按无记忆一阶熵）；实际体积取 RLE 字节数；差值 = RLE − B")
	fmt.Println()

	for _, s := range samples {
		r := report{Name: s.Name, Desc: s.Desc}

		// 1) 信息熵与体积下界
		h := entropyBits(s.Pix)
		bound := float64(N) * h / 8.0
		symbols := 0
		var seen [256]bool
		for _, v := range s.Pix {
			if !seen[v] {
				seen[v] = true
				symbols++
			}
		}
		r.EntropyBps = math.Round(h*10000) / 10000
		r.BoundBytes = math.Round(bound*100) / 100
		r.Symbols = symbols

		// 2) PGM 编码 -> RLE 编码 -> 解 RLE -> 再编码 PGM，全程走 api
		pgmIn, err := api.Encode(&api.Image{W: W, H: H, Pix: s.Pix}, api.Options{Format: "pgm"})
		must(err)
		img1, err := api.Decode(pgmIn, api.Options{Format: "pgm"})
		must(err)
		rleBody, err := api.Encode(img1, api.Options{Format: "rle"})
		must(err)
		img2, err := api.Decode(rleBody, api.Options{Format: "rle"})
		must(err)
		pgmOut, err := api.Encode(img2, api.Options{Format: "pgm"})
		must(err)

		r.PGMBytes = len(pgmIn)
		r.RLEBytes = len(rleBody)
		r.DiffBytes = math.Round((float64(len(rleBody))-bound)*100) / 100
		r.Runs = countRuns(s.Pix)
		r.RoundTripOK = bytes.Equal(img2.Pix, s.Pix) && bytes.Equal(pgmOut, pgmIn)

		// 3) 二分：游程数维度上逼近熵下界
		maxR, reachable, steps, tr := bisectMaxRuns(int(math.Ceil(bound)))
		r.Bisect.IntervalLo, r.Bisect.IntervalHi = 0, N
		r.Bisect.Steps = steps
		r.Bisect.MaxFeasible = maxR
		r.Bisect.Reachable = reachable
		r.Bisect.Trace = tr
		// 该图自身的实际 RLE 是否落在下界内
		actualWithin := float64(len(rleBody)) <= bound
		r.ActualWithinBound = actualWithin

		// 4) 落盘：互转前后各一份，便于复跑。
		// JSON 只存文件名，保证换输出目录复跑时报告仍可逐字节比对；完整路径在终端打印。
		inName, rleName, outName := s.Name+".in.pgm", s.Name+".rle", s.Name+".out.pgm"
		r.Files.PGMIn, r.Files.RLE, r.Files.PGMOut = inName, rleName, outName
		must(os.WriteFile(filepath.Join(*outDir, inName), pgmIn, 0o644))
		must(os.WriteFile(filepath.Join(*outDir, rleName), rleBody, 0o644))
		must(os.WriteFile(filepath.Join(*outDir, outName), pgmOut, 0o644))

		// 5) 差值归因
		r.Waste = wasteNote(s.Name, r.Runs, r.RLEBytes, bound, maxR, reachable)
		reports = append(reports, r)
	}

	// 三份样本各一行：熵 / 实际体积 / 差值
	fmt.Println("【对照表】每份样本一行（熵含 bit/符号 与换算成字节的下界）")
	fmt.Printf("%-6s %-28s %12s %12s %12s %s\n", "样本", "熵 H(X)", "熵下界B(字节)", "RLE实际(字节)", "差值(字节)", "往返")
	for _, r := range reports {
		fmt.Printf("%-6s %-28s %12.2f %12d %12.2f %s\n",
			r.Name,
			fmt.Sprintf("%.4f bit/符号", r.EntropyBps),
			r.BoundBytes,
			r.RLEBytes,
			r.DiffBytes,
			map[bool]string{true: "逐字节相同", false: "不一致"}[r.RoundTripOK],
		)
	}
	fmt.Println()

	for _, r := range reports {
		fmt.Printf("---------------- %s ----------------\n", r.Name)
		fmt.Printf("说明: %s\n", r.Desc)
		fmt.Printf("符号数=%d  游程数=%d  PGM=%d 字节  RLE=%d 字节  熵下界=%.2f 字节  差值=%+.2f 字节\n",
			r.Symbols, r.Runs, r.PGMBytes, r.RLEBytes, r.BoundBytes, r.DiffBytes)
		fmt.Printf("互转校验: 原始像素 == 往返像素 且 in.pgm == out.pgm : %v\n", r.RoundTripOK)
		fmt.Printf("二分: 区间 [%d, %d]，步数 %d，谓词 8+2r <= ceil(B)=%d\n",
			r.Bisect.IntervalLo, r.Bisect.IntervalHi, r.Bisect.Steps, int(math.Ceil(r.BoundBytes)))
		for _, t := range r.Bisect.Trace {
			fmt.Printf("    lo=%-4d hi=%-4d mid=%-4d -> GTR1(mid)=%4d 字节, 可行=%v\n",
				t.Lo, t.Hi, t.Mid, t.MidSize, t.Feasible)
		}
		if !r.Bisect.Reachable {
			fmt.Printf("结果: 【目标不可达】下界 %.2f 字节容纳不下任何合法 GTR1（最小 r=1 就要 10 字节）；二分形式最接近值 r*=%d -> %d 字节，但 r=0 编不了任何像素、不是合法文件；本图实际输出 %d 字节才是可落地的最接近值，已标注\n",
				r.BoundBytes, r.Bisect.MaxFeasible, 8+2*r.Bisect.MaxFeasible, r.RLEBytes)
		} else if r.ActualWithinBound {
			fmt.Printf("结果: 目标可达：最大可行游程数 r*=%d（对应 %d 字节 <= 下界 %.2f）；本图实际 %d 游程、RLE %d 字节，未超下界\n",
				r.Bisect.MaxFeasible, 8+2*r.Bisect.MaxFeasible, r.BoundBytes, r.Runs, r.RLEBytes)
		} else {
			fmt.Printf("结果: 【对本图不可达】二分给出格式允许的最接近值 r*=%d -> %d 字节（<= 下界 %.2f），但本图实际切成 %d 游程、RLE %d 字节，超出下界；%d 字节即该图能得到的最接近真实输出，已标注\n",
				r.Bisect.MaxFeasible, 8+2*r.Bisect.MaxFeasible, r.BoundBytes, r.Runs, r.RLEBytes, r.RLEBytes)
		}
		fmt.Printf("差值归因: %s\n", r.Waste)
		fmt.Printf("落盘: %s | %s | %s\n",
			filepath.Join(*outDir, r.Files.PGMIn),
			filepath.Join(*outDir, r.Files.RLE),
			filepath.Join(*outDir, r.Files.PGMOut))
		fmt.Println()
	}

	reportPath := filepath.Join(*outDir, "report.json")
	body, err := json.MarshalIndent(reports, "", "  ")
	must(err)
	must(os.WriteFile(reportPath, body, 0o644))
	fmt.Printf("机器可读报告: %s\n", reportPath)
}

func wasteNote(name string, runs, rleSize int, bound float64, maxR int, reachable bool) string {
	switch name {
	case "flat":
		return "差值 +42 字节全部是格式固定成本，分两处：① 8 字节 GTR1 头（魔数4+宽高4）；" +
			"② 计数字节只有 8 位、封顶 255，4096 个等长像素被迫切成 17 个游程（16×255+16），" +
			"本可 1 个游程表达的内容多写了 16 对 (计数,值)=32 字节。单符号熵为 0，下界只反映信息量、不承载格式开销，这 42 字节无法靠换符号编码消除。"
	case "band":
		return "差值为负不是浪费：游程编码利用了行间强相关，64 个游程共 128 字节负载 + 8 字节头；" +
			"无记忆一阶熵（6.0 bit/符号）忽略空间结构，真实条件熵更低。固定成本仍是 8 字节头，占 5.9%。"
	default: // noise
		return fmt.Sprintf("差值 +%.2f 字节即编码器浪费所在：白噪声几乎无相邻相等，%d 个像素切成 %d 个游程，"+
			"每游程 (计数,值) 固定 2 字节，负载相对原始像素膨胀 100%%（%d->%d 字节），再加 8 字节头；"+
			"计数字节对长度 1 的游程纯属冗余，这是 RLE 面对高熵数据的结构性浪费。",
			float64(rleSize)-bound, N, runs, N, 2*runs)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "失败:", err)
		os.Exit(1)
	}
}
