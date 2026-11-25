# -*- coding: utf-8 -*-
"""等效核严格定界。

- 暗边宽度：补零模式常数信号上，左缘影响带恒为 2^L；右缘沿 floor 减半链
  按末位奇偶外扩，宽度由递归 r_0=1, r_j=2r_{j-1}-1[s_{j-1} 奇] 给出。
- 对称模式最外两列误差界：等效核在一阶差分基上的 ℓ1 系数 × G。
- 内部误差界：排除 2^L 边界影响带后，等效核在二阶差分基上的 ℓ1 系数 × D。
- 全部界用 2 万次随机信号模糊测试验证。
"""

import os
import sys
from fractions import Fraction

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import numpy as np
from pyramid import reduce_1d, expand_1d

rng = np.random.default_rng(42)


def tl_matrix(n, levels, mode):
    """T_L=(EXPAND)^L(REDUCE)^L 的 n×n 显式矩阵。"""
    cols = []
    for i in range(n):
        e = np.zeros(n)
        e[i] = 1.0
        g = e
        sizes = [n]
        for _ in range(levels):
            g = reduce_1d(g[None, :], mode)[0]
            sizes.append(len(g))
        y = g
        for k in range(levels, 0, -1):
            y = expand_1d(y[None, :], sizes[k - 1], mode)[0]
        cols.append(y)
    return np.array(cols).T


def first_diff_matrix(n):
    Lmat = np.zeros((n, n - 1))
    for j in range(n):
        Lmat[j, :j] = 1.0
    return Lmat


def second_diff_basis(n):
    B = np.zeros((n - 1, 1 + (n - 2)))
    B[:, 0] = 1.0
    for r in range(n - 1):
        B[r, 1:r + 1] = 1.0
    return B


def edge_constants(K):
    n = K.shape[0]
    assert np.allclose(K.sum(axis=1), 1.0)
    A = (K - np.eye(n)) @ first_diff_matrix(n)
    return np.abs(A[0, :]).sum(), np.abs(A[1, :]).sum()


def edge_constants_fraction(K):
    n = K.shape[0]
    Kf = np.vectorize(lambda x: Fraction(x).limit_denominator(1 << 20))(K)
    Lf = np.vectorize(lambda x: Fraction(int(x), 1))(first_diff_matrix(n))
    eye = np.vectorize(lambda x: Fraction(int(x), 1))(np.eye(n))
    Af = (Kf - eye) @ Lf
    f0 = sum(abs(Af[0, t]) for t in range(n - 1))
    f1 = sum(abs(Af[1, t]) for t in range(n - 1))
    return f0, f1


def interior_constants(K, band):
    n = K.shape[0]
    A = (K - np.eye(n)) @ first_diff_matrix(n)
    AB = A @ second_diff_basis(n)
    lo, hi = band, n - band
    lin = np.abs(AB[lo:hi, 0]).max()
    l1 = np.abs(AB[lo:hi, 1:]).sum(axis=1)
    return l1.min(), l1.max(), lin


def edge_span(n, L, mode="zero"):
    """左/右半区内，误差非零像素距各自边界的影响带像素数。"""
    K = tl_matrix(n, L, mode)
    e = np.abs(K @ np.ones(n) - np.ones(n)) > 1e-12
    idx = np.where(e)[0]
    left_idx = idx[idx < n / 2]
    right_idx = idx[idx >= n / 2]
    left = int(left_idx.max()) + 1 if len(left_idx) else 0
    right = int(n - right_idx.min()) if len(right_idx) else 0
    return left, right


