"""4:2:0 色度采样-重建管线与块内相位估计。

坐标约定（关键，全部统一到「全分辨率像素」单位）
--------------------------------------------------
全分辨率像素 p 的中心位于 x = p + 0.5。
4:2:0 色度点 g（半分辨率网格）在零相位时位于 x_g = 2g + 1，
即两个像素 (2g, 2g+1) 的正中，其零相位值为这 2 个像素的面积均值
（2D 为 2x2 均值）。

块相位 δ_b（全分辨率像素单位，x、y 两轴同幅）把采样中心移到
    x = 2g + 1 + δ_b
面积核为中心 ±0.5 的 box，在相邻整数像素上给出双线性权重（边界
归一），保证平移等变。

重建先在名义网格上用核 k∈{bilinear,bicubic(Catmull-Rom)} 放大到
全分辨率，再按每块相位做一次亚像素 warp。物理上采样中心 +δ 等价于
在名义位置 x 处取到 f(x-δ)，故漂移
    rec(x) - f(x) ≈ -δ·∇f(x)
符号为约定所致，比例系数的模即梯度。

窗口 W4 / W8
------------
块边长 8 全分辨率像素（含 4x4 色度点）。估计窗 win=1 即块内 4x4
色度点（全分辨率 8x8，W4）；win=2 为 2x2 块（全分辨率 16x16，W8）。
全程 float64，不做 8bit 量化，隔离相位这一单一误差源。
"""
import numpy as np

FULL = 128          # 全分辨率边长
HALF = FULL // 2    # 色度网格边长 = 64
BLK = 8             # 块边长（全分辨率像素）
BLK_H = BLK // 2    # 块内色点数 = 4


# --------------------------------------------------------------------------
# 插值核
# --------------------------------------------------------------------------
def _kernel_weights(t, kind):
    """单位间距网格上对位置 (i+t) 插值的核权重与整数起点 start。"""
    if kind == "bilinear":
        start = int(np.floor(t))
        u = t - start
        return start, np.array([1.0 - u, u])
    if kind == "bicubic":  # Catmull-Rom, a=-0.5
        start = int(np.floor(t)) - 1
        u = t - start - 1
        u2, u3 = u * u, u * u * u
        w = np.array([
            -0.5 * u + u2 - 0.5 * u3,
            1.0 - 2.5 * u2 + 1.5 * u3,
            0.5 * u + 2.0 * u2 - 1.5 * u3,
            -0.5 * u2 + 0.5 * u3,
        ])
        return start, w
    raise ValueError(kind)


def sample1d(sig, positions, kind, clamp=True):
    """1D 信号 sig 在 positions（浮点坐标）处插值，边界钳制（默认）。"""
    n = sig.shape[0]
    out = np.zeros_like(positions, dtype=np.float64)
    for idx, t in enumerate(positions):
        start, w = _kernel_weights(float(t), kind)
        acc = 0.0
        for j, wj in enumerate(w):
            p = start + j
            if clamp:
                p = 0 if p < 0 else (n - 1 if p >= n else p)
                acc += wj * sig[p]
            elif 0 <= p < n:
                acc += wj * sig[p]
        out[idx] = acc
    return out


# --------------------------------------------------------------------------
# 4:2:0 面积采样（带块相位）
# --------------------------------------------------------------------------
def _area_weights(center, n):
    """中心 center（全分辨率单位）的 box 面积核在整数像素上的归一权重。"""
    i0 = int(np.floor(center - 0.5))
    frac = (center - 0.5) - i0  # 落在 i0+1 的权重
    raw = ((i0, 1.0 - frac), (i0 + 1, frac))
    w = np.zeros(n)
    for p, wt in raw:
        if 0 <= p < n:
            w[p] += wt
    s = w.sum()
    return w / s if s > 0 else w


