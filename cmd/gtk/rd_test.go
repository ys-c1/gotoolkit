package main

import (
	"bytes"
	"strings"
	"testing"

	"gotoolkit/api"
)

func TestErrorCodeMapping(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{api.ErrBadTarget, "ERR_BAD_TARGET"},
		{api.ErrZeroStep, "ERR_ZERO_STEP"},
		{api.ErrBadOption, "ERR_BAD_OPTION"},
		{api.ErrUnknownFormat, "ERR_UNKNOWN_FORMAT"},
	}
	for _, c := range cases {
		if got := errorCode(c.err); got != c.want {
			t.Fatalf("%v -> %s, 期望 %s", c.err, got, c.want)
		}
	}
}

func TestVerifyTwelveComplete(t *testing.T) {
	var rows []rawRecord
	for _, f := range rdFormats {
		for _, bpp := range targetBPPs {
			rows = append(rows, rawRecord{Format: f, TargetBPP: bpp})
		}
	}
	var buf bytes.Buffer
	if !verifyTwelve(rows, &buf) {
		t.Fatal("完整十二组应通过")
	}
	if !strings.Contains(buf.String(), "十二组对账全部通过") {
		t.Fatalf("缺少通过提示: %s", buf.String())
	}
}

func TestVerifyTwelveMissingGroup(t *testing.T) {
	// 缺 rle 4.0 一档。
	var rows []rawRecord
	for _, f := range rdFormats {
		for _, bpp := range targetBPPs {
			if f == "rle" && bpp == 4.0 {
				continue
			}
			rows = append(rows, rawRecord{Format: f, TargetBPP: bpp})
		}
	}
	var buf bytes.Buffer
	if verifyTwelve(rows, &buf) {
		t.Fatal("缺一组应不通过")
	}
}

func TestVerifyTwelveErrorRowFails(t *testing.T) {
	var rows []rawRecord
	for _, f := range rdFormats {
		for _, bpp := range targetBPPs {
			rows = append(rows, rawRecord{Format: f, TargetBPP: bpp})
		}
	}
	rows[0].ErrorCode = "ERR_ZERO_STEP"
	var buf bytes.Buffer
	if verifyTwelve(rows, &buf) {
		t.Fatal("常规档带错误码应不通过")
	}
}

func TestExtremeRowExcludedFromTwelve(t *testing.T) {
	var rows []rawRecord
	for _, f := range rdFormats {
		for _, bpp := range targetBPPs {
			rows = append(rows, rawRecord{Format: f, TargetBPP: bpp})
		}
		// 极端档即使带错误码也不影响十二组判定。
		rows = append(rows, rawRecord{Format: f, TargetBPP: extremeBPP, Extreme: true, ErrorCode: "ERR_ZERO_STEP"})
	}
	var buf bytes.Buffer
	if !verifyTwelve(rows, &buf) {
		t.Fatal("极端档错误不应拖垮十二组对账")
	}
}
