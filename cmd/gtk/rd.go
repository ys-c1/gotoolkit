package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"

	"gotoolkit/api"
)

// 四档常规目标 bpp，外加一档极端值（目标高到反解步长为 0，用来走错误码路径）。
var targetBPPs = []float64{0.5, 1.0, 2.0, 4.0}

const extremeBPP = 32.0

var rdFormats = []string{"pgm", "bmp", "rle"}

// rawRecord 是每一档（格式×目标）的一行原始记录，JSONL 落盘。
type rawRecord struct {
	Format      string  `json:"format"`
	TargetBPP   float64 `json:"target_bpp"`
	TargetBits  int     `json:"target_bits"`
	ActualBPP   float64 `json:"actual_bpp,omitempty"`
	ActualBits  int     `json:"actual_bits,omitempty"`
	Bytes       int     `json:"bytes,omitempty"`
	Step        int     `json:"step,omitempty"`
	InitialStep int     `json:"initial_step,omitempty"`
	Reachable   bool    `json:"reachable,omitempty"`
	GapBits     int     `json:"gap_bits,omitempty"`
	GapBPP      float64 `json:"gap_bpp,omitempty"`
	GapRatio    float64 `json:"gap_ratio,omitempty"`
	Extreme     bool    `json:"extreme,omitempty"`
	ErrorCode   string  `json:"error_code,omitempty"`
	Error       string  `json:"error,omitempty"`
}

func runRD(args []string) int {
	fs := flag.NewFlagSet("rd", flag.ContinueOnError)
	in := fs.String("in", "", "输入 PGM")
	recordPath := fs.String("record", "artifacts/rd-records.jsonl", "原始记录输出路径（JSONL，每档一行）")
	threshold := fs.Float64("threshold", 0.1, "差距比例红标阈值（超过即标红）")
	noColor := fs.Bool("no-color", false, "关闭红色标注（NO_COLOR 环境变量同样生效）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in")
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	img, err := api.Decode(raw, api.Options{Format: "pgm"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码失败:", err)
		return 1
	}

	color := !*noColor && os.Getenv("NO_COLOR") == ""
	red := func(s string) string {
		if !color {
			return s
		}
		return "\x1b[31m" + s + "\x1b[0m"
	}

	rf, err := os.Create(*recordPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "原始记录创建失败:", err)
		return 1
	}
	defer rf.Close()
	enc := json.NewEncoder(rf)

	all := append(append([]float64{}, targetBPPs...), extremeBPP)
	var rows []rawRecord
	var unreachable []rawRecord

	fmt.Printf("率失真对账  图=%dx%d  红标阈值=%.0f%%  记录=%s\n", img.W, img.H, *threshold*100, *recordPath)
	fmt.Println("格式  目标bpp   目标bits   实际bpp    实际bits   步长  反解步长  差距bits  差距比例   可达")
	fmt.Println("----  --------  --------  ---------  --------  ----  --------  --------  --------  ----")

	for _, f := range rdFormats {
		for _, bpp := range all {
			rec := rawRecord{Format: f, TargetBPP: bpp, Extreme: bpp == extremeBPP}
			rec.TargetBits = int(math.Round(bpp * float64(img.W*img.H)))
			opt := api.Options{Format: f, EnableRD: true, TargetBPP: bpp}
			report, rerr := api.EncodeRD(img, opt)
			if rerr != nil {
				rec.ErrorCode = errorCode(rerr)
				rec.Error = rerr.Error()
				_ = enc.Encode(rec)
				rows = append(rows, rec)
				fmt.Printf("%-4s  %-8.3f  %8d  %9s  %8s  %4s  %8s  %8s  %8s  %s\n",
					f, bpp, rec.TargetBits, "-", "-", "-", "-", "-", "-", red("ERR:"+rec.ErrorCode))
				continue
			}
			rec.TargetBits = report.TargetBits
			rec.ActualBPP = report.ActualBPP
			rec.ActualBits = report.ActualBits
			rec.Bytes = report.Bytes
			rec.Step = report.Step
			rec.InitialStep = report.InitialStep
			rec.Reachable = report.Reachable
			rec.GapBits = report.GapBits
			rec.GapBPP = report.GapBPP
			rec.GapRatio = report.GapRatio
			_ = enc.Encode(rec)
			rows = append(rows, rec)
			if !report.Reachable {
				unreachable = append(unreachable, rec)
			}

			ratio := fmt.Sprintf("%.2f%%", report.GapRatio*100)
			reach := "是"
			if !report.Reachable {
				reach = "否"
			}
			if report.GapRatio > *threshold {
				ratio = red(ratio)
				reach = red(reach)
			}
			fmt.Printf("%-4s  %-8.3f  %8d  %9.4f  %8d  %4d  %8d  %8d  %8s  %s\n",
				f, bpp, report.TargetBits, report.ActualBPP, report.ActualBits,
				report.Step, report.InitialStep, report.GapBits, ratio, reach)
		}
	}

	if len(unreachable) > 0 {
		fmt.Println("\n目标不可达明细（给出最接近结果与差距）:")
		for _, r := range unreachable {
			fmt.Printf("  %s 目标 bpp=%.3f (%d bits) 不可达；最接近步长=%d，实际 bpp=%.4f (%d bits)，差距 %d bits / %.2f%%\n",
				r.Format, r.TargetBPP, r.TargetBits, r.Step, r.ActualBPP, r.ActualBits, r.GapBits, r.GapRatio*100)
		}
	}

	// 十二组（3 格式 × 4 常规档）逐条对账，缺一组即非零退出。
	ok := verifyTwelve(rows, os.Stdout)
	if !ok {
		return 1
	}
	return 0
}

// verifyTwelve 校验十二组常规档全部产出有效报告（极端档不参与十二组对账）。
func verifyTwelve(rows []rawRecord, w io.Writer) bool {
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Extreme {
			continue
		}
		if r.ErrorCode != "" {
			fmt.Fprintf(w, "对账失败: %s bpp=%.3f 返回错误 %s\n", r.Format, r.TargetBPP, r.ErrorCode)
			return false
		}
		seen[fmt.Sprintf("%s:%.3f", r.Format, r.TargetBPP)] = true
	}
	for _, f := range rdFormats {
		for _, b := range targetBPPs {
			key := fmt.Sprintf("%s:%.3f", f, b)
			if !seen[key] {
				fmt.Fprintf(w, "对账失败: 缺少 %s\n", key)
				return false
			}
		}
	}
	fmt.Fprintf(w, "\n十二组对账全部通过（3 格式 × 4 档），另含极端档 bpp=%.1f 一行原始记录。\n", extremeBPP)
	return true
}

// errorCode 把哨兵错误映射成稳定的短码，写进原始记录。
func errorCode(err error) string {
	switch {
	case errors.Is(err, api.ErrBadTarget):
		return "ERR_BAD_TARGET"
	case errors.Is(err, api.ErrZeroStep):
		return "ERR_ZERO_STEP"
	case errors.Is(err, api.ErrBadOption):
		return "ERR_BAD_OPTION"
	case errors.Is(err, api.ErrUnknownFormat):
		return "ERR_UNKNOWN_FORMAT"
	default:
		return "ERR_UNKNOWN"
	}
}
