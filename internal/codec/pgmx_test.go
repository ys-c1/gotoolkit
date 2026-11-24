package codec

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// samplePix4x3 返回与 golden 同源的 4x3 像素（第 i 字节 = i*7）。
func samplePix4x3() []uint8 {
	pix := make([]uint8, 12)
	for i := range pix {
		pix[i] = uint8(i * 7)
	}
	return pix
}

// TestPGMXAcceptance 是头结构支路的十二组逐项验收。
// 任一组缺样例数据即失败（t.Fatal，不 skip）。
func TestPGMXAcceptance(t *testing.T) {
	basePix := samplePix4x3()

	load := func(name string) []byte {
		p := filepath.Join("..", "..", "testdata", "pgmx", name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("缺验收数据 %s: %v", p, err)
		}
		return b
	}
	// wantErr 为 nil 表示必须成功；否则 errors.Is 必须命中指定错误码。
	type want struct {
		meta PGMMeta
		pix  []uint8
		err  error
	}
	cases := []struct {
		name string
		file string
		want want
	}{
		{
			name: "01 两开关全关-按头还原",
			file: "valid_off_off.bin",
			want: want{meta: PGMMeta{ExtHeader: false, FlipRows: false}, pix: basePix},
		},
		{
			name: "02 仅扩展头开-位深与校验和还原",
			file: "valid_on_off.bin",
			want: want{meta: PGMMeta{ExtHeader: true, FlipRows: false}, pix: basePix},
		},
		{
			name: "03 仅行序开-行序标志还原",
			file: "valid_off_on.bin",
			want: want{meta: PGMMeta{ExtHeader: false, FlipRows: true}, pix: basePix},
		},
		{
			name: "04 两开关全开-三字段同时还原",
			file: "valid_on_on.bin",
			want: want{meta: PGMMeta{ExtHeader: true, FlipRows: true}, pix: basePix},
		},
		{
			name: "05 位深字段改坏-越界错误码并指出偏移",
			file: "bad_bitdepth.bin",
			want: want{err: ErrFieldOutOfRange},
		},
		{
			name: "06 行序标志改坏-冲突错误码并指出偏移",
			file: "bad_flags.bin",
			want: want{err: ErrFlagConflict},
		},
		{
			name: "07 校验和字段改坏-校验错误码并指出偏移",
			file: "bad_checksum.bin",
			want: want{err: ErrChecksum},
		},
		{
			name: "08 扩展块截断-截断错误码",
			file: "truncated_block.bin",
			want: want{err: ErrTruncated},
		},
		{
			name: "09 编码四组合-与样例逐字节相同(off off)",
			file: "valid_off_off.bin",
			want: want{meta: PGMMeta{}, pix: basePix},
		},
		{
			name: "10 编码四组合-与样例逐字节相同(on off)",
			file: "valid_on_off.bin",
			want: want{meta: PGMMeta{ExtHeader: true}, pix: basePix},
		},
		{
			name: "11 编码四组合-与样例逐字节相同(off on)",
			file: "valid_off_on.bin",
			want: want{meta: PGMMeta{FlipRows: true}, pix: basePix},
		},
		{
			name: "12 编码四组合-与样例逐字节相同(on on)",
			file: "valid_on_on.bin",
			want: want{meta: PGMMeta{ExtHeader: true, FlipRows: true}, pix: basePix},
		},
	}

	for idx, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := load(tc.file)
			if idx < 8 {
				// 01-08：解码端按头还原 / 检出并指出位置。
				_, _, gotPix, gotMeta, err := DecodePGMEx(fixture)
				if tc.want.err != nil {
					if !errors.Is(err, tc.want.err) {
						t.Fatalf("期望错误码 %v，实际 %v", tc.want.err, err)
					}
					if err == nil || !bytes.Contains([]byte(err.Error()), []byte("偏移")) {
						t.Fatalf("错误信息必须指出字段偏移位置: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("解码失败: %v", err)
				}
				if gotMeta != tc.want.meta {
					t.Fatalf("头状态还原错误: got=%+v want=%+v", gotMeta, tc.want.meta)
				}
				if !bytes.Equal(gotPix, tc.want.pix) {
					t.Fatalf("像素还原错误（行序未按头还原）")
				}
				return
			}
			// 09-12：编码端按开关产出，必须与样例逐字节相同，
			// 其中全关组合即与原字节基准一致。
			got, err := EncodePGMEx(4, 3, basePix, PGMOptions{
				ExtHeader: tc.want.meta.ExtHeader,
				FlipRows:  tc.want.meta.FlipRows,
			})
			if err != nil {
				t.Fatalf("编码失败: %v", err)
			}
			if !bytes.Equal(got, fixture) {
				t.Fatalf("编码字节与样例不一致\ngot : %v\nwant: %v", got, fixture)
			}
		})
	}
}

