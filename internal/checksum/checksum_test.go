package checksum

import "testing"

// 标准测试向量：CRC 模型对 "123456789" 的校验值是公开文献中的约定值。
func TestKnownVectors(t *testing.T) {
	if got := New(CRC32IEEE).Sum([]byte("123456789")); got != 0xCBF43926 {
		t.Fatalf("crc32-ieee 校验值错误: %08x", got)
	}
	if got := New(CRC16IBM).Sum([]byte("123456789")); got != 0xBB3D {
		t.Fatalf("crc16-ibm 校验值错误: %04x", got)
	}
}

func TestByNameRejectsUnknown(t *testing.T) {
	if _, err := ByName("crc99"); err == nil {
		t.Fatal("非法多项式名字应被拒绝")
	} else if got := err.Error(); !contains(got, "crc99") {
		t.Fatalf("错误必须给出非法名字本身: %s", got)
	}
	if _, err := ByName("crc32-ieee"); err != nil {
		t.Fatal(err)
	}
	if _, err := ByName("crc16-ibm"); err != nil {
		t.Fatal(err)
	}
}

// 任意单字节改动必须 100% 检出；32 位档在本长度下定位唯一且精确。
func TestLocateEverySingleByteFlip(t *testing.T) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i*31 + i>>3)
	}
	c := New(CRC32IEEE)
	stored := c.Sum(data)
	for i := range data {
		corrupt := make([]byte, len(data))
		copy(corrupt, data)
		corrupt[i] ^= 0xA5
		cands := c.LocateAll(corrupt, stored)
		if len(cands) != 1 {
			t.Fatalf("偏移 %d: 候选数 %d，应为 1", i, len(cands))
		}
		if cands[0].Offset != i {
			t.Fatalf("偏移 %d 的改动被定位到 %d", i, cands[0].Offset)
		}
		if cands[0].Delta != 0xA5 {
			t.Fatalf("偏移 %d: 差值 %02x，应为 a5", i, cands[0].Delta)
		}
	}
}

// 16 位档：单字节改动同样必检，真实偏移必在候选列表中；
// 位宽决定了症候空间只有 2^16，大文件上候选可能不唯一（生日界）。
func TestLocateEverySingleByteFlip16(t *testing.T) {
	data := make([]byte, 200)
	for i := range data {
		data[i] = byte(i*17 + 3)
	}
	c := New(CRC16IBM)
	stored := c.Sum(data)
	for i := range data {
		corrupt := make([]byte, len(data))
		copy(corrupt, data)
		corrupt[i] ^= 0xFF
		cands := c.LocateAll(corrupt, stored)
		found := false
		for _, cd := range cands {
			if cd.Offset == i && cd.Delta == 0xFF {
				found = true
			}
		}
		if !found {
			t.Fatalf("偏移 %d 的改动未定位到: 候选 %v", i, cands)
		}
	}
}

// 未改动时不得误报。
func TestNoFalsePositive(t *testing.T) {
	data := []byte("P5\n4 3\n255\n\x00\x07\x0e\x15\x1c\x23\x2a\x31\x38\x3f\x46\x4d")
	for _, def := range []Definition{CRC32IEEE, CRC16IBM} {
		c := New(def)
		if cands := c.LocateAll(data, c.Sum(data)); len(cands) != 0 {
			t.Fatalf("%s: 未改动却报 %v", def.Name, cands)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
