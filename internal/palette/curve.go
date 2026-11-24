package palette

import (
	"fmt"
	"math"
)

// UniformRampMSE predicts the variance-priority median-cut MSE for a
// uniformly sampled 256-level grayscale ramp at K requested buckets.
//
// The formula follows the actual split order: let K0 be the largest power of
// two not greater than K and r=K-K0. At depth log2(K0), buckets have width
// W=256/K0; the next r splits turn r width-W buckets into width-W/2 pairs.
// Error is therefore the count-weighted interpolation between E(W) and
// E(W/2). K is limited to [1,256].
func UniformRampMSE(k int) (float64, error) {
	if k < 1 || k > MaxBuckets {
		return 0, fmt.Errorf("%w: K=%d 必须在 [1,%d]", ErrBuckets, k, MaxBuckets)
	}
	k0 := 1
	for k0*2 <= k {
		k0 *= 2
	}
	w := float64(ValueMax+1) / float64(k0)
	wide := uniformBucketMSE(w)
	narrow := uniformBucketMSE(w / 2)
	r := float64(k - k0)
	return wide - r/float64(k0)*(wide-narrow), nil
}

// UniformRampBucketsForMSE returns the smallest K whose split-order curve
// reaches targetMSE on the uniform 256-level ramp. MSE=0 is reached at 256.
func UniformRampBucketsForMSE(targetMSE float64) (int, error) {
	if math.IsNaN(targetMSE) || math.IsInf(targetMSE, 0) || targetMSE < 0 {
		return 0, fmt.Errorf("%w: target MSE=%v 非法", ErrBuckets, targetMSE)
	}
	for k := 1; k <= MaxBuckets; k++ {
		v, err := UniformRampMSE(k)
		if err != nil {
			return 0, err
		}
		if v <= targetMSE+1e-12 {
			return k, nil
		}
	}
	return MaxBuckets, nil
}

// uniformBucketMSE is the MSE of one discrete uniform bucket of width w
// after mapping to its rounded mean. Even widths have a half-integer mean;
// width 1 is an exact singleton and has zero error.
func uniformBucketMSE(w float64) float64 {
	if w <= 1 {
		return 0
	}
	return w*w/12.0 + 1.0/6.0
}
