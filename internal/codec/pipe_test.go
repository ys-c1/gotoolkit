package codec

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func testStream() []uint8 {
	b := make([]uint8, 256)
	for i := range b {
		b[i] = uint8(i*3 + i/7)
	}
	return b
}

func TestPredictRoundTrip(t *testing.T) {
	data := testStream()
	for _, order := range []int{1, 2, 3} {
		res := PredictEncode(data, order)
		back := PredictDecode(res, order)
		if !bytes.Equal(back, data) {
			t.Fatalf("阶数 %d 预测往返不一致", order)
		}
	}
}

func TestSelectOrderPicksMinEntropy(t *testing.T) {
	// 线性斜坡：一阶残差全为常数 1（含 i=0，模 256 回绕处亦然），熵为 0，必须选中阶数 1
	ramp := make([]uint8, 512)
	for i := range ramp {
		ramp[i] = uint8(i) + 1
	}
	best, entropies := SelectOrder(ramp)
	if best != 1 {
		t.Fatalf("斜坡数据应选 1 阶, 实际 %d", best)
	}
	if entropies[0] != 0 {
		t.Fatalf("一阶残差熵应为 0, 实际 %v", entropies[0])
	}
	for i, order := range PredictOrderCandidates {
		if entropies[i] != ResidualEntropy(ramp, order) {
			t.Fatalf("阶数 %d 的熵未按档列出", order)
		}
	}
}

func TestSelectOrderTieBreaksLow(t *testing.T) {
	// 全零数据：三档熵都是 0，并列取低阶
	best, _ := SelectOrder(make([]uint8, 64))
	if best != 1 {
		t.Fatalf("熵并列应取低阶 1, 实际 %d", best)
	}
}

func TestMergeRunsRoundTrip(t *testing.T) {
	data := []uint8{7, 7, 7, 1, 2, 2, 9, 9, 9, 9, 9}
	merged := mergeRuns(data)
	back, err := splitRuns(merged)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, data) {
		t.Fatal("游程往返不一致")
	}
}

func TestSplitRunsRejectsCorrupt(t *testing.T) {
	if _, err := splitRuns([]uint8{3, 1, 2}); err == nil {
		t.Fatal("奇数长度应报错")
	}
	if _, err := splitRuns([]uint8{0, 9}); err == nil {
		t.Fatal("零计数值应报错")
	}
}

func TestPipeContainerLayout(t *testing.T) {
	body := testStream()
	out, err := EncodePipe(body, FormatBMP, PipeOptions{Predict: true, MergeRuns: true, Checksum: "crc32"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out[:4]) != PipeMagic {
		t.Fatal("magic 错误")
	}
	if len(out) <= PipeHeaderSize {
		t.Fatal("容器过短")
	}
	if out[4] != flagPredict|flagMergeRun|flagChecksum {
		t.Fatalf("flags 错误: %08b", out[4])
	}
	if out[5] < 1 || out[5] > 3 {
		t.Fatalf("阶数字节越界: %d", out[5])
	}
	if out[10] != FormatBMP {
		t.Fatalf("内层格式错误: %d", out[10])
	}
	// 校验和定位：偏移精确到字节
	if ChecksumOffset != 6 {
		t.Fatalf("校验和偏移应为 6, 实际 %d", ChecksumOffset)
	}
	stored := binary.LittleEndian.Uint32(out[ChecksumOffset : ChecksumOffset+4])
	if stored != crc32.ChecksumIEEE(out[PipeHeaderSize:]) {
		t.Fatal("头部校验和与 payload 不符")
	}
	back, fid, err := DecodePipe(out)
	if err != nil {
		t.Fatal(err)
	}
	if fid != FormatBMP || !bytes.Equal(back, body) {
		t.Fatal("容器往返不一致")
	}
}

func TestPipeChecksumDetectsTamper(t *testing.T) {
	body := testStream()
	out, err := EncodePipe(body, FormatPGM, PipeOptions{Predict: true, MergeRuns: true, Checksum: "crc32"})
	if err != nil {
		t.Fatal(err)
	}
	out[len(out)-1] ^= 0xff
	if _, _, err := DecodePipe(out); err != ErrChecksumMismatch {
		t.Fatalf("篡改后应报校验和不匹配, 实际 %v", err)
	}
}

func TestPipeRejectsBadParams(t *testing.T) {
	body := testStream()
	if _, err := EncodePipe(body, FormatPGM, PipeOptions{Predict: true, Order: -1}); err != ErrBadPredictOrder {
		t.Fatalf("负阶数应报 ErrBadPredictOrder, 实际 %v", err)
	}
	if _, err := EncodePipe(body, FormatPGM, PipeOptions{Predict: true, Order: 4}); err != ErrBadPredictOrder {
		t.Fatalf("阶数越界应报 ErrBadPredictOrder, 实际 %v", err)
	}
	if _, err := EncodePipe(body, FormatPGM, PipeOptions{Checksum: "md5"}); err != ErrBadChecksumParam {
		t.Fatalf("非法校验参数应报 ErrBadChecksumParam, 实际 %v", err)
	}
}
