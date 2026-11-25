# -*- coding: utf-8 -*-
"""1D 理论命题的数值核验：暗边宽度等式、对称延拓误差界、精确重建对照。"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import numpy as np
from pyramid import reduce_1d, expand_1d, _ext


def gauss_recon_1d(f, levels, mode):
    g = f
    sizes = [len(f)]
    for _ in range(levels):
        g = reduce_1d(g[None, :], mode)[0]
        sizes.append(len(g))
    y = g
    for k in range(levels, 0, -1):
        y = expand_1d(y[None, :], sizes[k - 1], mode)[0]
    return y


def lap_recon_1d(f, levels, mode):
    g = f
    laps, sizes = [], [len(f)]
    for _ in range(levels):
        gn = reduce_1d(g[None, :], mode)[0]
        up = expand_1d(gn[None, :], len(g), mode)[0]
        laps.append(g - up)
        g = gn
        sizes.append(len(g))
    y = g
    for k in range(levels - 1, -1, -1):
        y = expand_1d(y[None, :], sizes[k], mode)[0] + laps[k]
    return y


def affected_width(err, tol=1e-12):
    """从左端数：误差绝对值超过 tol 的连续像素数。"""
    e = np.abs(err)
    w = 0
    for v in e:
        if v > tol:
            w += 1
        else:
            break
    return w


print("=" * 72)
print("命题 1：补零延拓下，常数信号 L 层高斯重建的暗边宽度 W_L = 2^L（R=1）")
print("=" * 72)
N, c = 128, 180.0
flat = np.full(N, c)
for L in range(1, 7):
    y = gauss_recon_1d(flat, L, "zero")
    w = affected_width(y - flat)
    # 最外像素的有符号偏差（暗 => 负）
    print(f"L={L}  实测暗边宽度={w:3d}  理论 2^L={2**L:3d}  "
          f"边端偏差={y[0]-c:+.6e}  内部最大|误差|={np.abs(y[2**L:]-c).max():.2e}")

print()
print("=" * 72)
print("命题 1 补充：边端偏差随层数加深（常数 c=180，补零不重归一化）")
print("=" * 72)
for L in range(1, 7):
    y = gauss_recon_1d(flat, L, "zero")
    print(f"L={L}  第0列偏差={y[0]-c:+.6f}  第1列偏差={y[1]-c:+.6f}  "
          f"第2列偏差={y[2]-c:+.6f}")

print()
print("=" * 72)
print("命题 1 的机制直证：单层 REDUCE 首样本 g[0]=3c/4，EXPAND 后前两像素变暗")
print("=" * 72)
g1 = reduce_1d(flat[None, :], "zero")[0]
print(f"REDUCE: g[0]={g1[0]:.6f}（理论 3c/4={0.75*c:.6f}），"
      f"内部 g[1]={g1[1]:.6f}（理论 c={c:.6f}）")
y1 = expand_1d(g1[None, :], N, "zero")[0]
print(f"EXPAND: y[0]={y1[0]:.6f}（理论 3c/4），y[1]={y1[1]:.6f}（理论 7c/8={0.875*c:.6f}），"
      f"y[2]={y1[2]:.6f}（理论 c）")

print()
print("=" * 72)
print("命题 2：对称延拓消除暗边；L=1 外两列误差界 |e0|<=G/4, |e1|<=7G/8；内部 |e|<=3D/4")
print("=" * 72)
# 线性斜坡 f[n]=a*n，G=|a|（一阶差分），D=0（二阶差分）
a = 3.0
ramp = a * np.arange(N, dtype=np.float64)
G = np.abs(np.diff(ramp)).max()
D = np.abs(np.diff(ramp, 2)).max()
print(f"斜坡 f[n]={a}n：G=|一阶差分|={G:.4f}，D=|二阶差分|={D:.4f}")
ys = gauss_recon_1d(ramp, 1, "sym")
err = ys - ramp
print(f"对称 L=1：列0 |e|={abs(err[0]):.6f}（界 G/4={G/4:.6f}），"
      f"列1 |e|={abs(err[1]):.6f}（界 7G/8={7*G/8:.6f}）")
print(f"          内部(n>=2) |误差| 最大 = {np.abs(err[2:-2]).max():.2e}（斜坡 D=0，应为 0）")
print(f"补零 L=1 对照：列0 |e|={abs((gauss_recon_1d(ramp,1,'zero')-ramp)[0]):.6f}"
      f"（补零引入零样本，不受 G 界约束）")
# 一般平滑信号：高斯包络
nn = np.arange(N, dtype=np.float64)
sig = 180.0 + 60.0 * np.exp(-((nn - 40) / 18.0) ** 2)
Gg = np.abs(np.diff(sig)).max()
Dg = np.abs(np.diff(sig, 2)).max()
print()
print(f"高斯包络信号：G={Gg:.4f}，D={Dg:.4f}")
for mode in ("zero", "sym"):
    e = gauss_recon_1d(sig, 1, mode) - sig
    if mode == "sym":
        edge_note = f"列0界 G/4={Gg/4:.4f}, 列1界 7G/8={7*Gg/8:.4f}"
    else:
        edge_note = "补零外两列含零样本，无 G 界"
    print(f"{mode:4s} L=1：列0 |e|={abs(e[0]):8.4f} 列1 |e|={abs(e[1]):8.4f}（{edge_note}），"
          f"内部 max|e|={np.abs(e[2:-2]).max():8.4f}（严格界 3D/4={3*Dg/4:.4f}）")

print()
print("=" * 72)
print("命题 2 补充：对称延拓对常数信号零误差（无暗边），多层亦然")
print("=" * 72)
for L in range(1, 7):
    e = gauss_recon_1d(flat, L, "sym") - flat
    print(f"L={L}  对称重建 max|误差|={np.abs(e).max():.2e}")

print()
print("=" * 72)
print("精确重建对照：拉普拉斯金字塔（同延拓分析/合成）两种延拓都应精确")
print("=" * 72)
for mode in ("zero", "sym"):
    worst = max(np.abs(lap_recon_1d(sig, L, mode) - sig).max() for L in range(1, 7))
    print(f"mode={mode:4s}  L=1..6 最大重建误差 = {worst:.2e}")
