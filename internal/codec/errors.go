package codec

import "fmt"

// 解码错误码按格式分段：
//
//	PGM 1xxx，BMP 2xxx，RLE 3xxx
//
// 每个码唯一对应一种损坏，调用方凭码即可定位格式与原因。
const (
	// PGM
	CodePGMShort    = 1001 // 流太短，容不下魔数
	CodePGMMagicP   = 1002 // 偏移 0 应为 'P'
	CodePGMMagic5   = 1003 // 偏移 1 应为 '5'
	CodePGMWidth    = 1004 // 宽度缺失、非数字或为 0
	CodePGMHeight   = 1005 // 高度缺失、非数字或为 0
	CodePGMMaxVal   = 1006 // maxval 应为 255
	CodePGMPixShort = 1007 // 像素数据不足 w*h

	// BMP
	CodeBMPShort    = 2001 // 流太短，容不下魔数或 54 字节头
	CodeBMPMagic    = 2002 // 魔数应为 "BM"
	CodeBMPDim      = 2003 // 宽或高非正
	CodeBMPBpp      = 2004 // 位深应为 24
	CodeBMPPixShort = 2005 // 像素数据不足 rowSize*h

	// RLE
	CodeRLEShort    = 3001 // 流太短，容不下魔数或 8 字节头
	CodeRLEMagic    = 3002 // 魔数应为 "GTR1"
	CodeRLEDim      = 3003 // 宽或高非正
	CodeRLEOverrun  = 3004 // 游程总长超出画幅
	CodeRLEPixShort = 3005 // 游程总长不足画幅
	CodeRLEOddTail  = 3006 // 尾部落单字节，计数-值未成对
)

// DecodeError 是三种格式统一的解码失败报告。
// Offset 是失败字节的偏移；截断时是第一个缺失字节的偏移（即流长）。
// Got 是 Offset 处的实际字节，-1 表示该处已是 EOF。
type DecodeError struct {
	Code   int    // 分段错误码，见上
	Format string // pgm | bmp | rle
	Offset int    // 失败字节偏移
	Want   string // 期望字节/内容
	Got    int    // 实际字节，-1 表示 EOF
	Detail string // 补充说明，可为空
}

func (e *DecodeError) Error() string {
	got := "EOF"
	if e.Got >= 0 {
		got = fmt.Sprintf("0x%02X", e.Got)
	}
	s := fmt.Sprintf("%s [E%d] 偏移 %d: 期望 %s, 实际 %s",
		e.Format, e.Code, e.Offset, e.Want, got)
	if e.Detail != "" {
		s += " (" + e.Detail + ")"
	}
	return s
}

// gotAt 返回 data[i]，越界时返回 -1（EOF）。
func gotAt(data []byte, i int) int {
	if i >= 0 && i < len(data) {
		return int(data[i])
	}
	return -1
}
