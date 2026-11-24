package api

import (
	"bytes"
	"strconv"
)

// FlagDiff 描述声明组合与真正生效组合之间的差异。
type FlagDiff struct {
	Flag     string // RowOrder | ByteOrder | RowAlign
	Declared string
	Actual   string
}

// Diagnosis 是“某字节流解不出期望图”的定位结果。
// Working 是能在同格式下解出期望图的排布组合之一；
// Diff 列出被声明的标志里，哪些在所有可行组合里都对不上，即“没生效”的标志；
// Ambiguous 列出“声明值也行、换个值也行”的标志——本格式/本画幅下无法从字节区分，
// 不能判它没生效（例如 pgm 的 ByteOrder，或行宽已是 4 倍数时的 RowAlign）。
type Diagnosis struct {
	Working   Options
	Diff      []FlagDiff
	Ambiguous []FlagDiff
	Matched   [][]FlagDiff // 全部能解出期望图的组合，逐项列出与声明值的差异
}

// Diagnose 在同一格式的 8 种排布组合中寻找能解出 want 的组合，
// 从而指出声明组合里哪个标志没有生效。
// 若 8 种组合都解不出 want，Matched 为空，说明数据与任何重排都不自洽。
func Diagnose(body []byte, declared Options, want *Image) Diagnosis {
	base := declared
	base.RowOrder, base.ByteOrder, base.RowAlign = "", "", 0
	aligned := []Options{
		{RowOrder: "down", ByteOrder: "le", RowAlign: 1},
		{RowOrder: "down", ByteOrder: "le", RowAlign: 4},
		{RowOrder: "down", ByteOrder: "be", RowAlign: 1},
		{RowOrder: "down", ByteOrder: "be", RowAlign: 4},
		{RowOrder: "up", ByteOrder: "le", RowAlign: 1},
		{RowOrder: "up", ByteOrder: "le", RowAlign: 4},
		{RowOrder: "up", ByteOrder: "be", RowAlign: 1},
		{RowOrder: "up", ByteOrder: "be", RowAlign: 4},
	}
	var d Diagnosis
	var matched []Options
	for _, c := range aligned {
		opt := base
		opt.RowOrder, opt.ByteOrder, opt.RowAlign = c.RowOrder, c.ByteOrder, c.RowAlign
		img, err := Decode(body, opt)
		if err != nil || img.W != want.W || img.H != want.H || !bytes.Equal(img.Pix, want.Pix) {
			continue
		}
		matched = append(matched, opt)
		d.Matched = append(d.Matched, declared.diff(opt))
		if d.Working.Format == "" {
			d.Working = opt
		}
	}

	// 只在“所有能解出期望图的组合都与声明值不符”时才点名该标志。
	// 若存在某个组合与声明值一致，说明该标志与字节流相容（或在本格式下不可观测，
	// 例如 pgm 的 ByteOrder），不能判它没生效。
	d.Diff, d.Ambiguous = blameFlags(declared, matched)
	return d
}

// blameFlags 把每个标志分成三类：有可行组合与声明值一致（不点名）、
// 所有可行组合都与声明值不符（点名“没生效”）、或声明值本身被支持但也可以通过别的方式解释。
// 这里的实现把“有可行组合一致”一律视为不点名；Ambiguous 记录那些
// “可行组合里同时存在与声明值一致和不一致”的标志，提示无法从字节判定。
func blameFlags(declared Options, matched []Options) (blamed, ambiguous []FlagDiff) {
	if len(matched) == 0 {
		return nil, nil
	}
	checks := []struct {
		name   string
		equals func(Options) bool
		value  string
	}{
		{"RowOrder",
			func(o Options) bool {
				return normRowOrder(o.RowOrder, o.Format) == normRowOrder(declared.RowOrder, declared.Format)
			},
			normRowOrder(declared.RowOrder, declared.Format)},
		{"ByteOrder",
			func(o Options) bool { return normByteOrder(o.ByteOrder) == normByteOrder(declared.ByteOrder) },
			normByteOrder(declared.ByteOrder)},
		{"RowAlign",
			func(o Options) bool { return normAlign(o.RowAlign) == normAlign(declared.RowAlign) },
			itoa(normAlign(declared.RowAlign))},
	}
	for _, c := range checks {
		agree, disagree, actual := false, false, ""
		for _, m := range matched {
			if c.equals(m) {
				agree = true
			} else {
				disagree = true
				actual = flagValue(c.name, m)
			}
		}
		switch {
		case agree && disagree:
			ambiguous = append(ambiguous, FlagDiff{c.name, c.value, actual})
		case !agree:
			blamed = append(blamed, FlagDiff{c.name, c.value, actual})
		}
	}
	return blamed, ambiguous
}

func flagValue(flag string, o Options) string {
	switch flag {
	case "RowOrder":
		return normRowOrder(o.RowOrder, o.Format)
	case "ByteOrder":
		return normByteOrder(o.ByteOrder)
	default:
		return itoa(normAlign(o.RowAlign))
	}
}

// diff 只比较三个排布标志，忽略 Format / Gain。
func (o Options) diff(other Options) []FlagDiff {
	var out []FlagDiff
	ro, ro2 := normRowOrder(o.RowOrder, o.Format), normRowOrder(other.RowOrder, other.Format)
	if ro != ro2 {
		out = append(out, FlagDiff{"RowOrder", ro, ro2})
	}
	bo, bo2 := normByteOrder(o.ByteOrder), normByteOrder(other.ByteOrder)
	if bo != bo2 {
		out = append(out, FlagDiff{"ByteOrder", bo, bo2})
	}
	ra, ra2 := normAlign(o.RowAlign), normAlign(other.RowAlign)
	if ra != ra2 {
		out = append(out, FlagDiff{"RowAlign", itoa(ra), itoa(ra2)})
	}
	return out
}

func normRowOrder(v, format string) string {
	if v != "" {
		return v
	}
	if format == "bmp" {
		return "up"
	}
	return "down"
}

func normByteOrder(v string) string {
	if v == "" {
		return "le"
	}
	return v
}

func itoa(n int) string { return strconv.Itoa(n) }

func normAlign(v int) int {
	if v == 0 {
		return 1
	}
	return v
}
