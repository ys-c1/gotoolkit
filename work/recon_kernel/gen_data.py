# -*- coding: utf-8 -*-
"""
gen_data.py — 生成三档彩色测试图（合成数据，确定性随机种子，纯 numpy）。

设计依据：对 2:1 盒式下采样 + 5 抽头对称重建核，设计核相对双线性的可学习空间
来自信号的相关结构，且在两个空间维上都变化的斜向结构上最显著（轴对齐结构有一半
扫描线为常数，双线性在常数线上即最优，会把外抽头的作用平均稀释掉）。
三档按主导周期带宽递增构造，以多方向（含斜向）正弦为骨干：

  tier1 低频平滑：周期 48~96、宽方向角分布的缓变条纹 + 大尺度凸包
  tier2 中频边缘：周期 16~32 的定向条带 + 少量硬边矩形（典型彩色边缘）
  tier3 高频纹理：周期 6~12 的多向条纹（含 45° 斜向）+ 棋盘，接近奈奎斯特

噪声一律先高斯平滑再叠加（相关噪声）；不用白噪声——白噪声在 2:1 下采样后不含
可学习相位结构，只会把最小二乘解拉回双线性。

每档 10 张 256x256 RGB（uint8），前 6 张训练、后 4 张测试。
输出 data/<tier>/<id>.npy，shape=(256,256,3)。
"""
import os
import sys
import numpy as np

W = H = 256
N_PER_TIER = 10
SEED = 20261125

TIER_PARAMS = {
    # 窄频带主导：每档一条主正弦（周期在该档频带内小幅抖动）+ 很弱的次要结构，
    # 使五抽头外抽头面对的相关结构不被宽频混合平均掉。
    "tier1": {"periods": (48, 56, 64), "n_wave": 2, "n_blob": 3,
              "rects": 0, "checker": 0, "noise_sigma": 2.5, "noise_amp": 0.10},
    "tier2": {"periods": (16, 20, 24), "n_wave": 2, "n_blob": 1,
              "rects": 6, "checker": 0, "noise_sigma": 1.2, "noise_amp": 0.08},
    "tier3": {"periods": (7, 8, 9), "n_wave": 2, "n_blob": 0,
              "rects": 0, "checker": 1, "noise_sigma": 0.8, "noise_amp": 0.05},
}


def gauss_kernel1d(sigma, radius=None):
    if radius is None:
        radius = max(3, int(3 * sigma + 0.5))
    x = np.arange(-radius, radius + 1)
    k = np.exp(-0.5 * (x / sigma) ** 2)
    return k / k.sum()


def gauss_smooth(f, sigma):
    """可分离高斯平滑（边界反射），纯 numpy。"""
    k = gauss_kernel1d(sigma)
    r = len(k) // 2
    pad_h = np.pad(f, ((r, r), (0, 0)), mode="reflect")
    tmp = np.zeros_like(f)
    for i, w in enumerate(k):
        tmp += w * pad_h[i:i + f.shape[0], :]
    pad_w = np.pad(tmp, ((0, 0), (r, r)), mode="reflect")
    out = np.zeros_like(f)
    for i, w in enumerate(k):
        out += w * pad_w[:, i:i + f.shape[1]]
    return out


def make_field(rng, params):
    yy, xx = np.mgrid[0:H, 0:W].astype(np.float64)
    f = np.zeros((H, W))

    # 大尺度平滑凸包（仅低频档作底）
    for _ in range(params["n_blob"]):
        cx, cy = rng.uniform(0, W), rng.uniform(0, H)
        sx, sy = rng.uniform(40, 110), rng.uniform(40, 110)
        f += rng.uniform(-0.8, 0.8) * np.exp(-(((xx - cx) / sx) ** 2
                                               + ((yy - cy) / sy) ** 2))

    # 多方向正弦骨干：方向角覆盖含斜向的全象限，周期取自该档频带
    for _ in range(params["n_wave"]):
        ang = rng.uniform(0, np.pi)
        per = params["periods"][int(rng.integers(0, len(params["periods"])))]
        ph = rng.uniform(0, 2 * np.pi)
        amp = rng.uniform(0.7, 1.0)
        f += amp * np.sin(2 * np.pi * (xx * np.cos(ang) + yy * np.sin(ang))
                          / per + ph)

    # 少量硬边矩形（中频档彩色边缘）
    for _ in range(params["rects"]):
        x0 = int(rng.integers(0, W - 10))
        y0 = int(rng.integers(0, H - 10))
        rw = int(rng.integers(10, 60))
        rh = int(rng.integers(10, 60))
        f[y0:min(H, y0 + rh), x0:min(W, x0 + rw)] += rng.uniform(-1.2, 1.2)

    # 棋盘（高频档，含横竖两个方向的近奈奎斯特结构）
    if params["checker"]:
        block = int(rng.integers(3, 6))
        checker = (((xx // block).astype(int) + (yy // block).astype(int)) % 2) * 2 - 1
        f += 0.7 * checker

    # 相关噪声（先平滑再叠加）
    f += params["noise_amp"] * gauss_smooth(rng.standard_normal((H, W)),
                                            params["noise_sigma"])
    return f


def to_uint8_channel(f):
    lo, hi = f.min(), f.max()
    g = (f - lo) / (hi - lo + 1e-12)
    return np.clip(np.rint(g * 255.0), 0, 255).astype(np.uint8)


def main(out_dir):
    rng = np.random.default_rng(SEED)
    for tier in ("tier1", "tier2", "tier3"):
        d = os.path.join(out_dir, tier)
        os.makedirs(d, exist_ok=True)
        params = TIER_PARAMS[tier]
        for i in range(N_PER_TIER):
            img = np.stack([to_uint8_channel(make_field(rng, params))
                            for _ in range(3)], axis=-1)
            np.save(os.path.join(d, f"{i:02d}.npy"), img)
        print(f"{tier}: wrote {N_PER_TIER} images to {d}")


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else
         os.path.join(os.path.dirname(__file__), "data"))
