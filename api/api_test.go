package api

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotoolkit/internal/codec"
)

func sample() *Image {
	img := &Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	return img
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s 尺寸不一致", f)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 像素不一致", f)
		}
	}
}

func TestGoldenBytes(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 与原编码不逐字节一致", f)
		}
	}
}

func TestRejects(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "tga"}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
}

// pipeStages 是压缩链路的四个累积档位：游程在预测之后，校验和最后。
var pipeStages = []struct {
	name string
	opt  func(o *Options)
}{
	{"none", func(o *Options) {}},
	{"predict", func(o *Options) { o.Predict = true }},
	{"predict+rle", func(o *Options) { o.Predict = true; o.MergeRuns = true }},
	{"predict+rle+checksum", func(o *Options) {
		o.Predict = true
		o.MergeRuns = true
		o.Checksum = "crc32"
	}},
}

// TestPipelineTwelveCombos 跑遍 3 格式 × 4 链路档位 = 12 个组合，少一个都不行。
// 全关档必须与基准文件逐字节一致；其余档必须能无损往返。
func TestPipelineTwelveCombos(t *testing.T) {
	formats := []string{"pgm", "bmp", "rle"}
	ran := 0
	for _, f := range formats {
		for _, st := range pipeStages {
			ran++
			name := f + "/" + st.name
			img := sample()
			opt := Options{Format: f}
			st.opt(&opt)
			body, err := Encode(img, opt)
			if err != nil {
				t.Fatalf("%s 编码失败: %v", name, err)
			}
			if st.name == "none" {
				golden, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
				if err != nil {
					t.Skipf("缺 golden %s", f)
				}
				if !bytes.Equal(body, golden) {
					t.Fatalf("%s 三项全关时字节流与基准不一致", name)
				}
			} else if !codec.IsPipe(body) {
				t.Fatalf("%s 启用链路后应带 GTC1 头", name)
			}
			got, err := Decode(body, Options{Format: f})
			if err != nil {
				t.Fatalf("%s 解码失败: %v", name, err)
			}
			if got.W != img.W || got.H != img.H || !bytes.Equal(got.Pix, img.Pix) {
				t.Fatalf("%s 往返不一致", name)
			}
		}
	}
	if ran != 12 {
		t.Fatalf("应跑 12 个组合, 实际 %d", ran)
	}
}

func TestPipelineRejects(t *testing.T) {
	img := sample()
	// 阶数为负：独立错误码
	if _, err := Encode(img, Options{Format: "pgm", Predict: true, PredictOrder: -1}); !errors.Is(err, ErrBadPredictOrder) {
		t.Fatalf("负阶数应报 ErrBadPredictOrder, 实际 %v", err)
	}
	// 阶数超出候选集同码
	if _, err := Encode(img, Options{Format: "pgm", Predict: true, PredictOrder: 4}); !errors.Is(err, ErrBadPredictOrder) {
		t.Fatalf("阶数 4 应报 ErrBadPredictOrder, 实际 %v", err)
	}
	// 校验参数非法：独立错误码
	if _, err := Encode(img, Options{Format: "pgm", Checksum: "md5"}); !errors.Is(err, ErrBadChecksum) {
		t.Fatalf("非法校验参数应报 ErrBadChecksum, 实际 %v", err)
	}
	// 篡改 payload 一字节：校验和必须能查出来
	body, err := Encode(img, Options{Format: "pgm", Predict: true, MergeRuns: true, Checksum: "crc32"})
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 0xff
	if _, err := Decode(body, Options{Format: "pgm"}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("篡改后应报 ErrChecksumMismatch, 实际 %v", err)
	}
}

// TestChecksumOffsetExact 校验和写进文件头，偏移精确到字节。
func TestChecksumOffsetExact(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "rle", Checksum: "crc32"})
	if err != nil {
		t.Fatal(err)
	}
	if codec.ChecksumOffset != 6 {
		t.Fatalf("校验和偏移应为 6, 实际 %d", codec.ChecksumOffset)
	}
	stored := binary.LittleEndian.Uint32(body[codec.ChecksumOffset : codec.ChecksumOffset+4])
	if stored != crc32.ChecksumIEEE(body[codec.PipeHeaderSize:]) {
		t.Fatal("头部 6..9 字节不是 payload 的 CRC32")
	}
}

// reportImage 比 sample 大，残差分布更有观察价值。
func reportImage() *Image {
	img := &Image{W: 64, H: 64, Pix: make([]uint8, 64*64)}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Pix[y*64+x] = uint8((x*2 + y*3 + x*x/16) % 256)
		}
	}
	return img
}

// TestResidualReport 把每档残差熵、选阶依据和残差分布画成表，
// 同时写入 ../artifacts/pipeline_report.md。
func TestResidualReport(t *testing.T) {
	img := reportImage()
	var sb strings.Builder
	sb.WriteString("# 压缩链路残差报告\n\n")
	sb.WriteString("预测阶数候选集：{1, 2, 3}；残差熵单位：比特/字节。\n\n")
	sb.WriteString("选阶依据：残差熵越小说明残差分布越集中、后续游程合并的可压缩空间越大，故在候选集上逐档计算残差熵并取最小者（并列取低阶）。\n")
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		best, entropies := codec.SelectOrder(body)
		res := codec.PredictEncode(body, best)

		sb.WriteString(fmt.Sprintf("\n## 格式 %s（体 %d 字节）\n\n", f, len(body)))
		sb.WriteString("各档残差熵：\n\n| 阶数 | 残差熵 (bit/byte) | 选取 |\n|---:|---:|:--|\n")
		for i, order := range codec.PredictOrderCandidates {
			mark := ""
			if order == best {
				mark = "✔ 熵最小"
			}
			sb.WriteString(fmt.Sprintf("| %d | %.4f | %s |\n", order, entropies[i], mark))
		}
		sb.WriteString(fmt.Sprintf("\n选定阶数：%d（三档中残差熵最小）。\n", best))

		var hist [256]int
		for _, v := range res {
			hist[v]++
		}
		sb.WriteString(fmt.Sprintf("\n预测后（阶数 %d）残差分布（16 桶）：\n\n| 残差区间 | 计数 | 占比 | 分布 |\n|---|---:|---:|---|\n", best))
		for b := 0; b < 16; b++ {
			cnt := 0
			for v := b * 16; v < b*16+16; v++ {
				cnt += hist[v]
			}
			frac := float64(cnt) / float64(len(res))
			bar := strings.Repeat("█", int(frac*40+0.5))
			sb.WriteString(fmt.Sprintf("| %3d–%3d | %d | %.2f%% | %s |\n", b*16, b*16+15, cnt, frac*100, bar))
		}
	}
	report := sb.String()
	if err := os.MkdirAll("../artifacts", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("../artifacts/pipeline_report.md", []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + report)
}
