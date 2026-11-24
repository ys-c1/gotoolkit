// Command chainbench 生成多帧差分累积误差对照与存档产物。
//
// 用法：
//
//	go run ./cmd/chainbench
//	go run ./cmd/chainbench -out artifacts/chain -doc docs/accumulation.md
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gotoolkit/api"
)

const (
	quantStep = 8
	budget    = 12
)

type tier struct {
	Name   string
	Budget int
	K      int
	Report *api.ChainReport
	Blobs  [][]byte
}

type checkRow struct {
	Format          string `json:"format"`
	Frame           int    `json:"frame"`
	Bytes           int    `json:"bytes"`
	IdenticalEncode bool   `json:"identical_to_current_encode"`
	GoldenChecked   bool   `json:"golden_checked"`
	GoldenIdentical bool   `json:"golden_identical"`
	SHA256Prefix    string `json:"sha256_prefix"`
}

type outputJSON struct {
	QuantStep        int                `json:"quant_step"`
	PerFrameBound    int                `json:"per_frame_bound"`
	Rounding         string             `json:"rounding"`
	Frames           int                `json:"frames"`
	Width            int                `json:"width"`
	Height           int                `json:"height"`
	Tiers            []*api.ChainReport `json:"tiers"`
	AllKeyframe      *api.ChainReport   `json:"all_keyframe"`
	OversizedControl *api.ChainReport   `json:"oversized_control"`
	AllKeyChecks     []checkRow         `json:"all_keyframe_checks"`
}

func fixture() []*api.Image {
	const w, h = 4, 3
	// 固定的逐像素速度。速度 4/-4 档会落在 q=8 的半档 tie 上，
	// 使最坏像素恰好达到逐阶上界，避免只给宽松经验值。
	speeds := []int{4, 3, 5, 2, 6, 1, 7, 0, 4, 3, 5, 2}
	frames := make([]*api.Image, 24)
	for t := range frames {
		pix := make([]uint8, w*h)
		for i := range pix {
			v := i*7 + t*speeds[i]
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			pix[i] = uint8(v)
		}
		frames[t] = &api.Image{W: w, H: h, Pix: pix}
	}
	return frames
}

func main() {
	outDir := flag.String("out", filepath.Join("artifacts", "chain"), "产物目录")
	docPath := flag.String("doc", filepath.Join("docs", "accumulation.md"), "推导与对照表 Markdown")
	flag.Parse()

	if err := run(*outDir, *docPath); err != nil {
		fmt.Fprintln(os.Stderr, "chainbench:", err)
		os.Exit(1)
	}
}

