package codec

import "testing"

func TestIDXRoundTrip(t *testing.T) {
	pal := []uint8{0, 85, 170, 255}
	idx := []uint8{0, 1, 2, 3, 3, 2} // 含重复项：索引不去重
	body, err := EncodeIDX(3, 2, pal, idx)
	if err != nil {
		t.Fatal(err)
	}
	if string(body[:4]) != "GTID" {
		t.Fatalf("magic 错误: %q", body[:4])
	}
	if body[8] != 4 {
		t.Fatalf("桶数字段错误: %d", body[8])
	}
	// 9 字节头 + 4 字节调色板 + 6 个索引。
	if len(body) != 9+4+6 {
		t.Fatalf("长度错误: %d", len(body))
	}
	w, h, pix, err := DecodeIDX(body)
	if err != nil {
		t.Fatal(err)
	}
	if w != 3 || h != 2 || len(pix) != 6 {
		t.Fatal("回读画幅错误")
	}
	want := []uint8{0, 85, 170, 255, 255, 170}
	for i := range want {
		if pix[i] != want[i] {
			t.Fatalf("回填像素 %d = %d, 期望 %d", i, pix[i], want[i])
		}
	}
}

func TestIDXRejects(t *testing.T) {
	if _, err := EncodeIDX(2, 1, []uint8{0, 1}, []uint8{0, 5}); err == nil {
		t.Fatal("索引超界应报错")
	}
	if _, err := EncodeIDX(2, 1, nil, []uint8{0}); err == nil {
		t.Fatal("空调色板应报错")
	}
	bad := []byte{'G', 'T', 'I', 'D', 2, 0, 1, 0, 2, 0, 255}
	if _, _, _, err := DecodeIDX(bad); err == nil {
		t.Fatal("截断数据应报错")
	}
}
