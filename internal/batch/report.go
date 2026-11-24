package batch

import (
	"fmt"
	"io"
	"strings"
)

// 机读输出协议（固定列，制表符分隔，每行以 \n 结尾）：
//
//	FAIL\t<文件名>\t<原因>          失败文件：文件名与原因分两列
//	SUMMARY\ttotal=<N>\tsucceeded=<N>\tfailed=<N>   汇总：固定四字段
//
// 汇总恒满足 total == succeeded+failed。字段内部的制表符/换行一律替换为
// 空格，保证任何一行的列数不被内容破坏，便于按 \t 切列机读解析。
const (
	failPrefix    = "FAIL"
	summaryPrefix = "SUMMARY"
)

// RenderReport 把报告以机读格式写到 w。
func RenderReport(w io.Writer, rep *Report) error {
	for _, r := range rep.Results {
		if r.OK {
			continue
		}
		line := failPrefix + "\t" + sanitize(r.Name) + "\t" + sanitize(r.Err.Error()) + "\n"
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%s\ttotal=%d\tsucceeded=%d\tfailed=%d\n",
		summaryPrefix, rep.Total, rep.Succeeded, rep.Failed)
	return err
}

// ParseSummary 从汇总行解析四个固定字段，供测试与外部工具校验。
// 返回 total, succeeded, failed；格式不符时返回错误。
func ParseSummary(line string) (total, succeeded, failed int, err error) {
	line = strings.TrimRight(line, "\n")
	cols := strings.Split(line, "\t")
	if len(cols) != 4 || cols[0] != summaryPrefix {
		return 0, 0, 0, fmt.Errorf("汇总行列数或前缀非法: %q", line)
	}
	for _, c := range cols[1:] {
		kv := strings.SplitN(c, "=", 2)
		if len(kv) != 2 {
			return 0, 0, 0, fmt.Errorf("汇总字段非法: %q", c)
		}
		var n int
		if _, err := fmt.Sscanf(kv[1], "%d", &n); err != nil {
			return 0, 0, 0, fmt.Errorf("汇总字段非整数: %q", c)
		}
		switch kv[0] {
		case "total":
			total = n
		case "succeeded":
			succeeded = n
		case "failed":
			failed = n
		default:
			return 0, 0, 0, fmt.Errorf("汇总字段名非法: %q", kv[0])
		}
	}
	return total, succeeded, failed, nil
}

func sanitize(s string) string {
	r := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	return r.Replace(s)
}
