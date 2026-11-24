#!/usr/bin/env python3
"""先导实验：观察不同抖动配置下量化噪声的低频占比。

测试信号：256x256 缓变灰度图（对角线斜坡 + 平滑正弦），量化到 16 级（Δ=16）。
误差定义为行方向一维残差 e[n]，按行做 FFT 后平均功率谱。
低频占比 = 前 1/8 频带（k=0..N/16）能量 / 全频带能量（去掉直流 k=0）。
"""
import numpy as np

rng = np.random.default_rng(20261125)

H, W = 256, 256
yy, xx = np.mgrid[0:H, 0:W]
# 平滑缓变图：斜坡占主导 + 小幅度慢变正弦，量化后容易出现轮廓/低频相关
x = (xx + yy) * (255.0 / (W + H - 2))
x += 8.0 * np.sin(2 * np.pi * xx / 96.0) * np.sin(2 * np.pi * yy / 128.0)
x = np.clip(x, 0, 255)

DELTA = 16.0
LEVELS = 16


def quantize_round(v):
    return np.clip(np.round(v / DELTA) * DELTA, 0, 255)


def quantize_trunc(v):
    return np.clip(np.floor(v / DELTA) * DELTA, 0, 255)


def lf_fraction(e):
    # e: HxW 行方向残差
    E = np.fft.rfft(e, axis=1)
    P = (E * np.conj(E)).real.mean(axis=0)   # 行平均功率谱
    P[0] = 0.0                                # 去掉直流
    n = P.shape[0] - 1                        # 正频 bin 数（不含 DC）
    k_lf = max(1, n // 8)                     # 前 1/8 频带
    return P[1:k_lf + 1].sum() / P[1:].sum(), P


def mse(e):
    return float((e ** 2).mean())


print(f"{'config':28s} {'LF%':>8s} {'MSE':>10s} {'理论白噪LF%':>12s}")
print("-" * 62)

# 1. 无抖动，四舍五入
e = quantize_round(x) - x
lf, P = lf_fraction(e)
print(f"{'no dither, round':28s} {lf*100:8.2f} {mse(e):10.3f} {100/8:12.2f}")

# 2. 无抖动，截断（地板）——有直流偏置
e = quantize_trunc(x) - x
lf, P = lf_fraction(e)
print(f"{'no dither, truncate':28s} {lf*100:8.2f} {mse(e):10.3f}")

# 3. 满幅 RPDF 抖动（幅度 Δ，经典白噪条件），四舍五入
d = rng.uniform(-DELTA / 2, DELTA / 2, size=x.shape)
e = quantize_round(x + d) - x
lf, P = lf_fraction(e)
print(f"{'RPDF 1.0Δ dither, round':28s} {lf*100:8.2f} {mse(e):10.3f}")

# 4. 小幅 RPDF 抖动 0.25Δ —— 抖动不足，残留轮廓/相关
d = rng.uniform(-0.25 * DELTA / 2, 0.25 * DELTA / 2, size=x.shape)
e = quantize_round(x + d) - x
lf, P = lf_fraction(e)
print(f"{'RPDF 0.25Δ dither, round':28s} {lf*100:8.2f} {mse(e):10.3f}")

# 5. 满幅抖动 + 截断量化器（截断等价于固定偏置，但与满幅抖动组合后看谱形）
d = rng.uniform(0, DELTA, size=x.shape)
e = quantize_trunc(x + d) - x
lf, P = lf_fraction(e)
print(f"{'RPDF 1.0Δ dither, trunc':28s} {lf*100:8.2f} {mse(e):10.3f}")

# 6. 有序抖动（Bayer 4x4）——结构性周期图案，基频低
bayer = np.array([[0, 8, 2, 10],
                  [12, 4, 14, 6],
                  [3, 11, 1, 9],
                  [15, 7, 13, 5]], dtype=float)
thr = (bayer + 0.5) / 16.0 * DELTA - DELTA / 2
tile = np.tile(thr, (H // 4, W // 4))
e = quantize_round(x + tile) - x
lf, P = lf_fraction(e)
print(f"{'Bayer4x4 ordered dither':28s} {lf*100:8.2f} {mse(e):10.3f}")
