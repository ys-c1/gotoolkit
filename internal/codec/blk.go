package codec

import (
	"encoding/binary"
	"errors"
)

// blk 是块直流（DC）压缩格式：
//
//	GTR2 | w u16 | h u16 | blockSize u8 | predict u8 | nx*ny 个量化直流值 u8
//
// 编码只做一件事：把图切成 blockSize×blockSize 的块，每块保留一个量化直流值。
// 解码有两条路径，由 predict 字节切换，二者拿到的直流网格完全相同：
//   - predict=0：块内填该块直流（平面块），相邻块直流不同就出现缝，缝宽随块长变大；
//   - predict=1：用相邻块直流预测块边界节点（系数之和为一），块内双线性插值，
//     相邻块共用同一个边界节点，因此缝为零。
const (
	blkMagic      = "GTR2"
	blkMaxBlock   = 64
	blkPredictOff = 0
	blkPredictOn  = 1
)

// 预测系数：边界节点取相邻两块直流的加权平均，权重相等。
// 系数之和的验证见 TestBLKPredictWeightsSum（blk_test.go）。
const (
	blkWeightA = 1 // 前一块直流的权重
	blkWeightB = 1 // 后一块直流的权重
	blkWeightN = 2 // 权重分母，故系数为 1/2、1/2
)

// blkDims 返回块网格尺寸，最右侧/最下侧的尾块允许不足整块。
func blkDims(w, h, bs int) (nx, ny int) {
	return (w + bs - 1) / bs, (h + bs - 1) / bs
}

// blockDC 计算一块像素的量化直流：块内均值四舍五入到最近整数（取整步骤①）。
func blockDC(pix []uint8, w, x0, y0, x1, y1 int) uint8 {
	sum, n := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			sum += int(pix[y*w+x])
			n++
		}
	}
	return uint8(divRound(sum, n))
}

// divRound 是整数四舍五入（非负输入）。
func divRound(a, b int) int { return (a + b/2) / b }

// EncodeBLK 做块直流压缩。predict 只写入开关字节，不改变直流网格本身。
func EncodeBLK(w, h, bs int, predict bool, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if bs <= 0 || bs > blkMaxBlock {
		return nil, errors.New("块长越界")
	}
	nx, ny := blkDims(w, h, bs)
	out := make([]byte, 0, 9+nx*ny)
	out = append(out, blkMagic...)
	out = binary.LittleEndian.AppendUint16(out, uint16(w))
	out = binary.LittleEndian.AppendUint16(out, uint16(h))
	out = append(out, uint8(bs))
	if predict {
		out = append(out, blkPredictOn)
	} else {
		out = append(out, blkPredictOff)
	}
	for by := 0; by < ny; by++ {
		for bx := 0; bx < nx; bx++ {
			x0, y0 := bx*bs, by*bs
			x1, y1 := min(x0+bs, w), min(y0+bs, h)
			out = append(out, blockDC(pix, w, x0, y0, x1, y1))
		}
	}
	return out, nil
}

// DecodeBLK 解析块直流流，按 predict 开关走平面块或预测插值两条路径。
func DecodeBLK(data []byte) (int, int, []uint8, error) {
	if len(data) < 9 || string(data[0:4]) != blkMagic {
		return 0, 0, nil, errors.New("不是 GTR2 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	bs := int(data[8])
	predict := data[9]
	if w <= 0 || h <= 0 || bs <= 0 || bs > blkMaxBlock {
		return 0, 0, nil, errors.New("BLK 头非法")
	}
	nx, ny := blkDims(w, h, bs)
	if len(data) < 10+nx*ny {
		return 0, 0, nil, errors.New("直流数据被截断")
	}
	dc := make([]uint8, nx*ny)
	copy(dc, data[10:10+nx*ny])

	pix := make([]uint8, w*h)
	if predict == blkPredictOn {
		reconstructPredicted(pix, w, h, bs, nx, ny, dc)
	} else {
		reconstructFlat(pix, w, h, bs, nx, ny, dc)
	}
	return w, h, pix, nil
}

// reconstructFlat 不预测路径：每块填自己的直流，块边界处直接出现台阶。
func reconstructFlat(pix []uint8, w, h, bs, nx, ny int, dc []uint8) {
	for by := 0; by < ny; by++ {
		for bx := 0; bx < nx; bx++ {
			v := dc[by*nx+bx]
			x0, y0 := bx*bs, by*bs
			x1, y1 := min(x0+bs, w), min(y0+bs, h)
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					pix[y*w+x] = v
				}
			}
		}
	}
}

// reconstructPredicted 预测路径。
//
// 节点网格 node 有 (nx+1)×(ny+1) 个角点：
//   - 图像外沿节点直接取相邻块直流（单个系数 1，和仍为一）；
//   - 内部节点取四块直流的等权平均（四个 1/4，和为一）；
//   - 一维看，内部边节点即相邻两块直流的 (1·dcL + 1·dcR)/2（两个 1/2，和为一）。
//
// 相邻块共用同一节点；块内做单步整数双线性插值，且边界像素严格落在节点上
// （权重分母为 bw-1、bh-1），所以边界两侧像素是同一个整数表达式，
// 不经过各自独立取整，缝严格为零。
func reconstructPredicted(pix []uint8, w, h, bs, nx, ny int, dc []uint8) {
	node := make([]uint8, (nx+1)*(ny+1))
	at := func(ix, iy int) uint8 { return node[iy*(nx+1)+ix] }
	for iy := 0; iy <= ny; iy++ {
		for ix := 0; ix <= nx; ix++ {
			// 围绕节点 (ix,iy) 的块行列：节点左侧块 ix-1（若有）、右侧块 ix（若有）。
			bxs := make([]int, 0, 2)
			if ix > 0 {
				bxs = append(bxs, ix-1)
			}
			if ix < nx {
				bxs = append(bxs, ix)
			}
			bys := make([]int, 0, 2)
			if iy > 0 {
				bys = append(bys, iy-1)
			}
			if iy < ny {
				bys = append(bys, iy)
			}
			sum, cnt := 0, 0
			for _, by := range bys {
				for _, bx := range bxs {
					sum += int(dc[by*nx+bx])
					cnt++
				}
			}
			// 取整步骤②：节点直流平均落到 uint8。cnt 为 1、2 或 4，权重均为 1/cnt，和为一。
			node[iy*(nx+1)+ix] = uint8(divRound(sum, cnt))
		}
	}

	for by := 0; by < ny; by++ {
		for bx := 0; bx < nx; bx++ {
			x0, y0 := bx*bs, by*bs
			x1, y1 := min(x0+bs, w), min(y0+bs, h)
			tl, tr := at(bx, by), at(bx+1, by)
			bl, br := at(bx, by+1), at(bx+1, by+1)
			// 跨度为像素间距数；块宽/高为 1 时该维塌缩：两端节点取同一个，跨度按 1 处理。
			dx, dy := x1-x0-1, y1-y0-1
			if dx == 0 {
				tr, br, dx = tl, bl, 1
			}
			if dy == 0 {
				bl, br, dy = tl, tr, 1
			}
			for y := y0; y < y1; y++ {
				fy := y - y0
				for x := x0; x < x1; x++ {
					fx := x - x0
					// 取整步骤③：单步双线性，分子一次累加、只四舍五入一次。
					num := int(tl)*(dx-fx)*(dy-fy) +
						int(tr)*fx*(dy-fy) +
						int(bl)*(dx-fx)*fy +
						int(br)*fx*fy
					pix[y*w+x] = uint8(divRound(num, dx*dy))
				}
			}
		}
	}
}
