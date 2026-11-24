// Package batch 实现目录级批量转码：目录遍历、逐文件转码、失败隔离。
// 单文件的编解码仍走 gotoolkit/api，本包只负责编排与 I/O。
package batch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gotoolkit/api"
)

// ErrDirEmpty 表示输入目录中没有任何可处理的候选文件。
var ErrDirEmpty = errors.New("输入目录为空")

// ErrPermission 表示遍历或读写时权限不足。
var ErrPermission = errors.New("权限不足")

// Mode 是批量处理方向。
type Mode int

const (
	// Encode：目录内 PGM 逐张编码为目标格式。
	Encode Mode = iota
	// Decode：目录内目标格式文件逐个解码回 PGM。
	Decode
)

// FileResult 是单个文件的处理结果。失败时 Err 非空。
type FileResult struct {
	Name string // 基名（不含目录），机读输出的第一列
	OK   bool
	Err  error
}

// Report 是一次批量处理的汇总，计数恒满足 Total == Succeeded+Failed。
type Report struct {
	Mode      Mode
	Format    string
	Results   []FileResult
	Total     int
	Succeeded int
	Failed    int
}

// 各格式对应的规范扩展名。
var extOf = map[string]string{
	"pgm": ".pgm",
	"bmp": ".bmp",
	"rle": ".rle",
}

// Run 遍历 inDir 中的候选文件并逐个转码到 outDir，返回完整报告。
// 任一文件失败只记录在报告里，不中断其余文件；只有目录本身无法遍历
// （权限不足、为空）时才返回错误，调用方据此区分错误码 3/4 与逐文件失败。
func Run(mode Mode, format string, gain int, inDir, outDir string) (*Report, error) {
	opt := api.Options{Format: format, Gain: gain}
	if err := opt.Validate(); err != nil {
		return nil, err
	}
	names, err := listInputs(mode, format, inDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, mapErr(err)
	}

	rep := &Report{Mode: mode, Format: format}
	for _, name := range names {
		rep.Results = append(rep.Results, processOne(mode, format, gain, inDir, outDir, name))
	}
	rep.Total = len(rep.Results)
	for _, r := range rep.Results {
		if r.OK {
			rep.Succeeded++
		} else {
			rep.Failed++
		}
	}
	return rep, nil
}

// listInputs 非递归地列出候选普通文件，按文件名排序保证输出确定。
func listInputs(mode Mode, format, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, mapErr(err)
	}
	want := inputExt(mode, format)
	var names []string
	for _, e := range entries {
		// 只处理普通文件，跳过子目录与符号链接等。
		if !e.Type().IsRegular() {
			continue
		}
		if filepath.Ext(e.Name()) != want {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: %s 中没有 %s 文件", ErrDirEmpty, dir, want)
	}
	sort.Strings(names)
	return names, nil
}

func inputExt(mode Mode, format string) string {
	if mode == Encode {
		return ".pgm"
	}
	return extOf[format]
}

// processOne 处理单个文件，任何错误都收敛为 FileResult，绝不向上抛。
func processOne(mode Mode, format string, gain int, inDir, outDir, name string) FileResult {
	res := FileResult{Name: name}
	inPath := filepath.Join(inDir, name)
	raw, err := os.ReadFile(inPath)
	if err != nil {
		res.Err = fmt.Errorf("读取失败: %w", mapErr(err))
		return res
	}

	var body []byte
	if mode == Encode {
		img, derr := api.Decode(raw, api.Options{Format: "pgm"})
		if derr != nil {
			res.Err = fmt.Errorf("解码失败: %w", derr)
			return res
		}
		body, err = api.Encode(img, api.Options{Format: format, Gain: gain})
		if err != nil {
			res.Err = fmt.Errorf("编码失败: %w", err)
			return res
		}
	} else {
		img, derr := api.Decode(raw, api.Options{Format: format, Gain: gain})
		if derr != nil {
			res.Err = fmt.Errorf("解码失败: %w", derr)
			return res
		}
		body, err = api.Encode(img, api.Options{Format: "pgm"})
		if err != nil {
			res.Err = fmt.Errorf("编码失败: %w", err)
			return res
		}
	}

	outName := strings.TrimSuffix(name, filepath.Ext(name)) + extOf[targetFormat(mode, format)]
	if err := os.WriteFile(filepath.Join(outDir, outName), body, 0o644); err != nil {
		res.Err = fmt.Errorf("写出失败: %w", mapErr(err))
		return res
	}
	res.OK = true
	return res
}

func targetFormat(mode Mode, format string) string {
	if mode == Encode {
		return format
	}
	return "pgm"
}

// mapErr 把底层 I/O 错误归类为权限错误，其余原样返回。
func mapErr(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%w: %v", ErrPermission, err)
	}
	return err
}