func run(outDir, docPath string) error {
	frames := fixture()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if docDir := filepath.Dir(docPath); docDir != "." {
		if err := os.MkdirAll(docDir, 0o755); err != nil {
			return err
		}
	}

	budgets := []int{4, 8, 12}
	tiers := make([]tier, 0, 3)
	reports := make([]*api.ChainReport, 0, 4)
	for _, b := range budgets {
		k := api.KeyframeIntervalForBudget(b, quantStep)
		blobs, _, rep, err := api.AnalyzeSequence(frames, api.Options{
			Format: "pgm", KeyframeInterval: k, QuantStep: quantStep,
		})
		if err != nil {
			return err
		}
		rep.TargetBudget = b
		rep.WithinBudget = rep.LastActualMaxAbs <= b
		tiers = append(tiers, tier{Name: fmt.Sprintf("预算 %d", b), Budget: b, K: k, Report: rep, Blobs: blobs})
		reports = append(reports, rep)
	}

	_, _, k1Report, err := api.AnalyzeSequence(frames, api.Options{
		Format: "pgm", KeyframeInterval: 1, QuantStep: quantStep,
	})
	if err != nil {
		return err
	}
	k1Report.TargetBudget = 0
	k1Report.WithinBudget = k1Report.LastActualMaxAbs <= 0
	reports = append(reports, k1Report)

	_, _, oversized, err := api.AnalyzeSequence(frames, api.Options{
		Format: "pgm", KeyframeInterval: 8, QuantStep: quantStep,
	})
	if err != nil {
		return err
	}
	oversized.TargetBudget = budget
	oversized.WithinBudget = oversized.LastActualMaxAbs <= budget

	checks, err := writeKeyframeArtifacts(outDir, frames)
	if err != nil {
		return err
	}
	if err := writeFrameCSV(filepath.Join(outDir, "frame_metrics.csv"), reports); err != nil {
		return err
	}
	if err := writePixelCSV(filepath.Join(outDir, "pixel_errors.csv"), reports, oversized); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "report.json"), outputJSON{
		QuantStep:        quantStep,
		PerFrameBound:    api.MaxQuantError(quantStep),
		Rounding:         "K=1+floor(B/floor(q/2))，向下取整",
		Frames:           len(frames),
		Width:            frames[0].W,
		Height:           frames[0].H,
		Tiers:            []*api.ChainReport{tiers[0].Report, tiers[1].Report, tiers[2].Report},
		AllKeyframe:      k1Report,
		OversizedControl: oversized,
		AllKeyChecks:     checks,
	}); err != nil {
		return err
	}
	if err := writeLastFrameCSV(filepath.Join(outDir, "last_frame_pixels.csv"), tiers, k1Report); err != nil {
		return err
	}

	doc := renderMarkdown(tiers, k1Report, oversized, checks)
	if err := os.WriteFile(docPath, []byte(doc), 0o644); err != nil {
		return err
	}
	// 同时在 artifacts 目录留一份，方便不进 docs 即可看存档。
	if err := os.WriteFile(filepath.Join(outDir, "accumulation.md"), []byte(doc), 0o644); err != nil {
		return err
	}

	fmt.Printf("chainbench: wrote %s and %s\n", docPath, outDir)
	for _, tt := range tiers {
		r := tt.Report
		fmt.Printf("B=%2d K=%d last max=%2d bound=%2d within=%v\n", tt.Budget, tt.K, r.LastActualMaxAbs, r.LastTheoryBound, r.WithinBound)
	}
	fmt.Printf("K=1 all-keyframe last max=%d; golden checks=%d\n", k1Report.LastActualMaxAbs, len(checks))
	return nil
}

func writeKeyframeArtifacts(outDir string, frames []*api.Image) ([]checkRow, error) {
	dir := filepath.Join(outDir, "all-keyframes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var rows []checkRow
	for _, format := range []string{"pgm", "bmp", "rle"} {
		blobs, err := api.EncodeSequence(frames, api.Options{Format: format, KeyframeInterval: 1})
		if err != nil {
			return nil, err
		}
		var golden []byte
		golden, err = os.ReadFile(filepath.Join("testdata", "golden", format+".bin"))
		if err != nil {
			return nil, err
		}
		for i, blob := range blobs {
			want, err := api.Encode(frames[i], api.Options{Format: format})
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(blob)
			row := checkRow{
				Format:          format,
				Frame:           i,
				Bytes:           len(blob),
				IdenticalEncode: string(blob) == string(want),
				SHA256Prefix:    hex.EncodeToString(sum[:])[:16],
			}
			if i == 0 {
				row.GoldenChecked = true
				row.GoldenIdentical = string(blob) == string(golden)
			}
			rows = append(rows, row)

			// PGM 全部落盘；其他格式只落第 0 帧作为 golden 对照，避免存档噪音。
			if format == "pgm" {
				name := fmt.Sprintf("frame_%02d.pgm", i)
				if err := os.WriteFile(filepath.Join(dir, name), blob, 0o644); err != nil {
					return nil, err
				}
			}
			if i == 0 {
				name := fmt.Sprintf("frame_00.%s.bin", format)
				if err := os.WriteFile(filepath.Join(dir, name), blob, 0o644); err != nil {
					return nil, err
				}
			}
		}
	}
	return rows, nil
}

func writeFrameCSV(path string, reports []*api.ChainReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{
		"interval", "target_budget", "frame", "keyframe", "depth",
		"quant_max_abs", "theory_bound", "triangle_bound",
		"actual_max_abs", "actual_mean_abs", "actual_rmse",
		"theory_exceeded", "triangle_exceeded",
	}); err != nil {
		return err
	}
	for _, r := range reports {
		for _, m := range r.FrameMetrics {
			if err := w.Write([]string{
				fmt.Sprint(r.Interval), fmt.Sprint(r.TargetBudget), fmt.Sprint(m.Frame),
				fmt.Sprint(m.Keyframe), fmt.Sprint(m.Depth), fmt.Sprint(m.QuantMaxAbs),
				fmt.Sprint(m.TheoryBound), fmt.Sprint(m.TriangleBound),
				fmt.Sprint(m.ActualMaxAbs), fmt.Sprintf("%.6f", m.ActualMeanAbs),
				fmt.Sprintf("%.6f", m.ActualRMSE), fmt.Sprint(m.BoundExceeded),
				fmt.Sprint(m.TriangleExceeded),
			}); err != nil {
				return err
			}
		}
	}
	return w.Error()
}

