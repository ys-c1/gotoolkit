package api

import (
	"bytes"
	"testing"

	"gotoolkit/internal/codec"
)

func diagSample() *Image {
	return &Image{W: 5, H: 4, Pix: func() []uint8 {
		p := make([]uint8, 20)
		for i := range p {
			p[i] = uint8(1 + i*9)
		}
		return p
	}()}
}

// TestDiagnoseNamesIgnoredFlag 对每个标志各造一份“标志被实现忽略”的字节流，
// 再看 Diagnose 是否点名该标志。
func TestDiagnoseNamesIgnoredFlag(t *testing.T) {
	img := diagSample()
	good := codec.Layout{BottomUp: true, BigEndian: true, Align: 4}
	declared := Options{Format: "bmp", RowOrder: "up", ByteOrder: "be", RowAlign: 4}

	broken := map[string]codec.Layout{
		"RowOrder":  {BottomUp: false, BigEndian: true, Align: 4},
		"ByteOrder": {BottomUp: true, BigEndian: false, Align: 4},
		"RowAlign":  {BottomUp: true, BigEndian: true, Align: 1},
	}
	for flag, l := range broken {
		body, err := codec.EncodeBMPLayout(img.W, img.H, img.Pix, l)
		if err != nil {
			t.Fatal(err)
		}
		d := Diagnose(body, declared, img)
		if len(d.Diff) != 1 || d.Diff[0].Flag != flag {
			t.Fatalf("%s 未生效时应只点名它，实际: %+v (working=%+v)", flag, d.Diff, d.Working)
		}
	}

	// 对照：三个标志都真生效时不应报任何差异。
	body, _ := codec.EncodeBMPLayout(img.W, img.H, img.Pix, good)
	if d := Diagnose(body, declared, img); len(d.Diff) != 0 {
		t.Fatalf("全部生效时不应有差异，实际: %+v", d.Diff)
	}
}

func TestDiagnosePGMByteOrderIsUndetectable(t *testing.T) {
	// PGM 头是 ASCII，无论声明 le 还是 be，字节完全相同：
	// 该标志是“声明了但不生效”，Diagnose 不能凭空点名它。
	img := diagSample()
	body, _ := codec.EncodePGMLayout(img.W, img.H, img.Pix, codec.Layout{})
	d := Diagnose(body, Options{Format: "pgm", ByteOrder: "be"}, img)
	if len(d.Diff) != 0 {
		t.Fatalf("pgm 无法从字节区分字节序，不应点名 ByteOrder: %+v", d.Diff)
	}
	if len(d.Matched) != 2 {
		t.Fatalf("pgm 下 le/be 两种声明都应能解出，实际 %d 种", len(d.Matched))
	}
	// 两种都能解出，说明字节序不可观测：必须进 Ambiguous 而不是被当成“生效”或“失配”。
	if len(d.Ambiguous) != 1 || d.Ambiguous[0].Flag != "ByteOrder" {
		t.Fatalf("pgm 的 ByteOrder 应标为不可判定，实际: %+v", d.Ambiguous)
	}
}

func TestDiagnoseUnrecoverable(t *testing.T) {
	img := diagSample()
	body, _ := codec.EncodeBMPLayout(img.W, img.H, img.Pix, codec.Layout{BottomUp: true, Align: 4})
	body[54] ^= 0xff // 破坏首像素
	d := Diagnose(body, Options{Format: "bmp", RowOrder: "up", ByteOrder: "le", RowAlign: 4}, img)
	if len(d.Matched) != 0 {
		t.Fatalf("数据被破坏时不应有解，实际: %+v", d.Matched)
	}
}

// TestDiagnoseAcrossFormats 确认诊断在 pgm / rle 上同样可用。
func TestDiagnoseAcrossFormats(t *testing.T) {
	img := diagSample()
	cases := []struct {
		format   string
		declared Options
		broken   codec.Layout
		flag     string
	}{
		{"pgm", Options{Format: "pgm", RowOrder: "up", RowAlign: 4},
			codec.Layout{BottomUp: false, Align: 4}, "RowOrder"},
		{"pgm", Options{Format: "pgm", RowOrder: "down", RowAlign: 4},
			codec.Layout{BottomUp: false, Align: 1}, "RowAlign"},
		{"rle", Options{Format: "rle", RowOrder: "up", ByteOrder: "be", RowAlign: 4},
			codec.Layout{BottomUp: true, BigEndian: false, Align: 4}, "ByteOrder"},
		{"rle", Options{Format: "rle", RowOrder: "up", ByteOrder: "be", RowAlign: 4},
			codec.Layout{BottomUp: false, BigEndian: true, Align: 4}, "RowOrder"},
	}
	for _, c := range cases {
		var body []byte
		var err error
		switch c.format {
		case "pgm":
			body, err = codec.EncodePGMLayout(img.W, img.H, img.Pix, c.broken)
		default:
			body, err = codec.EncodeRLELayout(img.W, img.H, img.Pix, c.broken)
		}
		if err != nil {
			t.Fatal(err)
		}
		d := Diagnose(body, c.declared, img)
		if len(d.Diff) != 1 || d.Diff[0].Flag != c.flag {
			t.Fatalf("%s %s: 实际 diff=%+v working=%+v", c.format, c.flag, d.Diff, d.Working)
		}
	}
}

func TestDiagnoseImageIdentity(t *testing.T) {
	img := diagSample()
	body, _ := Encode(img, Options{Format: "rle", RowOrder: "up", ByteOrder: "be", RowAlign: 4})
	got, err := Decode(body, Options{Format: "rle", RowOrder: "up", ByteOrder: "be", RowAlign: 4})
	if err != nil || !bytes.Equal(got.Pix, img.Pix) {
		t.Fatal("样本自身往返应一致")
	}
}
