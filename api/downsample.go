package api

// downsampleAreaAvg 对灰度图做面积平均降采样，用于生成缩略图。
//
// 分块规则：把源图按 d×d 的互不重叠块切分，块 (ox,oy) 覆盖源行
// [oy*d, min(oy*d+d, H))、源列 [ox*d, min(ox*d+d, W))。输出画幅为
// (ceil(W/d), ceil(H/d))，即每个块（含边缘残缺块）都产出一个输出像素。
//
// 每个输出像素取源块内全部像素的面积平均，而不是取块内某个点：
//
//	v = floor((sum + n/2) / n)
//
// 其中 sum 是块内像素值之和（int64 累加防溢出），n 是块内实际像素数；
// n/2 实现四舍五入（0.5 向上）。
//
// 三项边界语义单独约定：
//
//  1. 边缘不足整块的像素：右边缘最后一列块、下边缘最后一行块可能不足
//     d×d。残缺块同样产出一个输出像素，除数 n 取块内实际像素数，不补
//     零、不复制边缘、也不丢弃。
//  2. 边缘块的权重分配：块内每个源像素权重相等，均为 1/n。残缺块不引入
//     虚拟填充格，参与平均的仍是实际存在的像素，它们之间依旧等权；
//     例如 d=2 时边缘只剩 2 个像素，则每个权重 1/2，而不是 1/4。
//  3. 除数不整除时余下像素的取法：余下的行/列单独组成最后一个残缺块，
//     输出边长向上取整，余数像素既不并进前一块，也不丢弃。
//
// d==1 时不做任何算术，原样复制一张图返回，保证输出与基准逐字节相同。
func downsampleAreaAvg(img *Image, d int) *Image {
	if d == 1 {
		pix := make([]uint8, len(img.Pix))
		copy(pix, img.Pix)
		return &Image{W: img.W, H: img.H, Pix: pix}
	}
	ow, oh := ceilDiv(img.W, d), ceilDiv(img.H, d)
	out := &Image{W: ow, H: oh, Pix: make([]uint8, ow*oh)}
	for oy := 0; oy < oh; oy++ {
		y0 := oy * d
		y1 := y0 + d
		if y1 > img.H {
			y1 = img.H
		}
		for ox := 0; ox < ow; ox++ {
			x0 := ox * d
			x1 := x0 + d
			if x1 > img.W {
				x1 = img.W
			}
			var sum int64
			for y := y0; y < y1; y++ {
				row := y * img.W
				for x := x0; x < x1; x++ {
					sum += int64(img.Pix[row+x])
				}
			}
			n := int64((y1 - y0) * (x1 - x0))
			out.Pix[oy*ow+ox] = uint8((sum + n/2) / n)
		}
	}
	return out
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }
