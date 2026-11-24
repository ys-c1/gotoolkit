#!/usr/bin/env python3
# 抖动矩阵频谱分析原型（验证用，正式实现见 internal/dither/dither.go）。
# 运行: python3 -I analysis/dither_prototype.py
import numpy as np

N = 8
SEED = 20261125          # 固定 LCG 种子，保证可复现
LCG_A, LCG_C, LCG_M = 1103515245, 12345, 1 << 31
SIGMA = 1.5              # void-and-cluster 高斯核标准差（单位：像素）
SWAP_PASSES = 4          # 频谱优化矩阵的交换轮数
SWAP_EPS = 1e-6          # 频谱交换接受阈值（最小真实改进约 8e-2，舍入噪声约 1e-13）


class LCG:
    """与 Go 实现共用同一参数的线性同余生成器，rand() -> [0,1)。"""
    def __init__(self, seed):
        self.s = seed & (LCG_M - 1)

    def rand(self):
        self.s = (LCG_A * self.s + LCG_C) % LCG_M
        return self.s / LCG_M


def bayer_matrix(n=N):
    """M1：Bayer 递归矩阵，B_1=[[0,2],[3,1]]，每次放大 2 倍。"""
    b = np.array([[0, 2], [3, 1]], dtype=np.float64)
    while b.shape[0] < n:
        k = b.shape[0]
        up = np.block([[4 * b, 4 * b + 2], [4 * b + 3, 4 * b + 1]])
        b = up
    return b


