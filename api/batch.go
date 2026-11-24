package api

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 批量入口的三个产物共用一套固定结构，便于内存版本与落盘版本逐项对照。
const (
	verdictPass = "通过"
)

var reportFields = []string{"文件名", "字节数", "校验结论"}

var (
	ErrBadDir       = errors.New("目录不合法")
	ErrReportWrite  = errors.New("报表写入失败")
	ErrReportVerify = errors.New("报表读回校验失败")
)

// BatchOptions 是批量转码的参数。Format/Gain 与单文件模式同义。
type BatchOptions struct {
	InDir  string // 输入目录，只处理其中的 *.pgm
	OutDir string // 输出目录，不存在时创建
	Format string // 输出格式：pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，默认 0
	Report string // 报表落盘路径
}

// BatchRow 是报表中的一行，字段顺序与 reportFields 一致。
type BatchRow struct {
	Name    string // 输入文件名
	Bytes   int    // 转码后输出文件的字节数
	Verdict string // 逐文件校验结论
}

// BatchFailure 记录被报表剔除的文件及原因。
type BatchFailure struct {
	Name string
	Err  error
}

// BatchResult 是批量入口的返回值。
type BatchResult struct {
	Rows   []BatchRow
	Failed []BatchFailure
	Report string
}

// RunBatch 是批量转码、逐文件校验、汇总报表的同一处入口。
// 每个文件转码后立刻回读校验；失败文件不进入报表。
// 报表落盘后会被读回并同时做结构校验与内存版本比对，全部通过才返回 nil。
func RunBatch(opt BatchOptions) (*BatchResult, error) {
	if opt.Format == "" {
		opt.Format = "pgm"
	}
	encOpt := Options{Format: opt.Format, Gain: opt.Gain}
	if err := encOpt.validate(); err != nil {
		return nil, err
	}
	if opt.InDir == "" || opt.OutDir == "" || opt.Report == "" {
		return nil, fmt.Errorf("%w: 输入目录、输出目录、报表路径都不能为空", ErrBadDir)
	}

	st, err := os.Stat(opt.InDir)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: 输入目录 %q", ErrBadDir, opt.InDir)
	}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("%w: 输出目录 %q: %v", ErrBadDir, opt.OutDir, err)
	}
	if st, err := os.Stat(opt.OutDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: 输出目录 %q", ErrBadDir, opt.OutDir)
	}

	entries, err := os.ReadDir(opt.InDir)
	if err != nil {
		return nil, fmt.Errorf("%w: 读取输入目录 %q: %v", ErrBadDir, opt.InDir, err)
	}

	res := &BatchResult{Rows: []BatchRow{}, Report: opt.Report}
	reportAbs, _ := filepath.Abs(opt.Report)
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pgm") {
			continue
		}
		inPath := filepath.Join(opt.InDir, e.Name())
		if abs, err := filepath.Abs(inPath); err == nil && abs == reportAbs {
			continue // 报表恰好落在输入目录且扩展名为 .pgm 时不要递归处理
		}
		row, err := transcodeAndVerify(inPath, opt.OutDir, e.Name(), encOpt)
		if err != nil {
			res.Failed = append(res.Failed, BatchFailure{Name: e.Name(), Err: err})
			continue
		}
		res.Rows = append(res.Rows, row)
	}

	if err := writeReport(opt.Report, res.Rows); err != nil {
		return res, fmt.Errorf("%w: %v", ErrReportWrite, err)
	}
	if err := verifyReportFile(opt.Report, res.Rows); err != nil {
		return res, fmt.Errorf("%w: %v", ErrReportVerify, err)
	}
	return res, nil
}

