package api

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	allFormats = []string{"pgm", "bmp", "rle"}
	allPolys   = []string{"crc32-ieee", "crc16-ibm"}
)

// 三种格式 × 两档多项式：带校验往返后像素必须一致。
func TestChecksumRoundTrip(t *testing.T) {
	for _, f := range allFormats {
		for _, p := range allPolys {
			img := sample()
			body, err := Encode(img, Options{Format: f, Checksum: true, Polynomial: p})
			if err != nil {
				t.Fatalf("%s/%s 编码失败: %v", f, p, err)
			}
			got, err := Decode(body, Options{Format: f, Checksum: true, Polynomial: p})
			if err != nil {
				t.Fatalf("%s/%s 解码失败: %v", f, p, err)
			}
			if got.W != img.W || got.H != img.H || !bytes.Equal(got.Pix, img.Pix) {
				t.Fatalf("%s/%s 往返不一致", f, p)
			}
		}
	}
}

// 改动任意一字节（含头部与校验字段）必须被检出。
// 32 位档：报错偏移必须逐字节精确等于被改偏移。
// 16 位档：必须检出，且真实偏移必在候选列表中（位宽决定大文件上候选可能不唯一）。
func TestChecksumDetectsEveryByteFlip(t *testing.T) {
	for _, f := range allFormats {
		for _, p := range allPolys {
			body, err := Encode(sample(), Options{Format: f, Checksum: true, Polynomial: p})
			if err != nil {
				t.Fatal(err)
			}
			for i := range body {
				corrupt := make([]byte, len(body))
				copy(corrupt, body)
				corrupt[i] ^= 0xFF
				_, err := Decode(corrupt, Options{Format: f, Checksum: true, Polynomial: p})
				var ce *ChecksumError
				if !errors.As(err, &ce) {
					t.Fatalf("%s/%s 偏移 %d 的改动未检出: %v", f, p, i, err)
				}
				if p == "crc32-ieee" {
					if ce.Offset != i {
						t.Fatalf("%s/%s 偏移 %d 的改动被报成 %d", f, p, i, ce.Offset)
					}
					continue
				}
				found := false
				for _, off := range ce.Candidates {
					if off == i {
						found = true
					}
				}
				if !found {
					t.Fatalf("%s/%s 偏移 %d 不在候选 %v 中", f, p, i, ce.Candidates)
				}
			}
		}
	}
}

// 校验开关关闭时，三种格式输出必须与基准逐字节相同（即使给了多项式名字）。
func TestChecksumOffByteIdenticalToGolden(t *testing.T) {
	img := sample()
	for _, f := range allFormats {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f, Checksum: false, Polynomial: "crc16-ibm"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 关闭校验后与基准不逐字节一致", f)
		}
	}
}

// 非法多项式参数必须被拒绝，且错误中给出该名字。
func TestChecksumRejectsBadPolynomial(t *testing.T) {
	img := sample()
	for _, opt := range []Options{
		{Format: "pgm", Checksum: true, Polynomial: "crc99"},
		{Format: "pgm", Checksum: false, Polynomial: "md5"},
	} {
		if _, err := Encode(img, opt); err == nil {
			t.Fatalf("非法多项式 %q 应被拒绝", opt.Polynomial)
		} else if !errors.Is(err, ErrBadOption) || !strings.Contains(err.Error(), opt.Polynomial) {
			t.Fatalf("错误必须带非法名字 %q: %v", opt.Polynomial, err)
		}
		if _, err := Decode([]byte("P5\n1 1\n255\n\x00"), opt); err == nil {
			t.Fatalf("解码时非法多项式 %q 应被拒绝", opt.Polynomial)
		} else if !strings.Contains(err.Error(), opt.Polynomial) {
			t.Fatalf("错误必须带非法名字 %q: %v", opt.Polynomial, err)
		}
	}
}

// 多项式留空时默认 crc32-ieee。
func TestChecksumDefaultPolynomial(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "pgm", Checksum: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(body, Options{Format: "pgm", Checksum: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("#GTKC32 ")) {
		t.Fatalf("默认应为 32 位档: %q", body[:20])
	}
}

// 单字节改动检出率实测：两档都必须是 100%（突发长 8 ≤ 位宽，理论必检）。
func TestChecksumDetectionRateSingleByte(t *testing.T) {
	img := &Image{W: 32, H: 32, Pix: make([]uint8, 1024)}
	for i := range img.Pix {
		img.Pix[i] = byte(i*13 + i>>5)
	}
	for _, p := range allPolys {
		for _, f := range allFormats {
			body, err := Encode(img, Options{Format: f, Checksum: true, Polynomial: p})
			if err != nil {
				t.Fatal(err)
			}
			detected, total := 0, 0
			for i := range body {
				for _, delta := range []byte{0x01, 0x80, 0xFF} {
					corrupt := make([]byte, len(body))
					copy(corrupt, body)
					corrupt[i] ^= delta
					total++
					if _, err := Decode(corrupt, Options{Format: f, Checksum: true, Polynomial: p}); err != nil {
						detected++
					}
				}
			}
			if detected != total {
				t.Fatalf("%s/%s 单字节检出率 %d/%d，必须 100%%", p, f, detected, total)
			}
			t.Logf("%s/%s 单字节检出率 %d/%d = 100%%", p, f, detected, total)
		}
	}
}
