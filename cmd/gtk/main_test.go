package main

import (
	"errors"
	"fmt"
	"testing"

	"gotoolkit/api"
)

// 错误码与字段必须一一对应，供脚本按退出码分流。
func TestExitCodes(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w: Gamma=255", api.ErrGammaZero), exitGammaZero},
		{fmt.Errorf("%w: Mask 长度=4", api.ErrMaskRange), exitMaskRange},
		{fmt.Errorf("%w: Gain=500", api.ErrBadOption), exitUsage},
		{errors.New("磁盘只读"), exitIO},
	}
	for i, c := range cases {
		if got := fail(c.err); got != c.want {
			t.Fatalf("case %d: 退出码 %d，期望 %d", i, got, c.want)
		}
	}
}