// transcodeAndVerify 完成单个文件的转码，并在写出后立刻回读比对。
func transcodeAndVerify(inPath, outDir, name string, opt Options) (BatchRow, error) {
	raw, err := os.ReadFile(inPath)
	if err != nil {
		return BatchRow{}, fmt.Errorf("读取失败: %v", err)
	}
	img, err := Decode(raw, Options{Format: "pgm"})
	if err != nil {
		return BatchRow{}, fmt.Errorf("转码解码失败: %v", err)
	}
	body, err := Encode(img, opt)
	if err != nil {
		return BatchRow{}, fmt.Errorf("转码编码失败: %v", err)
	}

	base := strings.TrimSuffix(name, filepath.Ext(name))
	outPath := filepath.Join(outDir, base+"."+opt.Format)
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		return BatchRow{}, fmt.Errorf("写出失败: %v", err)
	}

	// 转码后立刻从落盘文件回读校验，确认写出的字节与编码结果一致。
	saved, err := os.ReadFile(outPath)
	if err != nil {
		return BatchRow{}, fmt.Errorf("回读失败: %v", err)
	}
	if !bytes.Equal(saved, body) {
		return BatchRow{}, errors.New("回读校验不一致: 落盘字节与编码结果不同")
	}
	got, err := Decode(saved, Options{Format: opt.Format})
	if err != nil {
		return BatchRow{}, fmt.Errorf("回读解码失败: %v", err)
	}
	want := img
	if opt.Gain != 0 {
		want = applyGain(img, opt.Gain)
	}
	if got.W != want.W || got.H != want.H || !bytes.Equal(got.Pix, want.Pix) {
		return BatchRow{}, errors.New("回读校验不一致")
	}
	return BatchRow{Name: name, Bytes: len(body), Verdict: verdictPass}, nil
}

// writeReport 按固定字段顺序写出报表，任何字段都不省略。
func writeReport(path string, rows []BatchRow) error {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(reportFields); err != nil {
		return err
	}
	for _, r := range rows {
		if r.Name == "" || r.Verdict == "" {
			return fmt.Errorf("内存报表字段缺失: 文件名=%q 校验结论=%q", r.Name, r.Verdict)
		}
		rec := []string{r.Name, strconv.Itoa(r.Bytes), r.Verdict}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// ReportVerifyError 带出报表读回校验失败的行号与字段名。
type ReportVerifyError struct {
	Line  int
	Field string
	Msg   string
}

func (e *ReportVerifyError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("第 %d 行，字段「%s」：%s", e.Line, e.Field, e.Msg)
	}
	return fmt.Sprintf("字段「%s」：%s", e.Field, e.Msg)
}

func reportErr(line int, field, format string, args ...any) error {
	return &ReportVerifyError{Line: line, Field: field, Msg: fmt.Sprintf(format, args...)}
}

// verifyReportFile 把报表读回来，先做结构与字段校验，再与内存版本逐行逐字段比对。
func verifyReportFile(path string, want []BatchRow) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return reportErr(0, "文件名", "读取报表失败: %v", err)
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err == io.EOF {
		return reportErr(1, "文件名", "报表缺少表头")
	}
	if err != nil {
		return reportErr(1, "文件名", "表头解析失败: %v", err)
	}
	hLine, _ := r.FieldPos(0)
	for i, name := range reportFields {
		if i >= len(header) {
			return reportErr(hLine, name, "表头缺少该字段")
		}
		if header[i] != name {
			return reportErr(hLine, name, "表头字段名不符，读到 %q", header[i])
		}
	}
	if len(header) > len(reportFields) {
		return reportErr(hLine, reportFields[len(reportFields)-1], "表头字段过多（%d 个）", len(header))
	}

	got := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return reportErr(got+2, "文件名", "行解析失败: %v", err)
		}
		line, _ := r.FieldPos(0)
		for i, name := range reportFields {
			if i >= len(rec) {
				return reportErr(line, name, "该行缺少字段")
			}
		}
		if len(rec) > len(reportFields) {
			return reportErr(line, reportFields[len(reportFields)-1], "该行字段过多（%d 个）", len(rec))
		}
		if rec[0] == "" {
			return reportErr(line, "文件名", "字段为空")
		}
		if n, err := strconv.Atoi(rec[1]); err != nil || n < 0 {
			return reportErr(line, "字节数", "不是非负整数: %q", rec[1])
		}
		if rec[2] == "" {
			return reportErr(line, "校验结论", "字段为空")
		}

		if got >= len(want) {
			return reportErr(line, "文件名", "报表多出内存版本没有的行: %q", rec[0])
		}
		w := want[got]
		expect := []string{w.Name, strconv.Itoa(w.Bytes), w.Verdict}
		for i, name := range reportFields {
			if rec[i] != expect[i] {
				return reportErr(line, name, "与内存版本不一致：读回 %q，内存 %q", rec[i], expect[i])
			}
		}
		got++
	}
	if got != len(want) {
		return reportErr(got+2, "文件名", "报表比内存版本少 %d 行", len(want)-got)
	}
	return nil
}