// TestPGMXFieldsDoNotOverlap 校验固定偏移表三字段互不覆盖。
func TestPGMXFieldsDoNotOverlap(t *testing.T) {
	fields := []struct {
		name       string
		off, ln    int
	}{
		{"魔数", offMagic, magicLen},
		{"位深", offBitDepth, bitDepthLen},
		{"标志", offFlags, flagsLen},
		{"校验和", offChecksum, checksumLen},
	}
	for i := 0; i < len(fields); i++ {
		for j := i + 1; j < len(fields); j++ {
			a, b := fields[i], fields[j]
			if a.off < b.off+b.ln && b.off < a.off+a.ln {
				t.Fatalf("字段 %s 与 %s 偏移区间重叠", a.name, b.name)
			}
		}
		if end := fields[i].off + fields[i].ln; end > extSize {
			t.Fatalf("字段 %s 越出扩展块", fields[i].name)
		}
	}
}

// TestPGMXGoldenEquivalence 全关时头结构与原字节基准逐字节相同。
func TestPGMXGoldenEquivalence(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "pgm.bin"))
	if err != nil {
		t.Fatalf("缺 golden: %v", err)
	}
	// 用与 golden 完全相同的输入（4x3，像素 i*7）编码，两开关全关。
	basePix := samplePix4x3()
	body, err := EncodePGMEx(4, 3, basePix, PGMOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, golden) {
		t.Fatalf("全关时与 golden 不逐字节相同\ngot : %v\nwant: %v", body, golden)
	}
}

// TestPGMXRoundTripAllCombos 两开关四种开合全部回读一致。
func TestPGMXRoundTripAllCombos(t *testing.T) {
	basePix := samplePix4x3()
	for _, ext := range []bool{false, true} {
		for _, flip := range []bool{false, true} {
			opt := PGMOptions{ExtHeader: ext, FlipRows: flip}
			body, err := EncodePGMEx(4, 3, basePix, opt)
			if err != nil {
				t.Fatalf("编码失败 ext=%v flip=%v: %v", ext, flip, err)
			}
			w, h, pix, meta, err := DecodePGMEx(body)
			if err != nil {
				t.Fatalf("解码失败 ext=%v flip=%v: %v", ext, flip, err)
			}
			if w != 4 || h != 3 || !bytes.Equal(pix, basePix) {
				t.Fatalf("回读像素错误 ext=%v flip=%v", ext, flip)
			}
			if meta.ExtHeader != ext || meta.FlipRows != flip {
				t.Fatalf("回读头状态错误 ext=%v flip=%v: got=%+v", ext, flip, meta)
			}
		}
	}
	// 手工验证行翻转：翻转后首行应等于原图末行。
	flippedPix := reverseRows(4, 3, basePix)
	if !bytes.Equal(flippedPix[:4], basePix[8:12]) {
		t.Fatal("行翻转辅助函数错误：首行不等于原图末行")
	}
}