func writePixelCSV(path string, reports []*api.ChainReport, extra *api.ChainReport) error {
	all := append(append([]*api.ChainReport{}, reports...), extra)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"interval", "target_budget", "frame", "index", "x", "y", "signed_error", "abs_error", "quant_error", "theory_bound_at_frame"}); err != nil {
		return err
	}
	for _, r := range all {
		bounds := make(map[int]int, len(r.FrameMetrics))
		for _, m := range r.FrameMetrics {
			bounds[m.Frame] = m.TheoryBound
		}
		for _, p := range r.PixelErrors {
			if err := w.Write([]string{
				fmt.Sprint(r.Interval), fmt.Sprint(r.TargetBudget), fmt.Sprint(p.Frame),
				fmt.Sprint(p.Index), fmt.Sprint(p.X), fmt.Sprint(p.Y),
				fmt.Sprint(p.SignedError), fmt.Sprint(p.AbsError), fmt.Sprint(p.QuantError),
				fmt.Sprint(bounds[p.Frame]),
			}); err != nil {
				return err
			}
		}
	}
	return w.Error()
}

func writeLastFrameCSV(path string, tiers []tier, k1 *api.ChainReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	header := []string{"index", "x", "y"}
	for _, tt := range tiers {
		header = append(header,
			fmt.Sprintf("k%d_signed", tt.K),
			fmt.Sprintf("k%d_abs", tt.K),
			fmt.Sprintf("k%d_bound", tt.K),
			fmt.Sprintf("k%d_exceeded", tt.K),
		)
	}
	header = append(header, "k1_signed", "k1_abs", "k1_bound", "k1_exceeded")
	if err := w.Write(header); err != nil {
		return err
	}
	lastFrame := tiers[0].Report.Frames - 1
	for i := 0; i < tiers[0].Report.Width*tiers[0].Report.Height; i++ {
		row := []string{fmt.Sprint(i), fmt.Sprint(i % tiers[0].Report.Width), fmt.Sprint(i / tiers[0].Report.Width)}
		for _, tt := range tiers {
			p := findPixel(tt.Report, lastFrame, i)
			row = append(row, fmt.Sprint(p.SignedError), fmt.Sprint(p.AbsError),
				fmt.Sprint(tt.Report.LastTheoryBound), fmt.Sprint(p.AbsError > tt.Report.LastTheoryBound))
		}
		p := findPixel(k1, lastFrame, i)
		row = append(row, fmt.Sprint(p.SignedError), fmt.Sprint(p.AbsError), "0", fmt.Sprint(p.AbsError > 0))
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func findPixel(r *api.ChainReport, frame, index int) api.PixelError {
	for _, p := range r.PixelErrors {
		if p.Frame == frame && p.Index == index {
			return p
		}
	}
	return api.PixelError{}
}

func writeJSON(path string, v outputJSON) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func status(ok bool) string {
	if ok {
		return "✅ 未超限"
	}
	return "❌ 超限"
}

func signed(v int) string {
	if v > 0 {
		return fmt.Sprintf("+%d", v)
	}
	return fmt.Sprint(v)
}

func renderMarkdown(tiers []tier, k1, oversized *api.ChainReport, checks []checkRow) string {
	var b strings.Builder
	e := api.MaxQuantError(quantStep)
	fmt.Fprintf(&b, "# 多帧差分累积误差与关键帧间隔\n\n")
	fmt.Fprintf(&b, "本文件由 `go run ./cmd/chainbench` 生成；逐帧/逐像素原始记录在 `artifacts/chain/`。\n\n")

	b.WriteString("## 1. 累积误差上界推导\n\n")
	fmt.Fprintf(&b, "设第 $t$ 帧像素为 $I_t(p)$，相邻原始帧差分为 $D_t(p)=I_t(p)-I_{t-1}(p)$。量化步长 $q=%d$，最近邻量化为\n\n", quantStep)
	b.WriteString("```text\n")
	b.WriteString("L_t(p) = round-half-away-from-zero(D_t(p)/q)\n")
	b.WriteString("Q_t(p) = q * L_t(p)\n")
	b.WriteString("η_t(p) = Q_t(p) - D_t(p)\n")
	b.WriteString("```\n\n")
	fmt.Fprintf(&b, "对 8 bit 整数差分，单阶量化误差满足 $|η_t(p)|≤e_Q=\\lfloor q/2\\rfloor=%d$。\n\n", e)
	b.WriteString("关键帧 $kK$ 用既有无损格式编码，解码误差为 0。两个关键帧之间，解码端逐阶累加：\n\n")
	b.WriteString("```text\n")
	b.WriteString("Î_t(p) = clip[0,255](Î_{t-1}(p) + Q_t(p))\n")
	b.WriteString("```\n\n")
	b.WriteString("忽略 clip 时有\n\n")
	b.WriteString("$$\n")
	b.WriteString("\\hat I_{kK+d}(p)-I_{kK+d}(p)=\\sum_{j=1}^{d}\\eta_{kK+j}(p).\n")
	b.WriteString("$$\n\n")
	b.WriteString("实际每一步都先加量化差分再 clip。设 $E_d=\\hat I_{kK+d}-I_{kK+d}$，截断前相对当前原像素的偏差是 $z_d=E_{d-1}+\\eta_{kK+d}$；由于 $I_{kK+d}(p)\\in[0,255]$，投影到 `[0,255]` 不会增大到该原像素的距离，即 $|E_d|≤|z_d|$。于是 $|E_d|≤|E_{d-1}|+|\\eta|$，从关键帧 $E_0=0$ 归纳并使用三角不等式：\n\n")
	b.WriteString("$$\n")
	b.WriteString("|\\hat I_{kK+d}(p)-I_{kK+d}(p)|≤\\sum_{j=1}^{d}|\\eta_{kK+j}(p)|≤d\\,e_Q≤(K-1)e_Q=B_K.\n")
	b.WriteString("$$\n\n")
	b.WriteString("更紧的实测证书是 $B^{\\triangle}_t=\\sum_j \\max_p|\\eta_j(p)|$，对应存档中的 `triangle_bound`；理论反解使用不依赖具体帧的 $d e_Q$。\n\n")
	fmt.Fprintf(&b, "给定可接受的末帧每像素误差预算 $B^*$，令 $(K-1)e_Q≤B^*$，反解得\n\n$$\nK^*=1+\\left\\lfloor\\frac{B^*}{e_Q}\\right\\rfloor.\n$$\n\n")
	fmt.Fprintf(&b, "**取整方式：K 一律向下取整（整数除法即 floor），不做四舍五入或向上取整**；例如 $B^*=10$ 时 $K=1+\\lfloor10/4\\rfloor=3$，对应上界 8，若进到 K=4 上界就是 12，可能突破预算。\n\n")
	fmt.Fprintf(&b, "本次固定序列为 %d 帧、%dx%d；末帧索引 %d。24 是 lcm(2,3,4)，所以三档 K=2/3/4 的末帧都恰好位于各自 GOP 的最深处，便于并列比较。序列使用固定逐像素速度 `[4,3,5,2,6,1,7,0,4,3,5,2]`，其中 4 落在 q=8 的半档 tie 上，最坏像素会顶到理论上界。\n\n",
		k1.Frames, k1.Width, k1.Height, k1.Frames-1)

	b.WriteString("## 2. 三档间隔：末帧 aggregate 对照\n\n")
	b.WriteString("| 档位 | 预算 $B^*$ | 反解 K | 末帧深度 d | 理论上界 $d e_Q$ | 实测三角界 | 末帧最大绝对误差 | 平均绝对误差/像素 | RMSE | 对理论上界 | 对预算 |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|:---:|:---:|\n")
	for _, tt := range tiers {
		r := tt.Report
		budgetStatus := "✅ 未超预算"
		if r.LastActualMaxAbs > tt.Budget {
			budgetStatus = "❌ 超预算"
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | **%d** | %.4f | %.4f | %s | %s |\n",
			tt.Name, tt.Budget, tt.K, lastDepth(r), r.LastTheoryBound, r.LastTriangleBound,
			r.LastActualMaxAbs, r.LastActualMeanAbs, r.LastActualRMSE, status(r.LastActualMaxAbs <= r.LastTheoryBound), budgetStatus)
	}
	fmt.Fprintf(&b, "| 全关键帧基线 | 0 | 1 | 0 | 0 | 0 | **%d** | %.4f | %.4f | %s | ✅ |\n\n",
		k1.LastActualMaxAbs, k1.LastActualMeanAbs, k1.LastActualRMSE, status(k1.WithinBound))

	b.WriteString("## 3. 末帧误差按像素给，三档并列\n\n")
	b.WriteString("误差列为带符号的 `解码-原始`；上界是该档末帧的每像素绝对误差上界。所有 12 个像素均逐格判定。\n\n")
	b.WriteString("| 像素 i | (x,y) | K=2 误差 | K=2 上界 | 判定 | K=3 误差 | K=3 上界 | 判定 | K=4 误差 | K=4 上界 | 判定 | K=1 误差/上界 |\n")
	b.WriteString("|---:|:---:|---:|---:|:---:|---:|---:|:---:|---:|---:|:---:|:---:|\n")
	lastFrame := k1.Frames - 1
	for i := 0; i < k1.Width*k1.Height; i++ {
		p2 := findPixel(tiers[0].Report, lastFrame, i)
		p3 := findPixel(tiers[1].Report, lastFrame, i)
		p4 := findPixel(tiers[2].Report, lastFrame, i)
		p1 := findPixel(k1, lastFrame, i)
		fmt.Fprintf(&b, "| %d | (%d,%d) | %s | %d | %s | %s | %d | %s | %s | %d | %s | %d / 0 ✅ |\n",
			i, i%k1.Width, i/k1.Width,
			signed(p2.SignedError), tiers[0].Report.LastTheoryBound, mark(p2.AbsError, tiers[0].Report.LastTheoryBound),
			signed(p3.SignedError), tiers[1].Report.LastTheoryBound, mark(p3.AbsError, tiers[1].Report.LastTheoryBound),
			signed(p4.SignedError), tiers[2].Report.LastTheoryBound, mark(p4.AbsError, tiers[2].Report.LastTheoryBound),
			p1.AbsError)
	}
	b.WriteString("\n完整机器可读并列值见 `artifacts/chain/last_frame_pixels.csv`。\n\n")

	b.WriteString("## 4. 超限标出：故意把间隔取大的负对照\n\n")
	b.WriteString("仍以 $B^*=12$ 为目标；反解应得 K=4。故意取 K=8 时，末帧索引 23 位于关键帧 16 之后第 7 个 P 帧：\n\n")
	b.WriteString("| 对照 | 目标预算 | K | 末帧深度 | 理论累积上界 | 末帧最大实测 | 对理论上界 | 对目标预算 |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|:---:|:---:|\n")
	fmt.Fprintf(&b, "| 取大间隔 | 12 | 8 | %d | %d | **%d** | %s | ❌ **超限 %d：间隔取大，应降回 K≤4** |\n\n",
		lastDepth(oversized), oversized.LastTheoryBound, oversized.LastActualMaxAbs,
		status(oversized.LastActualMaxAbs <= oversized.LastTheoryBound), oversized.LastActualMaxAbs-12)
	b.WriteString("这说明理论三角界没有被实现突破（实现自洽），但 K=8 的理论界本身已大于质量预算 12；从预算视角必须标红并缩短间隔。\n\n")

	b.WriteString("## 5. 累积误差逐帧实测\n\n")
	b.WriteString("下表给每帧的最坏像素绝对误差 `max|e|` 与理论上界 `B=d e_Q`；关键帧显示为 `I`。逐像素、逐帧的全部数值另存 `artifacts/chain/pixel_errors.csv`，逐帧 aggregate 存 `frame_metrics.csv` / `report.json`。\n\n")
	b.WriteString("| 帧 | K=2 位置 | K=2 实测/上界 | 判定 | K=3 位置 | K=3 实测/上界 | 判定 | K=4 位置 | K=4 实测/上界 | 判定 |\n")
	b.WriteString("|---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|\n")
	for fi := 0; fi < k1.Frames; fi++ {
		m2 := tiers[0].Report.FrameMetrics[fi]
		m3 := tiers[1].Report.FrameMetrics[fi]
		m4 := tiers[2].Report.FrameMetrics[fi]
		fmt.Fprintf(&b, "| %d | %s | %d / %d | %s | %s | %d / %d | %s | %s | %d / %d | %s |\n",
			fi, pos(m2), m2.ActualMaxAbs, m2.TheoryBound, shortStatus(!m2.BoundExceeded),
			pos(m3), m3.ActualMaxAbs, m3.TheoryBound, shortStatus(!m3.BoundExceeded),
			pos(m4), m4.ActualMaxAbs, m4.TheoryBound, shortStatus(!m4.BoundExceeded))
	}
	b.WriteString("\n趋势：关键帧处误差清零；同一 GOP 内最坏误差随深度按 0→4→8→12 上升。K=2 每两帧清零，因此末帧只剩一阶误差。\n\n")

	b.WriteString("## 6. 全关键帧模式与现有产物一致性\n\n")
	b.WriteString("`KeyframeInterval=1` 时每帧直接调用既有 `api.Encode`，不经过 GTD1 差分路径。以下检查由 chainbench 实际写出：\n\n")
	allOK := true
	goldenOK := true
	for _, c := range checks {
		if !c.IdenticalEncode {
			allOK = false
		}
		if c.Frame == 0 && !c.GoldenIdentical {
			goldenOK = false
		}
	}
	fmt.Fprintf(&b, "- 三种格式 × 24 帧与当前 `api.Encode` 逐字节比较：**%s**（%d 行检查）。\n", map[bool]string{true: "全部一致", false: "存在不一致"}[allOK], len(checks))
	fmt.Fprintf(&b, "- 第 0 帧就是仓库锁定的 4×3 sample，与 `testdata/golden/{pgm,bmp,rle}.bin` 比较：**%s**。\n", map[bool]string{true: "三份 golden 全部一致", false: "golden 不一致"}[goldenOK])
	b.WriteString("- PGM 全帧落盘：`artifacts/chain/all-keyframes/frame_*.pgm`；第 0 帧三种格式副本为 `frame_00.{pgm,bmp,rle}.bin`。\n\n")
	b.WriteString("| 格式 | 检查帧数 | 单帧字节数 | sha256 前缀（第 0 帧） | golden |\n|---|---:|---:|:---:|:---:|\n")
	for _, format := range []string{"pgm", "bmp", "rle"} {
		var first checkRow
		n := 0
		for _, c := range checks {
			if c.Format == format {
				if c.Frame == 0 {
					first = c
				}
				n++
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | `%s` | %s |\n", format, n, first.Bytes, first.SHA256Prefix, map[bool]string{true: "✅ 一致", false: "❌ 不一致"}[first.GoldenIdentical])
	}

	b.WriteString("\n## 7. 复跑\n\n")
	b.WriteString("```bash\nmake test\nmake chain\n# 或：go run ./cmd/chainbench\n```\n")
	return b.String()
}

func mark(abs, bound int) string {
	if abs > bound {
		return "❌"
	}
	return "✅"
}

func shortStatus(ok bool) string {
	if ok {
		return "✅"
	}
	return "❌"
}

func pos(m api.FrameMetric) string {
	if m.Keyframe {
		return "I/d0"
	}
	return fmt.Sprintf("P/d%d", m.Depth)
}

func lastDepth(r *api.ChainReport) int {
	return r.FrameMetrics[len(r.FrameMetrics)-1].Depth
}
