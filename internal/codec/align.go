package codec

// AlignUnit 是像素数据起始偏移（文件内）的对齐边界，单位字节。
//
// 取 4 而非 2 的理由：
//   - BMP 的行内填充单位本来就是 4（rowSize = (3w+3) &^ 3）。把像素数组
//     起点也对到 4，则每一行起点 off+k*rowSize 都是 4 的倍数，与行填充
//     自洽；取 2 只能保证首行偶对齐，行起点可能 ≡2 (mod 4)。
//   - 4 同时覆盖 16/32 位访问；2 只覆盖 16 位。
//   - 最多补 3 字节，代价可忽略。
//
// 注意：三种解码器目前都是逐字节访问，取 2 或 4 在本机 amd64 上没有可测
// 的性能差异（见报告）；取 4 的一致性收益来自 BMP 行对齐，而非当前解码
// 器的提速。
const AlignUnit = 4

// padFor 返回使 off 落在 AlignUnit 整数倍上的最小非负填充字节数。
//
// 同余方程：off + p ≡ 0 (mod A)，即 p ≡ -off (mod A)。
// 闭式解：  p = (A - off mod A) mod A，其中 0 ≤ off mod A < A。
// 取整规则：先求余数 r = off mod A；r == 0 时 p = 0，否则 p = A - r。
// 结果恒有 0 ≤ p < A，即最少填充。
func padFor(off int) int {
	return (AlignUnit - off%AlignUnit) % AlignUnit
}
