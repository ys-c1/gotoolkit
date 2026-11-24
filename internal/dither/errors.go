package dither

import "errors"

// ErrBadSize 表示输入画幅非法。
var ErrBadSize = errors.New("画幅或像素数不匹配")