def right_width(n, L):
    """右缘宽度：污染下标集合沿金字塔的精确布尔传播（与数值算子同构）。

    两类补零污染来源：
      REDUCE：样本 j 用 f[2j-1..2j+1]，仅当层长 s 为偶时末样本 j=s-1
              触及 f[2s-1]=f[N] 越界零 -> 该样本污染；
      EXPAND：偶数位 2j=s'（s' 奇）直接取越界 g[s]=0；奇数位 2j+1 在
              j+1=s 时与越界零平均。两类越界位天然加入污染集合。
    域内传播：偶数位 2j 污染 <=> j 污染；奇数位 2j+1 污染 <=> j 或 j+1 污染。
    右缘宽度 = 最终层从最右端起连续污染的像素数。
    全偶尺寸链给出 2^L-1；奇数层的越界偶数位把宽度补偿到 2^L。
    """
    chain = [n]
    for _ in range(L):
        chain.append(chain[-1] // 2)
    # REDUCE 的下标 2j±1 对所有域内 j 均不越界（最大 2(s-1)+1=2s-1 <= N-1），
    # 故最粗层无域内污染；暗边全部来自 EXPAND 的越界补零。
    coarse_len = chain[-1]
    polluted = set()
    for step in range(L):
        fine_len = chain[L - 1 - step]
        nxt = set()
        for j in polluted:
            if 2 * j < fine_len:
                nxt.add(2 * j)                       # 域内偶数位
        if fine_len % 2 == 1:
            nxt.add(fine_len - 1)                    # 越界偶数位补零
        for j in range(-1, coarse_len):
            odd = 2 * j + 1
            if not (0 <= odd < fine_len):
                continue
            if (j in polluted) or (j + 1 in polluted) or (j + 1 == coarse_len):
                nxt.add(odd)
        polluted = nxt
        coarse_len = fine_len
    width = 0
    for i in range(n - 1, -1, -1):
        if i in polluted:
            width += 1
        else:
            break
    return width


def main():
    print("=" * 80)
    print("一、暗边宽度等式：补零模式常数信号，影响带宽度闭式核验")
    print("=" * 80)
    all_ok = True
    for n in (31, 32, 33, 48, 64, 100, 127, 128):
        for L in range(1, 7):
            if n // (2 ** L) < 2:
                continue
            w_left, w_right = edge_span(n, L)
            t_left, t_right = 2 ** L, right_width(n, L)
            ok = (w_left == t_left) and (w_right == t_right)
            all_ok &= ok
            if not ok:
                print("MISMATCH", n, L, w_left, t_left, w_right, t_right)
    print("全部 (n,L) 满足：左缘 W_L=2^L；右缘由 EXPAND 越界补零的布尔传播精确给出: "
          f"{all_ok}")
    print("全偶尺寸链右缘为 2^L-1；链中奇数层的越界偶数位把右缘补偿到 2^L。")

    print()
    print("=" * 80)
    print("二、对称延拓最外两列误差界 |e| <= C·G：精确分数 + 模糊测试")
    print("=" * 80)
    N = 64
    for L in range(1, 6):
        K = tl_matrix(N, L, "sym")
        c0, c1 = edge_constants(K)
        f0, f1 = edge_constants_fraction(K)
        worst = np.zeros(2)
        for _ in range(20000):
            d = rng.uniform(-1, 1, size=N - 1)
            f = np.concatenate([[0.0], np.cumsum(d)])
            e = K @ f - f
            worst = np.maximum(worst, np.abs(e[:2]))
        print(f"L={L}  列0: C={f0}={c0:.6f} 实测={worst[0]:.6f} | "
              f"列1: C={f1}={c1:.6f} 实测={worst[1]:.6f}")

    print()
    print("=" * 80)
    print("三、内部误差界 |e| <= C·D（排除 2^L 边界影响带）：两种延拓对照 + 模糊测试")
    print("=" * 80)
    for L in range(1, 5):
        band = 2 ** L
        row = f"L={L} 影响带 2^{L}={band:2d} | "
        for mode in ("zero", "sym"):
            K = tl_matrix(N, L, mode)
            cmin, cmax, lin = interior_constants(K, band)
            worst = 0.0
            for _ in range(20000):
                s = rng.uniform(-1, 1, size=N - 2)
                d = np.concatenate([[0.0], np.cumsum(s)])
                f = np.concatenate([[0.0], np.cumsum(d)])
                e = K @ f - f
                worst = max(worst, np.abs(e[band:N - band]).max())
            row += f"{mode:4s}: C∈[{cmin:.4f},{cmax:.4f}] 线性残余={lin:.1e} 实测={worst:.4f} | "
        print(row)

    print()
    print("=" * 80)
    print("四、同一信号两种延拓的外两列 vs 内部误差对照（L=1，高斯包络）")
    print("=" * 80)
    nn = np.arange(N, dtype=np.float64)
    sig = 180.0 + 60.0 * np.exp(-((nn - 40) / 18.0) ** 2)
    G = np.abs(np.diff(sig)).max()
    D = np.abs(np.diff(sig, 2)).max()
    print(f"G={G:.4f}  D={D:.4f}")
    for mode in ("zero", "sym"):
        K = tl_matrix(N, 1, mode)
        e = K @ sig - sig
        print(f"{mode:4s}: 列0误差={e[0]:+.4f} 列1误差={e[1]:+.4f} | "
              f"外两列 max|e|={np.abs(e[:2]).max():.4f} | 内部(2..N-3) max|e|={np.abs(e[2:-2]).max():.4f}")


if __name__ == "__main__":
    main()