def subsample_blockwise(img, delta, block_full=BLK):
    """2x2 面积平均降采样，块内施加相位 δ（标量或 (nb,nb) 块相位场）。"""
    h, w = img.shape
    nb_y, nb_x = h // block_full, w // block_full
    ch = np.zeros((h // 2, w // 2))
    if np.isscalar(delta):
        delta = np.full((nb_y, nb_x), float(delta))
    for by in range(nb_y):
        for bx in range(nb_x):
            d = float(delta[by, bx])
            for cy in range(block_full // 2):
                gy = by * (block_full // 2) + cy
                wy = _area_weights(2.0 * gy + 1.0 + d, h)
                ys = np.nonzero(wy)[0]
                for cx in range(block_full // 2):
                    gx = bx * (block_full // 2) + cx
                    wx = _area_weights(2.0 * gx + 1.0 + d, w)
                    xs = np.nonzero(wx)[0]
                    acc = 0.0
                    for yy in ys:
                        for xx in xs:
                            acc += wy[yy] * wx[xx] * img[yy, xx]
                    ch[gy, gx] = acc
    return ch


def upsample_2d(chroma, full_h, kind):
    """名义网格可分离放大。像素 p 中心 p+0.5 对应色度坐标 (p-0.5)/2。"""
    chh, cw = chroma.shape
    pos = (np.arange(full_h, dtype=np.float64) - 0.5) / 2.0
    tmp = np.zeros((full_h, cw))
    for ccol in range(cw):
        tmp[:, ccol] = sample1d(chroma[:, ccol], pos, kind)
    out = np.zeros((full_h, full_h))
    for r in range(full_h):
        out[r, :] = sample1d(tmp[r, :], pos, kind)
    return out


def warp_phase(up_full, delta_map, kind, block_full=BLK):
    """按每块相位对「整张」放大图做亚像素 warp，边缘钳制，无块缝。

    采样中心 +δ 等价于内容平移 -δ：输出像素 (r,c) 采样 up_full 在
    (c+δ_b, r+δ_b) 处（参数 +δ），得到 f(x-δ)，与降采样约定一致。
    delta_map 可为标量或 (nb,nb) 场。
    """
    h, w = up_full.shape
    if np.isscalar(delta_map):
        dfield = np.full((h // block_full, w // block_full), float(delta_map))
    else:
        dfield = delta_map
    out = np.zeros_like(up_full)
    n = h
    for r in range(h):
        by = r // block_full
        for c in range(w):
            d = float(dfield[by, c // block_full])
            tx, ty = c + d, r + d
            sx, wx = _kernel_weights(tx, kind)
            sy, wy = _kernel_weights(ty, kind)
            acc = 0.0
            for jy, wyj in enumerate(wy):
                py = sy + jy
                py = 0 if py < 0 else (n - 1 if py >= n else py)
                row_acc = 0.0
                for jx, wxj in enumerate(wx):
                    px = sx + jx
                    px = 0 if px < 0 else (n - 1 if px >= n else px)
                    row_acc += wxj * up_full[py, px]
                acc += wyj * row_acc
            out[r, c] = acc
    return out


def reconstruct(chroma, kind, block_full=BLK):
    """名义网格放大。重建端不知道采样相位，不做任何相位补偿。

    采样相位 δ 已在降采样阶段把信号平移 -δ；这里仅在名义网格上用核
    放大，净漂移 -δ·∇f 被保留下来——即观测到的整体偏色来源。
    """
    full_h = chroma.shape[0] * 2
    return upsample_2d(chroma, full_h, kind)


def apply_correction(nominal_rec, d_hat_field, kind, block_full=BLK):
    """闭环修正：用估计相位对名义重建做反向 warp 抵消 +δ 内容漂移。

    warp_phase(x, δ) 在参数 +δ 处采样产生 -δ 内容位移，故传入 -δ̂ 即
    把 +δ̂ 的内容漂移拉回。不可辨识块（nan）修正量取 0。
    """
    dfield = np.where(np.isnan(d_hat_field), 0.0, -d_hat_field)
    return warp_phase(nominal_rec, dfield, kind, block_full)


# --------------------------------------------------------------------------
# 参考梯度与块聚合
# --------------------------------------------------------------------------
def true_grad(img):
    """中心差分梯度 (gx, gy)，零填充边界。"""
    gx = np.zeros_like(img)
    gy = np.zeros_like(img)
    gx[:, 1:-1] = (img[:, 2:] - img[:, :-2]) / 2.0
    gy[1:-1, :] = (img[2:, :] - img[:-2, :]) / 2.0
    return gx, gy


def block_aggregate(field, block_full=BLK):
    """全分辨率场按块平均，返回块网格 (nb,nb)。"""
    h, w = field.shape
    nb_y, nb_x = h // block_full, w // block_full
    out = np.zeros((nb_y, nb_x))
    for by in range(nb_y):
        for bx in range(nb_x):
            out[by, bx] = field[by * block_full:(by + 1) * block_full,
                                bx * block_full:(bx + 1) * block_full].mean()
    return out


# --------------------------------------------------------------------------
# 块内相位估计：块内逐像素最小二乘
#   对块 b 内（含 win 邻域）全分辨率像素 p：
#     e(p) ≈ β g(p),  g(p)=gx(p)+gy(p),  β=-δ
#   β̂ = Σ_p e g / Σ_p g² ；物理相位 δ̂ = -β̂
# 必须在逐像素尺度回归——块级空间平均会同时把信号与回归量平均掉，
# 使恒相位也不可辨识。
# --------------------------------------------------------------------------
def estimate_phase_pixels(err, ogx, ogy, win, block_full=BLK, gthresh=1e-2):
    """块内逐像素最小二乘相位估计。

    err,e 残差；ogx,ogy 为名义重建图「可观」梯度。本管线的几何约定下
    带相位降采样产生内容 +δ 漂移，故块内 e(p)≈+δ g(p)，g=gx+gy。
    在以目标块为中心的 win 邻域全分辨率像素上：
        δ̂ = Σ_p e g / Σ_p g²
    win=1 -> W4，块内 4x4 色度点（全分辨率 8x8）；
    win=2 -> W8，2x2 块（全分辨率 16x16）。
    平坦窗（Σg²<gthresh）返回 nan。同时返回窗口梯度能量。
    """
    h, w = err.shape
    nb = h // block_full
    gfull = ogx + ogy
    out = np.full((nb, nb), np.nan)
    energy = np.full((nb, nb), np.nan)
    pad = win // 2
    for by in range(nb):
        for bx in range(nb):
            r0 = max(0, by - pad) * block_full
            r1 = min(nb, by + pad + 1) * block_full
            c0 = max(0, bx - pad) * block_full
            c1 = min(nb, bx + pad + 1) * block_full
            gw = gfull[r0:r1, c0:c1].reshape(-1)
            ew = err[r0:r1, c0:c1].reshape(-1)
            denom = float(gw @ gw)
            energy[by, bx] = denom
            if denom < gthresh:
                continue
            out[by, bx] = float(gw @ ew) / denom
    return out, energy


def directional_second_deriv(img):
    """沿 (1,1) 方向的二阶导 D = f_xx + 2 f_xy + f_yy（中心差分）。"""
    fxx = np.zeros_like(img)
    fyy = np.zeros_like(img)
    fxx[:, 1:-1] = img[:, 2:] - 2 * img[:, 1:-1] + img[:, :-2]
    fyy[1:-1, :] = img[2:, :] - 2 * img[1:-1, :] + img[:-2, :]
    fxy = np.zeros_like(img)
    fxy[1:-1, 1:-1] = ((img[2:, 2:] - img[2:, :-2]
                        - img[:-2, 2:] + img[:-2, :-2]) / 4.0)
    return fxx + 2.0 * fxy + fyy


def curvature_error_bound(d_abs, d2_window, g_energy_window, npix):
    """Taylor 二阶余项给出的相位估计误差上界。

    e_p = δ g_p + ε_p，|ε_p| ≤ (1/2) δ²·M2（M2=窗内 |D| 最大值）。
    最小二乘偏差
        |δ̂-δ| = |Σ ε g|/Σ g² ≤ (1/2)δ² M2 · Σ|g|/Σ g²
               ≤ (1/2)δ² M2 / ḡ_rms， ḡ_rms=√(Σg²/N)
    返回该上界（标量）。
    """
    m2 = float(np.max(np.abs(d2_window)))
    g_rms = np.sqrt(g_energy_window / npix)
    if g_rms < 1e-12:
        return float("inf")
    return 0.5 * (d_abs ** 2) * m2 / g_rms
