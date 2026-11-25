#!/usr/bin/env python3
"""Lloyd-Max 最优标量量化：8 位源降到 b 位时的电平重摆。

建模（带约束的分配问题）：
  源直方图 h[v], v = 0..255，非空桶集合 S = {v : h[v] > 0}
  变量：K = 2^b 个电平 y_1 < ... < y_K（整数，0..255），
        分配 x[v,k] ∈ {0,1}
  约束：每个非空桶恰分到一个电平  Σ_k x[v,k] = 1,  v ∈ S   （覆盖全部非空桶）
        电平数固定 = K，电平严格单调
        每个电平至少分到一个非空桶（空桶数 = 0）
  目标：min Σ_v h[v] Σ_k x[v,k] (v - y_k)^2                    （量化误差 SSE）

最优性条件（Lloyd-Max）：
  (C1) 质心条件：y_k = E[v | v ∈ C_k]（Voronoi 区间的条件期望）
  (C2) 最近邻条件：C_k 是电平的 Voronoi 区间，边界 b_k = (y_k + y_{k+1}) / 2

由 (C2)，最优划分的区间是桶轴上的连续段，于是一维 DP 给出全局最优：
  f[k][j] = min_{i<j} f[k-1][i] + cost(i..j-1)
cost 用前缀和 O(1) 算，电平取区间质心（四舍五入到整数桶）。

对照方案：等距电平 u_k = round((k + 0.5) * 256 / K)。
"""
import csv
import numpy as np
import matplotlib

matplotlib.use("Agg")
from matplotlib import font_manager, pyplot as plt

# ---------------------------------------------------------------- 源图像
# 256x256 水平渐变，经 smoothstep 影调曲线并压缩到 [48, 207]：
# 直方图非均匀（两端重、中部稀），[48,207] 之外全部为空桶。
W = H = 256
t = np.arange(W) / (W - 1)
s = 3 * t**2 - 2 * t**3                      # smoothstep
col = 48 + np.round(159 * s).astype(int)     # 每列的灰度值
img = np.broadcast_to(col, (H, W))
h = np.bincount(img.ravel(), minlength=256).astype(float)
N = h.sum()
V = 256

# ---------------------------------------------------------------- 前缀和
S0 = np.concatenate([[0.0], np.cumsum(h)])
S1 = np.concatenate([[0.0], np.cumsum(h * np.arange(V))])
S2 = np.concatenate([[0.0], np.cumsum(h * np.arange(V) ** 2)])


def cell_cost_level(i, j):
    """区间 [i, j]（含端点）的最小代价与最优整数电平。"""
    m = S0[j + 1] - S0[i]
    if m == 0:
        return 0.0, i
    s1 = S1[j + 1] - S1[i]
    s2 = S2[j + 1] - S2[i]
    y = int(round(s1 / m))
    y = min(max(y, i), j)
    return s2 - 2 * y * s1 + y * y * m, y


# 代价矩阵 C[i][j] 与电平矩阵 Y[i][j]（i <= j）
C = np.zeros((V, V))
Y = np.zeros((V, V), dtype=int)
for i in range(V):
    for j in range(i, V):
        C[i, j], Y[i, j] = cell_cost_level(i, j)


def optimal_quantizer(K):
    """一维 DP：把桶 0..255 划成 K 个连续区间，最小化总 SSE。返回 (cells, levels)。"""
    INF = np.inf
    f = np.full((K + 1, V + 1), INF)
    arg = np.full((K + 1, V + 1), -1, dtype=int)
    f[0, 0] = 0.0
    for k in range(1, K + 1):
        for j in range(k, V + 1):                # 前 j 个桶划成 k 段
            # i 是上一段的结束位置（段 [i, j-1]）
            costs = f[k - 1, :j] + C[:j, j - 1]
            i = int(np.argmin(costs))
            f[k, j] = costs[i]
            arg[k, j] = i
    cells, j = [], V
    for k in range(K, 0, -1):
        i = arg[k, j]
        cells.append((i, j - 1))
        j = i
    cells.reverse()
    levels = [Y[i, j] for (i, j) in cells]
    return cells, levels


