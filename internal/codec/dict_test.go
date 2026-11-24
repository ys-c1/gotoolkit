package codec

import (
	"bytes"
	"testing"
)

func TestDictRoundTripRandom(t *testing.T) {
	cases := [][]byte{
		{0, 0, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8},
		make([]byte, 4096),
	}
	// 确定性伪随机
	var state uint32 = 0x9e3779b9
	rand := make([]byte, 4096)
	for i := range rand {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		rand[i] = byte(state)
	}
	cases = append(cases, rand)
	// 全 256 符号各出现多次
	wide := make([]byte, 4096)
	for i := range wide {
		wide[i] = byte(i)
	}
	cases = append(cases, wide)

	for ci, pix := range cases {
		w, h := len(pix), 1
		body, st, err := EncodeDict(w, h, pix)
		if err != nil {
			t.Fatalf("case %d encode: %v", ci, err)
		}
		dw, dh, got, err := DecodeDict(body)
		if err != nil {
			t.Fatalf("case %d decode: %v (stats %+v, len %d)", ci, err, st, len(body))
		}
		if dw != w || dh != h || !bytes.Equal(got, pix) {
			t.Fatalf("case %d mismatch", ci)
		}
	}
}

func TestCanonicalPrefix(t *testing.T) {
	// 经典用例：码长 1,2,3,3 -> Kraft = 1
	lengths := []int{1, 2, 3, 3}
	codes, bl := canonicalCodes(lengths)
	want := []uint32{0, 2, 6, 7}
	for i := range codes {
		if codes[i] != want[i] || bl[i] != lengths[i] {
			t.Fatalf("i=%d got code=%d len=%d want %d/%d", i, codes[i], bl[i], want[i], lengths[i])
		}
	}
}

func TestDictEdgeCases(t *testing.T) {
	cases := [][]byte{
		{42},                             // 1 像素：单符号码树
		make([]byte, 100),                // 常量图
		bytes.Repeat([]byte{0, 255}, 50), // 差分含大量 0xFF
	}
	// 256 个符号全部频繁出现，占满字典，ESC 靠降级获得槽位。
	wide := make([]byte, 512)
	for i := range wide {
		wide[i] = byte(i)
	}
	cases = append(cases, wide)

	for ci, pix := range cases {
		body, st, err := EncodeDict(len(pix), 1, pix)
		if err != nil {
			t.Fatalf("case %d encode: %v", ci, err)
		}
		if got := st.HeaderBytes + st.DictBytes + st.BitstreamBytes; got != st.TotalBytes || got != len(body) {
			t.Fatalf("case %d 对账失败", ci)
		}
		_, _, got, err := DecodeDict(body)
		if err != nil {
			t.Fatalf("case %d decode: %v", ci, err)
		}
		if !bytes.Equal(got, pix) {
			t.Fatalf("case %d 像素不一致", ci)
		}
	}
}

func TestDictRejectsBadInput(t *testing.T) {
	if _, _, _, err := DecodeDict([]byte("GTD1")); err == nil {
		t.Fatal("过短流应报错")
	}
	if _, _, err := EncodeDict(0, 1, []byte{1}); err == nil {
		t.Fatal("零宽应报错")
	}
	if _, _, err := EncodeDict(2, 1, []byte{1}); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
}
