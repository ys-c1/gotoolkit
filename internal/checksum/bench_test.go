package checksum

import "testing"

func benchmarkSum(b *testing.B, def Definition) {
	data := make([]byte, 1<<20) // 1 MiB 文件体
	for i := range data {
		data[i] = byte(i*31 + i>>4)
	}
	c := New(def)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Sum(data)
	}
}

func BenchmarkSumCRC32IEEE(b *testing.B) { benchmarkSum(b, CRC32IEEE) }
func BenchmarkSumCRC16IBM(b *testing.B)  { benchmarkSum(b, CRC16IBM) }
