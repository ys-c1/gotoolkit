"""噪声空间相关性分析：区域分类、自相关、相关长度估计。

口径约定（全文统一，避免估计窗口不一致）：
- 基本单元为 8x8 块；三类区域按原图块活动度（AC 能量）的 1/3、2/3 分位划分，
  分类只做一次（在原图上），所有质量档沿用同一套块标签。
- 主口径（块内中心自相关，within-block centered ACF）：
  对每个 8x8 残差块去块均值后，按位移 lag 只统计完全落在块内的像素对，
  再对该类所有块求平均；水平、竖直两个方向分别计算。
  估计窗口固定为「块内 lag 0..7」，相关长度对包络 |r| 的指数拟合只用 lag 1..4
  （远端样本少、方差大）。
- 复核口径（全场 FFT 自相关）：对每类区域的残差掩膜做带掩膜的 FFT 自相关，
  仅用于检查块边界与跨块效应，不参与主表数字。
"""
from __future__ import annotations

import numpy as np

BLOCK = 8
MAX_LAG = BLOCK - 1
FIT_LAGS = (1, 2, 3, 4)  # 包络指数拟合窗口；lag0 恒为 1 不参与，lag>=5 样本过少
CLASS_NAMES = ("flat", "edge", "texture")
CLASS_CN = {"flat": "平坦区", "edge": "边缘区", "texture": "纹理区"}


def classify_blocks(activity: np.ndarray) -> np.ndarray:
    """按活动度三分位返回每块类别索引 0=flat 1=edge 2=texture。"""
    lo, hi = np.quantile(activity, [1 / 3, 2 / 3])
    labels = np.ones(len(activity), dtype=int)
    labels[activity <= lo] = 0
    labels[activity > hi] = 2
    return labels


def block_acf(resid_blocks: np.ndarray, max_lag: int = MAX_LAG) -> np.ndarray:
    """块内中心自相关，返回 (2, max_lag+1)，行 0=水平、行 1=竖直，lag0 归一为 1。

    对每块先去块均值（消去 DC 量化带来的常数偏置，只看空间结构），
    分母用该块零滞后能量；所有块等权平均后再用 lag=0 归一。
    """
    nb = resid_blocks.shape[0]
    x = resid_blocks - resid_blocks.mean(axis=(1, 2), keepdims=True)
    acc = np.zeros((2, max_lag + 1))
    for lag in range(max_lag + 1):
        if lag == 0:
            e = np.mean(x * x)
            acc[:, 0] = e
            continue
        h_pairs = x[:, :, :-lag] * x[:, :, lag:]
        v_pairs = x[:, :-lag, :] * x[:, lag:, :]
        acc[0, lag] = h_pairs.mean()
        acc[1, lag] = v_pairs.mean()
    return acc / acc[0, 0]


def masked_acf_2d(resid: np.ndarray, mask: np.ndarray, max_lag: int = MAX_LAG) -> np.ndarray:
    """复核口径：掩膜内残差的全场 FFT 自相关，返回 (2, max_lag+1)。

    用 Wiener–Khinchin：零填充到 2H x 2W 做循环相关，再用掩膜的像素对计数归一，
    消除非平稳/边界影响。残差同样先做全局去均值。
    """
    h, w = resid.shape
    r = np.where(mask, resid - resid[mask].mean(), 0.0)
    m = mask.astype(np.float64)
    fft_size = (2 * h, 2 * w)
    fr = np.fft.rfftn(r, fft_size)
    fm = np.fft.rfftn(m, fft_size)
    corr = np.fft.irfftn(fr * np.conj(fr), fft_size)
    norm = np.fft.irfftn(fm * np.conj(fm), fft_size)
    corr = corr / np.maximum(norm, 1e-12)

    out = np.zeros((2, max_lag + 1))
    c0 = corr[0, 0]
    for lag in range(max_lag + 1):
        out[0, lag] = corr[0, lag] / c0
        out[1, lag] = corr[1, lag] / c0
    return out