def gaussian_kernel(n, sigma):
    yy, xx = np.mgrid[0:n, 0:n]
    d2 = (xx - n // 2) ** 2 + (yy - n // 2) ** 2
    g = np.exp(-d2 / (2.0 * sigma * sigma))
    g[n // 2, n // 2] = 0.0          # 不含自身
    return g


def void_and_cluster(n, seed, sigma):
    """M2：Ulichney void-and-cluster，周期边界，返回 0..n^2-1 的等级矩阵。"""
    rng = LCG(seed)
    g = gaussian_kernel(n, sigma)
    m = np.zeros((n, n), dtype=np.int64)
    # 1) 随机初始二值图案，密度 1/2
    order = list(range(n * n))
    for i in range(len(order) - 1, 0, -1):
        j = int(rng.rand() * (i + 1))
        order[i], order[j] = order[j], order[i]
    for idx in order[: n * n // 2]:
        m[idx // n, idx % n] = 1

    def convolution(pattern):
        # 直接循环卷积，与 Go convolvePeriodic 同式：
        # v[y,x]=Σ pattern[ky,kx]*g[(y-ky) mod n,(x-kx) mod n]。
        p = pattern.astype(np.float64)
        v = np.zeros((n, n))
        for y in range(n):
            for x in range(n):
                s = 0.0
                for ky in range(n):
                    for kx in range(n):
                        if p[ky, kx] == 0:
                            continue
                        s += p[ky, kx] * g[(y - ky) % n, (x - kx) % n]
                v[y, x] = s
        return v

    rank = np.zeros((n, n), dtype=np.int64)
    total = n * n // 2
    # 2) 移除阶段：反复找最紧簇中的 1，按移除顺序赋等级 0..total-1
    ones = m.copy()
    for r in range(total):
        v = convolution(ones)
        idx = np.argmax(np.where(ones == 1, v, -np.inf))
        y, x = divmod(int(idx), n)
        ones[y, x] = 0
        rank[y, x] = r
    # 3) 加入阶段：从初始图案出发，反复找最大空隙中的 0，赋等级 total..n²-1
    add_rank = total
    zeros = m.copy()
    while add_rank < n * n:
        v = convolution(zeros)
        idx = np.argmax(np.where(zeros == 0, v, -np.inf))
        y, x = divmod(int(idx), n)
        zeros[y, x] = 1
        rank[y, x] = add_rank
        add_rank += 1
    return rank


def lowfreq_mask(n):
    """低频集合（fftshift 坐标系，中心在 (n/2,n/2)）：
    主集合半径 r<=2，共 13 个系数（含 DC）；
    对照集合 r<=sqrt5。非 DC 泄漏由调用方减去中心 DC。"""
    cy = cx = n // 2
    yy, xx = np.indices((n, n))
    r2 = (yy - cy) ** 2 + (xx - cx) ** 2
    return r2 <= 4, r2 <= 5


def spectral_metrics(mat):
    """DFT 指标：M 先去均值再做未归一化 DFT，E_LF=Σ|F|²（非 DC，r<=2），
    E_tot 用 Parseval 恒等式校验。"""
    n = mat.shape[0]
    x = mat.astype(np.float64)
    x = x - x.mean()
    f = np.fft.fft2(x)
    p = np.abs(f) ** 2
    m2, m3 = lowfreq_mask(n)
    e_tot = p.sum()
    e_lf = np.fft.fftshift(p)[m2].sum() - np.fft.fftshift(p)[n // 2, n // 2]
    e_lf_wide = np.fft.fftshift(p)[m3].sum() - np.fft.fftshift(p)[n // 2, n // 2]
    parseval = (n * n) * (x ** 2).sum()
    return e_lf, e_lf_wide, e_tot, parseval


def spectral_swap(mat, passes, seed):
    """M3：以 M2 为初值，贪心交换使 r<=2 非 DC 能量下降的像素对。"""
    rng = LCG(seed ^ 0x9E3779B9)
    n = mat.shape[0]
    cur = mat.astype(np.int64).copy()
    mask, _ = lowfreq_mask(n)
    cy = cx = n // 2

    def energy(m):
        f = np.fft.fft2(m.astype(np.float64) - m.mean())
        p = np.abs(f) ** 2
        ps = np.fft.fftshift(p)
        return ps[mask].sum() - ps[cy, cx]

    e0 = energy(cur)
    pairs = [(i, j) for i in range(n * n) for j in range(i + 1, n * n)]
    for _ in range(passes):
        cand = pairs[:]
        # 固定种子的洗牌，保证扫描顺序可复现
        for i in range(len(cand) - 1, 0, -1):
            j = int(rng.rand() * (i + 1))
            cand[i], cand[j] = cand[j], cand[i]
        improved = False
        for a, b in cand:
            ya, xa = divmod(a, n)
            yb, xb = divmod(b, n)
            cur[ya, xa], cur[yb, xb] = cur[yb, xb], cur[ya, xa]
            e = energy(cur)
            if e < e0 - SWAP_EPS:
                e0, improved = e, True
            else:
                cur[ya, xa], cur[yb, xb] = cur[yb, xb], cur[ya, xa]
        if not improved:
            break
    return cur


def dither(img, mat):
    """周期平铺的有序抖动二值化，阈值等级 -> 比较 input*n²/256。"""
    n = mat.shape[0]
    h, w = img.shape
    yy, xx = np.mgrid[0:h, 0:w]
    thr = mat[yy % n, xx % n]
    scaled = img.astype(np.float64) * (n * n) / 256.0
    return (scaled > thr).astype(np.uint8) * 255


def test_suite():
    h = w = 256
    yy, xx = np.mgrid[0:h, 0:w].astype(np.float64)
    gx = np.tile(np.linspace(0, 255, w), (h, 1))                       # 水平渐变
    radial = np.clip(np.hypot(xx - 128, yy - 128) * 1.4, 0, 255)       # 径向缓坡
    rng = np.random.default_rng(SEED)
    blob = np.zeros((h, w))
    for _ in range(24):                                                # 高斯斑平滑图
        cx0, cy0 = rng.integers(0, w), rng.integers(0, h)
        s = rng.uniform(12, 48)
        a = rng.uniform(-80, 80)
        blob += a * np.exp(-((xx - cx0) ** 2 + (yy - cy0) ** 2) / (2 * s * s))
    blob = np.clip(128 + blob, 0, 255)
    flats = [np.full((h, w), g) for g in (32, 64, 96, 128, 160, 192, 224)]
    return flats + [gx, radial, blob]


def main():
    m1 = bayer_matrix()
    m2 = void_and_cluster(N, SEED, SIGMA)
    m3 = spectral_swap(m2, SWAP_PASSES, SEED)

    names = ["M1_bayer", "M2_void_cluster", "M3_spectral_swap"]
    mats = [m1, m2, m3]
    print("== 矩阵内容（8x8，等级 0..63）==")
    for name, m in zip(names, mats):
        print(name)
        for row in m:
            print(" ".join(f"{int(v):2d}" for v in row))
        assert sorted(map(int, m.flatten())) == list(range(64)), "必须是 0..63 的排列"
        print()

    print("== 频谱指标（未归一化 DFT，去均值；Parseval 校验应与 E_tot 相等）==")
    for name, m in zip(names, mats):
        e_lf, e_lf_wide, e_tot, pv = spectral_metrics(m)
        print(f"{name:20s} E_LF(r<=2,12项)={e_lf:12.2f}  "
              f"E_LF'(r<=sqrt5)={e_lf_wide:12.2f}  E_tot={e_tot:12.2f}  "
              f"parseval={pv:12.2f}  占比={100*e_lf/e_tot:6.3f}%")

    print("\n== 量化误差（测试图集 MSE/MAE，二值输出按 0/255）==")
    suite = test_suite()
    for name, m in zip(names, mats):
        mses, maes = [], []
        for img in suite:
            out = dither(img, m)
            err = out.astype(np.float64) - img.astype(np.float64)
            mses.append((err ** 2).mean())
            maes.append(np.abs(err).mean())
        print(f"{name:20s} MSE={np.mean(mses):10.4f}  MAE={np.mean(maes):8.4f}")

    print("\n== 规则纹对比度（8x8 基频格：阈值级 1..63 二值图案谱的均值）==")
    cy = cx = N // 2
    for name, m in zip(names, mats):
        pars, anis = [], []
        for level in range(1, N * N):
            b = (m < level).astype(np.float64)
            b -= b.mean()
            p = np.abs(np.fft.fftshift(np.fft.fft2(b))) ** 2
            nz = p.copy()
            nz[cy, cx] = 0.0
            pars.append(nz.max() / (nz.sum() / (N * N - 1)))   # 峰均比 PAR
            anis.append((p[cy, :].sum() + p[:, cy].sum()) / p.sum())  # 轴对齐能量占比
        print(f"{name:20s} PAR(峰均比)={np.mean(pars):6.2f}  各向异性={np.mean(anis):6.3f}")


if __name__ == "__main__":
    main()
