#!/usr/bin/env python3 -I
"""探索: Bayer 有序抖动图的频谱峰值与矩阵周期的关系。

只做诊断输出, 不生成最终交付物。
"""
import numpy as np


def bayer_matrix(k: int) -> np.ndarray:
    """生成阶数 k 的 Bayer 阈值矩阵, 尺寸 T=2^k, 值域 [0, T^2)。"""
    m = np.array([[0, 2], [3, 1]], dtype=np.int64)
    for _ in range(k - 1):
        m = np.block(
            [
                [4 * m + 0, 4 * m + 2],
                [4 * m + 3, 4 * m + 1],
            ]
        )
    return m


def dither(T: int, g: float, N: int = 512) -> np.ndarray:
    """对均匀灰度 g (0~1) 做周期 T 的 Bayer 有序抖动, 返回 0/1 图。"""
    k = int(round(np.log2(T)))
    b = bayer_matrix(k)
    thresholds = (b + 0.5) / (T * T)
    ii, jj = np.meshgrid(np.arange(N), np.arange(N), indexing="ij")
    tile = thresholds[ii % T, jj % T]
    return (g >= tile).astype(np.float64)


def peak_freq(img: np.ndarray, T: int):
    """返回 (基频1/T处能量, 主峰频率, 主峰阶数 n*)。频率单位 cy/px。"""
    N = img.shape[0]
    F = np.fft.fftshift(np.fft.fft2(img - img.mean()))
    mag = np.abs(F)
    fx = np.fft.fftshift(np.fft.fftfreq(N))
    fy = fx
    FX, FY = np.meshgrid(fx, fy, indexing="ij")
    fr = np.sqrt(FX**2 + FY**2)

    # 屏蔽 DC (最近的一个 bin)
    dc = (N // 2, N // 2)
    mag[dc] = 0.0

    # 主峰
    idx = np.unravel_index(np.argmax(mag), mag.shape)
    f_peak = float(fr[idx])
    return f_peak, f_peak * T, float(mag[idx])


def main() -> None:
    Ts = [4, 8, 16, 32, 64]
    grays = [0.125, 0.25, 0.5, 0.75]
    print(f"{'g':>6} {'T':>4} {'f_peak(cy/px)':>14} {'n*=f*T':>10} {'1/T':>10}")
    for g in grays:
        for T in Ts:
            img = dither(T, g)
            f_peak, n_star, amp = peak_freq(img, T)
            print(f"{g:>6.3f} {T:>4d} {f_peak:>14.5f} {n_star:>10.3f} {1.0/T:>10.5f}")
        print()

    # 另看 1/T 基频处是否确有谱线 (以 T=16, g=0.25 为例打印径向谱前几个 bin)
    T = 16
    img = dither(T, 0.25, N=1024)
    N = img.shape[0]
    F = np.fft.fftshift(np.fft.fft2(img - img.mean()))
    mag = np.abs(F)
    mag[N // 2, N // 2] = 0
    fx = np.fft.fftshift(np.fft.fftfreq(N))
    FX, FY = np.meshgrid(fx, fx, indexing="ij")
    fr = np.sqrt(FX**2 + FY**2)
    # 按径向 bin 汇总
    bins = np.arange(0, 0.55, 1.0 / N)
    sums, _ = np.histogram(fr, bins=bins, weights=mag)
    counts, _ = np.histogram(fr, bins=bins)
    radial = sums / np.maximum(counts, 1)
    order = np.argsort(radial)[::-1][:8]
    print("T=16, g=0.25 径向谱最强的 8 个 bin:")
    for o in order:
        f = 0.5 * (bins[o] + bins[o + 1])
        print(f"  f={f:.5f} cy/px (= {f*T:.2f}/T)  平均幅度={radial[o]:.1f}")


if __name__ == "__main__":
    main()
