package codec

import (
	"bytes"
	"testing"
)

func TestGTBKRoundTrip(t *testing.T) {
	w, h := 16, 16
	// 平滑图：块内残差在 [-127,127] 内，编码应无损往返。
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8((x + y*2) % 200)
		}
	}
	for _, block := range []int{2, 4, 8, 16} {
		body, err := EncodeGTBK(w, h, pix, block)
		if err != nil {
			t.Fatalf("block=%d 编码失败: %v", block, err)
		}
		if string(body[0:4]) != "GTBK" {
			t.Fatalf("block=%d 魔数错误", block)
		}
		if int(body[8]) != block {
			t.Fatalf("block=%d 头内块长错误: %d", block, body[8])
		}
		gw, gh, got, err := DecodeGTBK(body)
		if err != nil {
			t.Fatalf("block=%d 解码失败: %v", block, err)
		}
		if gw != w || gh != h || !bytes.Equal(got, pix) {
			t.Fatalf("block=%d 往返不一致", block)
		}
	}
}

func TestGTBKFlatBlock(t *testing.T) {
	// 整块同值时 k=1，每像素 1 位，残差全 0。
	pix := make([]uint8, 16)
	body, err := EncodeGTBK(4, 4, pix, 4)
	if err != nil {
		t.Fatal(err)
	}
	// 9 头 + 4 块头 + ceil(16/8)=2 残差字节
	if len(body) != 9+4+2 {
		t.Fatalf("平坦块长度错误: %d", len(body))
	}
	if _, _, got, err := DecodeGTBK(body); err != nil || !bytes.Equal(got, pix) {
		t.Fatalf("平坦块往返失败: %v", err)
	}
}

func TestGTBKRejectsBadBlock(t *testing.T) {
	pix := make([]uint8, 64)
	if _, err := EncodeGTBK(8, 8, pix, 3); err == nil {
		t.Fatal("非 2 的幂块长应报错")
	}
	if _, err := EncodeGTBK(8, 8, pix, 1); err == nil {
		t.Fatal("块长 1 应报错")
	}
	pix80 := make([]uint8, 80)
	if _, err := EncodeGTBK(10, 8, pix80, 4); err == nil {
		t.Fatal("宽不能整除应报错")
	}
	if _, _, _, err := DecodeGTBK([]byte("GTBK\x08\x00\x08\x00\x03")); err == nil {
		t.Fatal("非法块长流应报错")
	}
	if _, _, _, err := DecodeGTBK([]byte("XXXX\x08\x00\x08\x00\x02")); err == nil {
		t.Fatal("魔数错误应报错")
	}
}

func TestGTBKTruncated(t *testing.T) {
	pix := make([]uint8, 64)
	for i := range pix {
		pix[i] = uint8(i * 4)
	}
	body, err := EncodeGTBK(8, 8, pix, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := DecodeGTBK(body[:len(body)-2]); err == nil {
		t.Fatal("截断流应报错")
	}
	if _, _, _, err := DecodeGTBK(append(body, 0)); err == nil {
		t.Fatal("尾部冗余应报错")
	}
}
