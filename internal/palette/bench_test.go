package palette

import "testing"

func benchInput(n int) []uint8 {
	pix := make([]uint8, n)
	x := uint32(2463534242)
	for i := range pix {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		pix[i] = uint8(x >> 24)
	}
	return pix
}

func BenchmarkQuantizeK8(b *testing.B) {
	pix := benchInput(640 * 480)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Quantize(pix, Options{Buckets: 8}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQuantizeK32(b *testing.B) {
	pix := benchInput(640 * 480)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Quantize(pix, Options{Buckets: 32}); err != nil {
			b.Fatal(err)
		}
	}
}
