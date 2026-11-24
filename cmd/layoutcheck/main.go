// Command layoutcheck 生成“三种格式 × 八种排布组合”的映射表，并对三份样本
// 各跑八种组合（3 份 × 8 组合 × 3 格式 = 72 次解码，其中按样本原生格式统计为 24 个结果），
// 核对：同组合跨格式逐像素相同、且与不带标志的旧文件一致。
//
// 样本布局（由 cmd/mksamples 生成）：
//
//	sampleN.src.pgm   逻辑像素基准
//	sampleN.<legacy>  旧文件，不带任何标志
//	sampleN.alt.<ext> 同一数据、另一种行序
//
// 旧文件组的来源：
//  1. testdata/golden/{pgm,bmp,rle}.bin —— 仓库自带、不许改的旧基准；
//  2. testdata/samples/sampleN.<legacy> —— 独立手写拼接的旧文件。
//
// 用法：go run ./cmd/layoutcheck -samples testdata/samples -out artifacts/layoutcheck
package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gotoolkit/api"
)

type combo struct {
	name      string
	rowOrder  string
	byteOrder string
	rowAlign  int
}

var combos = []combo{
	{"C0", "down", "le", 1},
	{"C1", "down", "le", 4},
	{"C2", "down", "be", 1},
	{"C3", "down", "be", 4},
	{"C4", "up", "le", 1},
	{"C5", "up", "le", 4},
	{"C6", "up", "be", 1},
	{"C7", "up", "be", 4},
}

var formats = []string{"pgm", "bmp", "rle"}

func (c combo) options(format string) api.Options {
	return api.Options{Format: format, RowOrder: c.rowOrder, ByteOrder: c.byteOrder, RowAlign: c.rowAlign}
}

type nativeSample struct {
	name   string
	ext    string // legacy 文件的扩展名：pgm | bmp | gtr
	format string // 旧文件解码时用的 api 格式名
}

var samples = []nativeSample{
	{"sample1", "pgm", "pgm"},
	{"sample2", "bmp", "bmp"},
	{"sample3", "gtr", "rle"},
}

