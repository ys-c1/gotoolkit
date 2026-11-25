#!/usr/bin/env python3 -I
"""探索 2: 扫描序阈值矩阵 (规则周期条带) 与 点阵矩阵的主频位置。"""
import numpy as np


def raster_matrix(T: int) -> np.ndarray:
    """行优先 (raster) 阈值矩阵, 值域 [0, T^2)。"""
    return np.arange(T * T, dtype=np.int64).reshape(T, T)


def dot_matrix(T: int) -> np.ndarray:
    """每个 T×T tile 仅 1 个亮点的阈值矩阵 (rank-0 在 (0,0))。

    其余位置给大阈值; 用于低密度灰阶的点阵实验。
    """
    m = np.full((T, T), T * T, dtype=np.int64)
    m[0, 0] = 0
    return m


def dither(mat: np.ndarray, T: int, g: float, N: int = 1024) -> np.ndarray:
    thresholds = (mat + 0.5) / (T * T)
    ii, jj = np.meshgrid(np.arange(N), np.arange(N), indexing="ij")
    tile = thresholds[ii % T, jj % T]
    return (g >= tile).astype(np.float64)


def radial_top_peaks(img: np.ndarray, top: int = 5):
    N = img.shape[0]
    F = np.fft.fftshift(np.fft.fft2(img - img.mean()))
    mag = np.abs(F)
    mag[N // 2, N // 2] = 0
    fx = np.fft.fftshift(np.fft.fftfreq(N))
    FX, FY = np.meshgrid(fx, fx, indexing="ij")
    fr = np.sqrt(FX**2 + FY**2)
    bins = np.arange(0, 0.55, 1.0 / N)
    sums, _ = np.histogram(fr, bins=bins, weights=mag)
    counts, _ = np.histogram(fr, bins=bins)
    radial = sums / np.maximum(counts, 1)
    order = np.argsort(radial)[::-1][:top]
    out = []
    for o in order:
        f = 0.5 * (bins[o] + bins[o + 1])
        out.append((f, radial[o]))
    return out


def main() -> None:
    Ts = [4, 8, 16, 32, 64]
    print("== raster 矩阵, g=0.5 (条带) ==")
    print(f"{'T':>4} {'1/T':>10}  径向谱前 5 峰 (cy/px, 幅度)")
    for T in Ts:
        img = dither(raster_matrix(T), T, 0.5)
        peaks = radial_top_peaks(img)
        pstr = ", ".join(f"{f:.5f}({a:.0f})" for f, a in peaks)
        print(f"{T:>4d} {1.0/T:>10.5f}  {pstr}")

    print()
    print("== raster 矩阵, g=0.25 ==")
    for T in Ts:
        img = dither(raster_matrix(T), T, 0.25)
        peaks = radial_top_peaks(img, 3)
        pstr = ", ".join(f"{f:.5f}({a:.0f})" for f, a in peaks)
        print(f"T={T:>3d} 1/T={1.0/T:.5f}  {pstr}")

    print()
    print("== 点阵矩阵, g 极小 (每 tile 1 点) ==")
    # g 选 1/T^2 与 2/T^2 之间, 保证每 tile 恰好 1 个白像素
    for T in [8, 16, 32]:
        g = 1.0 / (T * T)
        img = dither(dot_matrix(T), T, g)
        peaks = radial_top_peaks(img, 3)
        pstr = ", ".join(f"{f:.5f}({a:.0f})" for f, a in peaks)
        print(f"T={T:>3d} 1/T={1.0/T:.5f}  {pstr}")


if __name__ == "__main__":
    main()