def uniform_quantizer(K):
    """等距电平 + 最近邻（等宽 Voronoi 区间）。"""
    levels = [int(round((k + 0.5) * V / K)) for k in range(K)]
    bounds = [0] + [(levels[k] + levels[k + 1]) // 2 + 1 for k in range(K - 1)] + [V]
    cells = [(bounds[k], bounds[k + 1] - 1) for k in range(K)]
    return cells, levels


def evaluate(cells, levels):
    """返回 (MSE, 空桶数, 各区间质量)。"""
    masses = [S0[j + 1] - S0[i] for (i, j) in cells]
    sse = 0.0
    for (i, j), y in zip(cells, levels):
        m = S0[j + 1] - S0[i]
        if m == 0:
            continue
        s1 = S1[j + 1] - S1[i]
        s2 = S2[j + 1] - S2[i]
        sse += s2 - 2 * y * s1 + y * y * m
    empty = sum(1 for m in masses if m == 0)
    return sse / N, empty, masses


def centroid_resid(cells, levels):
    """(C1) 质心条件：整数电平与区间条件期望的最大偏差（应 < 0.5，仅舍入）。"""
    resid = 0.0
    for (i, j), y in zip(cells, levels):
        m = S0[j + 1] - S0[i]
        if m == 0:
            continue
        mu = (S1[j + 1] - S1[i]) / m
        resid = max(resid, abs(y - mu))
    return resid


def boundary_optimal(cells):
    """(C2) 离散边界条件：任一相邻边界向任一侧移动一个桶，总代价不下降。

    这是 Voronoi 最近邻条件在离散桶上的等价形式（连续情形即
    b_k = (y_k + y_{k+1}) / 2）。整数电平舍入会让逐桶 Voronoi 检查
    在边界桶上差 1 桶，故用代价不可改进来判定。
    """
    eps = 1e-9
    for (i, m), (m2, j) in zip(cells, cells[1:]):
        assert m2 == m + 1
        cur = C[i, m] + C[m2, j]
        if i < m and C[i, m - 1] + C[m, j] < cur - eps:   # 边界左移一桶
            return False
        if m2 < j and C[i, m + 1] + C[m2 + 1, j] < cur - eps:  # 边界右移一桶
            return False
    return True


# ---------------------------------------------------------------- 计算三档
BITS = [4, 5, 6]
rows = []
detail = {}
for b in BITS:
    K = 2**b
    oc, ol = optimal_quantizer(K)
    uc, ul = uniform_quantizer(K)
    mse_o, emp_o, masses_o = evaluate(oc, ol)
    mse_u, emp_u, _ = evaluate(uc, ul)
    resid, vor = centroid_resid(oc, ol), boundary_optimal(oc)
    resid_u = centroid_resid(uc, ul)
    assert emp_o == 0, f"{b} 位：最优解存在空桶"
    assert mse_o < mse_u, f"{b} 位：最优解误差不低于等距"
    assert vor, f"{b} 位：最优解不满足边界最优条件"
    assert all(ol[k] < ol[k + 1] for k in range(K - 1)), "电平非单调"
    rows.append(dict(bits=b, K=K, emp_u=emp_u, emp_o=emp_o,
                     mse_u=mse_u, mse_o=mse_o,
                     dmse=mse_o - mse_u, pct=100 * (mse_u - mse_o) / mse_u,
                     resid=resid, resid_u=resid_u))
    detail[b] = dict(uc=uc, ul=ul, oc=oc, ol=ol, masses_o=masses_o)

# ---------------------------------------------------------------- 输出表格
print(f"源直方图：非空桶 {int((h > 0).sum())}/256，像素 {int(N)}")
print()
hdr = ("位深", "K", "空桶(等距)", "空桶(最优)", "MSE(等距)", "MSE(最优)",
       "ΔMSE(最优-等距)", "误差降幅", "质心残差(最优)", "质心残差(等距)")
print(("{:>4}{:>5}{:>10}{:>10}{:>12}{:>12}{:>16}{:>10}{:>14}{:>14}").format(*hdr))
for r in rows:
    print(("{:>4}{:>5}{:>10}{:>10}{:>12.3f}{:>12.3f}{:>16.3f}{:>9.1f}%{:>14.3f}{:>14.3f}")
          .format(r["bits"], r["K"], r["emp_u"], r["emp_o"], r["mse_u"],
                  r["mse_o"], r["dmse"], r["pct"], r["resid"], r["resid_u"]))

with open("analysis/summary.csv", "w", newline="") as fp:
    w = csv.writer(fp)
    w.writerow(["bits", "K", "empty_uniform", "empty_optimal", "mse_uniform",
                "mse_optimal", "delta_mse", "reduction_pct",
                "centroid_resid_optimal", "centroid_resid_uniform"])
    for r in rows:
        w.writerow([r["bits"], r["K"], r["emp_u"], r["emp_o"],
                    f"{r['mse_u']:.4f}", f"{r['mse_o']:.4f}",
                    f"{r['dmse']:.4f}", f"{r['pct']:.2f}",
                    f"{r['resid']:.4f}", f"{r['resid_u']:.4f}"])

with open("analysis/levels.csv", "w", newline="") as fp:
    w = csv.writer(fp)
    w.writerow(["bits", "scheme", "k", "level", "cell_lo", "cell_hi", "cell_mass"])
    for b in BITS:
        d = detail[b]
        for k, ((i, j), y) in enumerate(zip(d["uc"], d["ul"])):
            w.writerow([b, "uniform", k, y, i, j, int(S0[j + 1] - S0[i])])
        for k, ((i, j), y) in enumerate(zip(d["oc"], d["ol"])):
            w.writerow([b, "optimal", k, y, i, j, int(S0[j + 1] - S0[i])])

# ---------------------------------------------------------------- 出图
font_manager.fontManager.addfont(
    "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf")
plt.rcParams.update({
    "font.family": "Noto Sans CJK SC",
    "axes.unicode_minus": False,
})
SURFACE, INK, INK2, MUTED = "#fcfcfb", "#0b0b0b", "#52514e", "#898781"
GRID, BASE, BAR = "#e1e0d9", "#c3c2b7", "#c3c2b7"
C_UNI, C_OPT = "#2a78d6", "#eb6834"          # 已通过 validate_palette.js 六项检查

for b in BITS:
    d = detail[b]
    r = rows[BITS.index(b)]
    K = 2**b
    fig, ax = plt.subplots(figsize=(9.6, 4.4), dpi=160)
    fig.patch.set_facecolor(SURFACE)
    ax.set_facecolor(SURFACE)

    ax.bar(np.arange(V), h, width=0.88, color=BAR, label="源直方图（每桶像素数）")

    ymax = h.max()
    y_uni, y_opt = ymax * 1.16, ymax * 1.05
    # 2px 表面色圆环（先画大一圈的表面色 marker）
    for ys, lev in ((y_uni, d["ul"]), (y_opt, d["ol"])):
        ax.scatter(lev, [ys] * K, s=180, facecolors="none",
                   edgecolors=SURFACE, linewidths=2.2, zorder=3)
    # 等距电平：空桶画空心，非空画实心（形状区分，不靠颜色单独表达）
    emp_mask = [S0[j + 1] - S0[i] == 0 for (i, j) in d["uc"]]
    ul = np.array(d["ul"])
    em = np.array(emp_mask)
    ax.scatter(ul[~em], [y_uni] * int((~em).sum()), marker="v", s=80,
               color=C_UNI, zorder=4, label="等距电平（实心=有像素）")
    if em.any():
        ax.scatter(ul[em], [y_uni] * int(em.sum()), marker="v", s=80,
                   facecolors="none", edgecolors=C_UNI, linewidths=1.6,
                   zorder=4, label="等距电平·空桶（无像素落入）")
    ax.scatter(d["ol"], [y_opt] * K, marker="^", s=80, color=C_OPT,
               zorder=4, label="最优电平（Lloyd-Max）")

    ax.set_xlim(-4, 260)
    ax.set_ylim(0, ymax * 1.52)
    ax.set_xticks([0, 48, 64, 128, 192, 207, 255])
    ax.yaxis.grid(True, color=GRID, linewidth=1)
    ax.set_axisbelow(True)
    for side in ("top", "right"):
        ax.spines[side].set_visible(False)
    for side in ("left", "bottom"):
        ax.spines[side].set_color(BASE)
    ax.tick_params(colors=INK2, labelsize=9)
    ax.set_title(f"{b} 位（K={K}）电平位置对照",
                 loc="left", fontsize=13, fontweight="bold", color=INK, pad=18)
    ax.text(0, 1.015, f"等距空桶 {r['emp_u']} 个 → 最优空桶 0 个 · "
            f"MSE {r['mse_u']:.1f} → {r['mse_o']:.1f}（降 {r['pct']:.1f}%）",
            transform=ax.transAxes, fontsize=9.5, color=INK2)
    ax.set_xlabel("灰度值", fontsize=10, color=INK2)
    ax.set_ylabel("像素数", fontsize=10, color=INK2)
    leg = ax.legend(loc="upper center", bbox_to_anchor=(0.5, 0.98), ncol=2,
                    frameon=False, fontsize=9, labelcolor=INK2,
                    borderaxespad=0.1)
    fig.tight_layout()
    fig.savefig(f"analysis/levels_{b}bit.png", facecolor=SURFACE)
    plt.close(fig)
    print(f"已写出 analysis/levels_{b}bit.png")