func main() {
	samplesDir := flag.String("samples", "testdata/samples", "样本目录")
	goldenDir := flag.String("golden", "testdata/golden", "旧基准目录")
	outDir := flag.String("out", "artifacts/layoutcheck", "输出目录")
	flag.Parse()
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log := &logger{}
	fail := 0
	failf := func(format string, args ...any) { fail++; log.printf("FAIL "+format, args...) }

	// 1) 映射表：三格式 × 八组合，共 24 张，逐行列出。
	tableW, tableH := 5, 5
	pix := make([]uint8, tableW*tableH)
	for i := range pix {
		pix[i] = uint8(10 + i*4)
	}
	for _, f := range formats {
		for _, c := range combos {
			d, err := api.Describe(f, tableW, tableH, pix, c.options(f))
			if err != nil {
				failf("映射表 %s %s: %v", f, c.name, err)
				continue
			}
			log.raw(renderTable(d, c))
		}
	}

	// 2) 旧基准（不带标志）单独验：逐字节自洽 + 与 ref 一致。
	log.printf("== 旧文件组（不带任何标志） ==")
	goldenBaseline := map[string]*api.Image{}
	for _, f := range formats {
		path := filepath.Join(*goldenDir, f+".bin")
		raw, err := os.ReadFile(path)
		if err != nil {
			failf("%s: 读旧基准失败: %v", path, err)
			continue
		}
		img, err := api.Decode(raw, api.Options{Format: f})
		if err != nil {
			failf("%s: 默认参数解码失败: %v", path, err)
			continue
		}
		back, err := api.Encode(img, api.Options{Format: f})
		if err != nil {
			failf("%s: 默认参数重编码失败: %v", path, err)
			continue
		}
		if !bytes.Equal(back, raw) {
			failf("%s: 默认参数重编码与旧基准字节不同", path)
			continue
		}
		goldenBaseline[f] = img
		log.printf("  golden/%s.bin: %dx%d，默认参数往返逐字节一致（%d 字节）", f, img.W, img.H, len(raw))
	}

	// 2b) 样本旧文件逐字节自洽。
	baseline := map[string]*api.Image{}
	for _, s := range samples {
		raw, err := os.ReadFile(filepath.Join(*samplesDir, s.name+"."+s.ext))
		if err != nil {
			failf("%s: 读旧文件失败: %v", s.name, err)
			continue
		}
		img, err := api.Decode(raw, api.Options{Format: s.format})
		if err != nil {
			failf("%s: 旧文件默认参数解码失败: %v", s.name, err)
			continue
		}
		back, err := api.Encode(img, api.Options{Format: s.format})
		if err != nil {
			failf("%s: 旧文件默认参数重编码失败: %v", s.name, err)
			continue
		}
		if !bytes.Equal(back, raw) {
			failf("%s: 旧文件重编码字节不同", s.name)
			continue
		}
		src, err := os.ReadFile(filepath.Join(*samplesDir, s.name+".src.pgm"))
		if err != nil {
			failf("%s: 读逻辑基准失败: %v", s.name, err)
			continue
		}
		srcImg, err := api.Decode(src, api.Options{Format: "pgm"})
		if err != nil || !bytes.Equal(srcImg.Pix, img.Pix) || srcImg.W != img.W || srcImg.H != img.H {
			failf("%s: 旧文件解出的图与逻辑基准不同", s.name)
			continue
		}
		baseline[s.name] = img
		log.printf("  samples/%s.%s: %dx%d，默认参数往返逐字节一致，且与 .src.pgm 同图", s.name, s.ext, img.W, img.H)
	}

	// 2c) alt 文件（另一种行序）也必须解回同一张图，证明“同数据两种行序”。
	altFormats := map[string]string{"pgm": "pgm", "bmp": "bmp", "gtr": "rle"}
	for _, s := range samples {
		if baseline[s.name] == nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(*samplesDir, s.name+".alt."+s.ext))
		if err != nil {
			failf("%s: 读 alt 文件失败: %v", s.name, err)
			continue
		}
		// alt 与旧文件的行序相反：用该格式的另一种行序声明解码。
		altOpt := api.Options{Format: altFormats[s.ext]}
		if s.ext == "bmp" {
			altOpt.RowOrder = "down" // 旧 bmp 是 up，alt 是 down
		} else {
			altOpt.RowOrder = "up"
		}
		img, err := api.Decode(raw, altOpt)
		if err != nil {
			failf("%s: alt 文件解码失败: %v", s.name, err)
			continue
		}
		if !bytes.Equal(img.Pix, baseline[s.name].Pix) {
			failf("%s: alt 文件与旧文件解出的图像素不同（行序标志未区分）", s.name)
			continue
		}
		log.printf("  samples/%s.alt.%s: 用相反行序解码，与旧文件逐像素一致", s.name, s.ext)
	}

	// 3) 三份样本 × 八组合 = 24 个结果。
	log.printf("\n== 三份样本 × 八组合（共 24 个结果） ==")
	total := 0
	for _, s := range samples {
		base := baseline[s.name]
		if base == nil {
			continue
		}
		log.printf("\n-- %s %dx%d --", s.name, base.W, base.H)
		for _, c := range combos {
			reference := []byte(nil)
			var refFormat string
			sampleResults := 0
			for _, f := range formats {
				opt := c.options(f)
				body, err := api.Encode(base, opt)
				if err != nil {
					failf("%s %s/%s 编码失败: %v", s.name, f, c.name, err)
					continue
				}
				dec, err := api.Decode(body, opt)
				if err != nil {
					failf("%s %s/%s 解码失败: %v（%s）",
						s.name, f, c.name, err, diagnoseText(body, opt, base))
					continue
				}
				if !sameImage(dec, base) {
					failf("%s %s/%s 像素与旧文件不同（%s）",
						s.name, f, c.name, diagnoseText(body, opt, base))
					continue
				}
				if reference == nil {
					reference, refFormat = dec.Pix, f
				} else if !bytes.Equal(reference, dec.Pix) {
					failf("%s %s/%s 与同组合的 %s 解码结果不同（%s）",
						s.name, f, c.name, refFormat, diagnoseText(body, opt, base))
					continue
				}
				log.printf("  %s %-3s %-3s byte=%s align=%d  %4d 字节  OK 与%s同图",
					s.name, c.name, f, c.byteOrder, c.rowAlign, len(body), refFormat)
				sampleResults++
			}
			if sampleResults != len(formats) {
				failf("%s %s: 仅 %d/%d 个格式通过", s.name, c.name, sampleResults, len(formats))
			}
			total++
		}
	}

	// 3b) 旧基准组（三份样本即三份 golden）各按自身格式跑八组合，共 24 个结果。
	//     这是“三份样本 × 八组合”的另一种自然读法，与 3) 并列保留。
	log.printf("\n== 旧基准 × 八组合（三份 golden 各按自身格式，共 24 个结果） ==")
	goldenTotal := 0
	for _, f := range formats {
		base := goldenBaseline[f]
		if base == nil {
			continue
		}
		for _, c := range combos {
			opt := c.options(f)
			body, err := api.Encode(base, opt)
			if err != nil {
				failf("golden/%s %s 编码失败: %v", f, c.name, err)
				continue
			}
			dec, err := api.Decode(body, opt)
			if err != nil {
				failf("golden/%s %s 解码失败: %v（%s）", f, c.name, err, diagnoseText(body, opt, base))
				continue
			}
			if !sameImage(dec, base) {
				failf("golden/%s %s 像素与旧基准不同（%s）", f, c.name, diagnoseText(body, opt, base))
				continue
			}
			log.printf("  golden/%-3s %-3s row=%-4s byte=%s align=%d  OK", f, c.name, c.rowOrder, c.byteOrder, c.rowAlign)
			goldenTotal++
		}
	}
	log.printf("旧基准结果数: %d（期望 24）", goldenTotal)
	if goldenTotal != 24 {
		failf("旧基准结果数 = %d，未达到 24", goldenTotal)
	}

	log.printf("\n合计结果数: %d（期望 3 份 × 8 组合 = 24）", total)
	if total != 24 {
		failf("结果数 = %d，未达到 24，按未完成处理", total)
	}

	path := filepath.Join(*outDir, "report.txt")
	// 4) 故障注入：故意声称一个组合、实际按另一个组合写入，确认诊断能点名未生效的标志。
	log.printf("\n== 故障注入（验证“指认未生效标志”） ==")
	probe := baseline["sample1"]
	if probe == nil {
		failf("故障注入缺少 sample1 基线")
	}
	if probe != nil {
		// 4a) 三个标志一起错：应三个都点名。
		declared := api.Options{Format: "bmp", RowOrder: "up", ByteOrder: "be", RowAlign: 4}
		actual := api.Options{Format: "bmp", RowOrder: "down", ByteOrder: "le", RowAlign: 1}
		body, err := api.Encode(probe, actual)
		if err != nil {
			failf("故障注入（三标志）编码失败: %v", err)
		} else {
			log.printf("  三标志同时错：点名 [%s]", strings.Join(diffNames(api.Diagnose(body, declared, probe)), ","))
		}
		// 4b) 每次只错一个：应恰好点名那一个。
		for _, tc := range []struct {
			flag   string
			actual api.Options
		}{
			{"RowOrder", api.Options{Format: "bmp", RowOrder: "down", ByteOrder: "be", RowAlign: 4}},
			{"ByteOrder", api.Options{Format: "bmp", RowOrder: "up", ByteOrder: "le", RowAlign: 4}},
			{"RowAlign", api.Options{Format: "bmp", RowOrder: "up", ByteOrder: "be", RowAlign: 1}},
		} {
			body, err := api.Encode(probe, tc.actual)
			if err != nil {
				failf("故障注入 %s 编码失败: %v", tc.flag, err)
				continue
			}
			names := diffNames(api.Diagnose(body, declared, probe))
			if len(names) != 1 || names[0] != tc.flag {
				failf("故障注入 %s：应只点名它，实际 [%s]", tc.flag, strings.Join(names, ","))
				continue
			}
			log.printf("  仅 %s 错：点名 [%s]  OK", tc.flag, names[0])
		}
	}

	if err := os.WriteFile(path, []byte(log.b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "tables.txt"), []byte(log.tables.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if fail != 0 {
		fmt.Printf("FAIL: %d 项不一致，详见 %s\n", fail, path)
		os.Exit(1)
	}
	fmt.Printf("PASS: 24/24（样本）与 24/24（golden）结果逐像素一致；旧文件组单独通过；映射表见 %s\n",
		filepath.Join(*outDir, "tables.txt"))
}

// diagnoseText 在解不出期望图时，用 8 组合搜索指出是哪个标志没生效。
func diagnoseText(body []byte, declared api.Options, want *api.Image) string {
	d := api.Diagnose(body, declared, want)
	if len(d.Matched) == 0 {
		return "8 种组合都解不出期望图——数据与任何重排都不自洽"
	}
	var parts []string
	for _, f := range d.Diff {
		parts = append(parts, fmt.Sprintf("%s 声明 %s 实际 %s", f.Flag, f.Declared, f.Actual))
	}
	for _, f := range d.Ambiguous {
		parts = append(parts, fmt.Sprintf("%s 不可判定（声明 %s，%s 也可解）", f.Flag, f.Declared, f.Actual))
	}
	if len(parts) == 0 {
		return "可行组合含声明值，无法归因"
	}
	return "未生效标志: " + strings.Join(parts, "; ")
}

func diffNames(d api.Diagnosis) []string {
	var names []string
	for _, f := range d.Diff {
		names = append(names, f.Flag)
	}
	return names
}

func sameImage(a, b *api.Image) bool {
	return a.W == b.W && a.H == b.H && bytes.Equal(a.Pix, b.Pix)
}

func renderTable(d api.LayoutDescription, c combo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s %s  row=%s byte=%s align=%d\n", d.Format, c.name, d.RowOrder, d.ByteOrder, d.RowAlign)
	fmt.Fprintf(&b, "画幅 %dx%d  头 %d 字节 [%s]  行跨度 %d  偏移空间 %s\n",
		d.W, d.H, d.HeaderLen, d.HeaderHex, d.Stride, d.OffsetSpace)
	fmt.Fprintf(&b, "%-4s %-4s %-7s %-4s %-4s %s\n", "slot", "log", "offset", "len", "pad", "bytes")
	for _, r := range d.Rows {
		fmt.Fprintf(&b, "%-4d %-4d %-7d %-4d %-4d %s\n",
			r.StoredRow, r.LogicalRow, r.Offset, r.PixelLen, r.PadLen, rowHex(r.Bytes))
	}
	if inert := d.InertFlags(); len(inert) > 0 {
		fmt.Fprintf(&b, "未生效标志: %s\n", strings.Join(inert, ","))
	}
	for _, n := range d.Notes {
		fmt.Fprintf(&b, "  注: %s\n", n)
	}
	b.WriteString("\n")
	return b.String()
}

func rowHex(b []byte) string {
	const max = 24
	if len(b) <= max {
		return hex.EncodeToString(b)
	}
	return hex.EncodeToString(b[:max]) + fmt.Sprintf("…(%d)", len(b))
}

type logger struct {
	b, tables strings.Builder
}

func (l *logger) printf(format string, args ...any) {
	fmt.Fprintf(&l.b, format+"\n", args...)
}

// raw 写映射表（同时进报告与单独的 tables.txt）。
func (l *logger) raw(s string) {
	l.b.WriteString(s)
	l.tables.WriteString(s)
}