def correlation_length(acf: np.ndarray) -> dict:
    """由 ACF 曲线估计相关长度。

    边缘/纹理区的 ACF 常振荡（边缘 Gibbs、高频量化误差），直接对 r(d) 拟合指数
    会被负值破坏，故主口径对 **包络 |r(d)|** 估计（水平/竖直先平均取曲线，
    再取绝对值）。三个口径全部给出，估计窗口统一：

    估计窗口：块内 lag 0..7（与 8x8 块一致）；
    指数拟合只用 lag 1..4 四个点（lag>=5 的像素对只有 3 列/行，样本少、方差大）。

      L_env  : 主口径。对 lag1..4 的 log|r| 做最小二乘直线 log|r|=a-b*d，
               包络 e-fold 长度 L_env=1/b。
      L_efold: 包络首次降到 e^-1=0.3679 以下的线性插值距离（窗口内不出现则 NaN）。
      L_int  : 离散积分长度 Σ_{d=0..7}|r(d)|，对振荡不敏感的稳健辅助量。
      L_zero : r(d) 首次穿零的插值距离（振荡类会很小，仅作对照）。
    """
    r = acf.mean(axis=0)  # 水平、竖直平均
    env = np.abs(r)

    lags = np.arange(1, len(FIT_LAGS) + 1, dtype=np.float64)  # 1..4
    vals = env[1: 1 + len(lags)]
    pos = vals > 1e-9
    if pos.sum() >= 2:
        x, y = lags[pos], np.log(vals[pos])
        b = -(np.mean(x * y) - x.mean() * y.mean()) / (np.mean(x * x) - x.mean() ** 2)
        a = y.mean() + b * x.mean()
        l_env = 1.0 / b if b > 0 else np.nan
    else:
        b, a, l_env = np.nan, np.nan, np.nan

    l_efold = np.nan
    for d in range(1, len(env)):
        if env[d] <= np.exp(-1.0) < env[d - 1]:
            l_efold = (d - 1) + (env[d - 1] - np.exp(-1.0)) / (env[d - 1] - env[d])
            break

    l_int = float(np.sum(env))

    l_zero = np.nan
    for d in range(1, len(r)):
        if r[d] <= 0.0 < r[d - 1]:
            l_zero = (d - 1) + r[d - 1] / (r[d - 1] - r[d])
            break
    return {"L_env": l_env, "L_efold": l_efold, "L_int": l_int,
            "L_zero": l_zero, "curve": r, "slope": b, "intercept": a}


def block_rms_noise(resid_blocks: np.ndarray) -> float:
    """块残差的 RMS（含 DC 偏置，反映总噪声能量）。"""
    return float(np.sqrt(np.mean(resid_blocks ** 2)))


def effective_step(steps: np.ndarray) -> float:
    """区域有效量化步长 s_eff：块均方步长在该类块上的平均再开方（与噪声方差同量纲）。"""
    per_block = np.sqrt(np.mean(steps ** 2, axis=(1, 2)))
    return float(per_block.mean())


def spectral_centroid(qcoeffs: np.ndarray) -> float:
    """量化后非零系数的平均频率半径（衡量存活模式的空间频率，越低越平滑）。"""
    k, l = np.meshgrid(np.arange(BLOCK), np.arange(BLOCK), indexing="ij")
    radius = np.sqrt(k ** 2 + l ** 2)
    active = qcoeffs != 0
    weights = active.astype(np.float64)
    wsum = weights.sum()
    if wsum == 0:
        return np.nan
    return float((radius[None, :, :] * weights).sum() / wsum)


def ac_zero_fraction(qcoeffs: np.ndarray) -> float:
    """AC 系数落入死区（被量化为 0）的比例。"""
    ac = qcoeffs.reshape(-1, BLOCK * BLOCK)[:, 1:]
    return float(np.mean(ac == 0))


def class_block_mask(labels: np.ndarray, cls: int, nb_h: int, nb_w: int, h: int, w: int) -> np.ndarray:
    """把块标签展开成像素掩膜。"""
    lab = labels.reshape(nb_h, nb_w)
    block_mask = (lab == cls)
    mask = np.kron(block_mask, np.ones((BLOCK, BLOCK), dtype=bool))
    return mask[:h, :w]
