# -*- coding: utf-8 -*-
"""多分辨率重建（Burt-Adelson 高斯/拉普拉斯金字塔），可切换边界延拓方式。

约定（本实验全程固定，保证理论与代码逐项对应）：
- 低通核 w = (1/4, 1/2, 1/4)，半径 R = 1，和为 1。
- REDUCE: 1D 卷积 w 后下标隔 2 取 1，g[j] = 1/4 f[2j-1] + 1/2 f[2j] + 1/4 f[2j+1]。
- EXPAND: 偶数位 y[2j]=g[j]；奇数位 y[2j+1]=(g[j]+g[j+1])/2。
  即 BA1983 标准约定，对无限长常数/斜坡信号恒等。
- 2D: 先逐行再逐列可分离处理；长宽奇数时每层 floor 减半。
- mode="zero"  : 越界样本补 0（不做权重重归一化，制造暗边的正是这一点）。
- mode="sym"   : 半样本对称延拓 f[-1]=f[0], f[N]=f[N-1]（whole-point symmetry）。
"""

import numpy as np


def _ext(arr, pad, mode):
    """对最后一个轴做边界延拓。pad=(left,right)。"""
    widths = [(0, 0)] * (arr.ndim - 1) + [list(pad)]
    if mode == "zero":
        return np.pad(arr, pad_width=widths, mode="constant",
                      constant_values=0.0)
    if mode == "sym":
        # numpy reflect: 边界像素本身不重复，f[-1]=f[1]... 不是我们要的。
        # symmetric: f[-1]=f[0], f[N]=f[N-1]，即半样本（whole-point）对称。
        return np.pad(arr, pad_width=widths, mode="symmetric")
    raise ValueError(mode)


def reduce_1d(f, mode):
    """长度 n -> ceil? 这里用 floor 减半: m = n//2，下标 g[j] 用 f[2j-1..2j+1]。"""
    n = f.shape[-1]
    m = n // 2
    fp = _ext(f, (1, 1), mode)
    # fp[k] = f[k-1]，故 f[2j-1]=fp[2j], f[2j]=fp[2j+1], f[2j+1]=fp[2j+2]
    idx = 2 * np.arange(m)
    g = 0.25 * fp[..., idx] + 0.5 * fp[..., idx + 1] + 0.25 * fp[..., idx + 2]
    return g


def expand_1d(g, n_out, mode):
    """长度 m -> n_out，偶数位 g[j]，奇数位 (g[j]+g[j+1])/2，越界按 mode 处理。"""
    gp = _ext(g, (1, 1), mode)
    # gp[k] = g[k-1]
    y = np.empty(g.shape[:-1] + (n_out,), dtype=np.float64)
    j_even = np.arange(0, (n_out + 1) // 2)          # 偶数位 2j
    j_odd = np.arange(0, n_out // 2)                 # 奇数位 2j+1
    # y[2j]   = g[j]       = gp[j+1]
    y[..., 2 * j_even] = gp[..., j_even + 1]
    # y[2j+1] = (g[j]+g[j+1])/2 = (gp[j+1]+gp[j+2])/2
    y[..., 2 * j_odd + 1] = 0.5 * (gp[..., j_odd + 1] + gp[..., j_odd + 2])
    return y


def reduce_2d(img, mode):
    t = reduce_1d(img, mode)              # 水平（最后轴 = 宽）
    t = reduce_1d(t.T, mode).T            # 垂直
    return t


def expand_2d(g, out_shape, mode):
    h, w = out_shape
    t = expand_1d(g, w, mode)             # 水平
    t = expand_1d(t.T, h, mode).T         # 垂直
    return t


def gaussian_recon(img, levels, mode):
    """只走高斯通道：连续 REDUCE L 次再连续 EXPAND L 次回到原尺寸。

    暗边效应的载体（不保留拉普拉斯残差）。
    """
    g = img
    sizes = [img.shape]
    for _ in range(levels):
        g = reduce_2d(g, mode)
        sizes.append(g.shape)
    y = g
    for k in range(levels, 0, -1):
        y = expand_2d(y, sizes[k - 1], mode)
    return y


def laplacian_recon(img, levels, mode):
    """完整拉普拉斯金字塔重建（分析/合成同算子、同延拓），作为精确重建对照。"""
    g = img
    laps = []
    sizes = [img.shape]
    for _ in range(levels):
        gn = reduce_2d(g, mode)
        up = expand_2d(gn, g.shape, mode)
        laps.append(g - up)
        g = gn
        sizes.append(g.shape)
    y = g
    for k in range(levels - 1, -1, -1):
        y = expand_2d(y, sizes[k], mode) + laps[k]
    return y
