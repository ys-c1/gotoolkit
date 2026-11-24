package api

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func loadPGMX(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "pgmx", name))
	if err != nil {
		t.Fatalf("缺验收数据 %s: %v", name, err)
	}
	return b
}

// TestHeaderSwitchValidation 头开关只允许 pgm，其他格式必须报参数越界。
func TestHeaderSwitchValidation(t *testing.T) {
	img := sample()
	for _, f := range []string{"bmp", "rle"} {
		if _, err := Encode(img, Options{Format: f, ExtHeader: true}); !errors.Is(err, ErrBadOption) {
			t.Fatalf("%s + ExtHeader 应报 ErrBadOption，实际 %v", f, err)
		}
		if _, err := Encode(img, Options{Format: f, FlipRows: true}); !errors.Is(err, ErrBadOption) {
			t.Fatalf("%s + FlipRows 应报 ErrBadOption，实际 %v", f, err)
		}
	}
}

// TestHeaderSwitchRoundTrip 四种开合经 api 层往返，Meta 按头还原。
func TestHeaderSwitchRoundTrip(t *testing.T) {
	img := sample()
	for _, ext := range []bool{false, true} {
		for _, flip := range []bool{false, true} {
			opt := Options{Format: "pgm", ExtHeader: ext, FlipRows: flip}
			body, err := Encode(img, opt)
			if err != nil {
				t.Fatalf("编码失败 ext=%v flip=%v: %v", ext, flip, err)
			}
			got, err := Decode(body, Options{Format: "pgm"})
			if err != nil {
				t.Fatalf("解码失败 ext=%v flip=%v: %v", ext, flip, err)
			}
			if got.Meta == nil || got.Meta.ExtHeader != ext || got.Meta.FlipRows != flip {
				t.Fatalf("Meta 未按头还原 ext=%v flip=%v: %+v", ext, flip, got.Meta)
			}
			if !bytes.Equal(got.Pix, img.Pix) {
				t.Fatalf("像素往返错误 ext=%v flip=%v", ext, flip)
			}
		}
	}
}

// TestHeaderErrorCodes 三个字段各留一个冲突样例，错误码必须透传到 api 层。
func TestHeaderErrorCodes(t *testing.T) {
	cases := []struct {
		file string
		code error
	}{
		{"bad_bitdepth.bin", ErrFieldOutOfRange},
		{"bad_flags.bin", ErrFlagConflict},
		{"bad_checksum.bin", ErrChecksum},
		{"truncated_block.bin", ErrTruncated},
	}
	for _, tc := range cases {
		body := loadPGMX(t, tc.file)
		_, err := Decode(body, Options{Format: "pgm"})
		if !errors.Is(err, tc.code) {
			t.Fatalf("%s 期望错误码 %v，实际 %v", tc.file, tc.code, err)
		}
	}
}

// TestHeaderSwitchesOffByteIdentical 全关时与 golden 原字节基准逐字节相同。
func TestHeaderSwitchesOffByteIdentical(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("..", "testdata", "golden", "pgm.bin"))
	if err != nil {
		t.Fatalf("缺 golden: %v", err)
	}
	body, err := Encode(sample(), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, golden) {
		t.Fatal("全关时与 golden 不逐字节相同")
	}
}
