# -*- coding: utf-8 -*-
"""
design_kernel.py — 色度半采样（4:2:0 型，每维 2:1）重建核的约束最小二乘设计与评估。

模型（1D，竖直/水平两维可分离使用）：
  原信号 x[0..N-1] -> 2:1 盒式下采样 d[k] = (x[2k]+x[2k+1])/2
  重建端零插值 u：u[2k]=d[k], u[2k+1]=0
  5 抽头 FIR： xhat[n] = sum_{m=-2..2} h_m u[n+m]

参数化（关键）：零插入使一半样本为零，直接对 u 施加"和为一"的核 DC 增益只有
0.5，故采用归一化核 c 与物理抽头 h=2c 的双层写法：
  归一化核 c=(c-2,c-1,c0,c1,c2)，满足 c0+2c1+2c2=1（系数和为一）、c-j=cj（中心对称）
  物理 FIR 抽头 h=2c，其偶位增益 h0+2h2=2、奇位增益 2h1=2，两相位 DC 均无失真。
  自由参数 theta=(c0,c1,c2)，模型 xhat = 2 Z theta，Z 为由 u 构成的设计矩阵。
  双线性在该参数化下即 theta_bi=[1/2,1/4,0]，归一化 c5=[0,1/4,1/2,1/4,0]，
  物理抽头 [0,1/2,1,1/2,0]，与专用线性插值路径逐点等价（见 main 中自检）。

正规方程（KKT）写成矩阵形式：
  min ||2 Z theta - x||^2  s.t. C theta = h0,  C=[1 2 2], h0=1
  令 A=Z^T Z, b=(1/2) Z^T x（目标函数整体除以 4 不改变 argmin，乘子相应缩放）：
  [ A  C^T ] [theta ] = [b]
  [ C   0  ] [lambda]   [1]

双线性路径（保留的对照）：偶数位 d[k]，奇数位 (d[k]+d[k+1])/2，独立实现、不走 FIR。
"""
import csv
import json
import os
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.join(HERE, "data")
RES_DIR = os.path.join(HERE, "results")
TIERS = ("tier1", "tier2", "tier3")
N_TRAIN, N_TEST = 6, 4
TOL = 1e-10

# BT.601（全幅 0..255，色度居中 128）
KR, KB = 0.299, 0.114
KG = 1.0 - KR - KB


def rgb_to_ycbcr(rgb):
    rgb = rgb.astype(np.float64)
    r, g, b = rgb[..., 0], rgb[..., 1], rgb[..., 2]
    y = KR * r + KG * g + KB * b
    cb = (b - y) / (2.0 * (1.0 - KB)) + 128.0
    cr = (r - y) / (2.0 * (1.0 - KR)) + 128.0
    return y, cb, cr


def ycbcr_to_rgb(y, cb, cr):
    b = y + 2.0 * (1.0 - KB) * (cb - 128.0)
    r = y + 2.0 * (1.0 - KR) * (cr - 128.0)
    g = (y - KR * r - KB * b) / KG
    rgb = np.stack([r, g, b], axis=-1)
    return np.clip(rgb, 0.0, 255.0)


def downsample2(x):
    """2:1 盒式下采样，长度 N -> N/2，N 须为偶数。"""
    assert x.shape[-1] % 2 == 0
    return 0.5 * (x[..., 0::2] + x[..., 1::2])


def upsample_fir(d, c5):
    """零插值 + 物理抽头 2*c5 的 5 抽头 FIR，长度 K -> 2K。
    c5 为归一化核 (和为 1)；边界按 d[-1]=d[0]、d[K]=d[K-1] 复制延伸，
    与 build_ls_1d 的建模约定一致，也使末奇数位与双线性路径一致。

    直接在低速率域按相位写出（等价于零插值后卷积，避免边界相位错位）：
      偶数位 2k  : 2 c2 d[k-1] + 2 c0 d[k] + 2 c2 d[k+1]
      奇数位 2k+1: 2 c1(d[k]+d[k+1])
    """
    k = d.shape[-1]
    dprev = np.empty_like(d)
    dprev[..., 1:] = d[..., :-1]
    dprev[..., 0] = d[..., 0]
    dnext = np.empty_like(d)
    dnext[..., :-1] = d[..., 1:]
    dnext[..., -1] = d[..., -1]
    c0, c1, c2 = c5[2], c5[1], c5[0]
    out = np.empty(d.shape[:-1] + (2 * k,))
    out[..., 0::2] = 2.0 * (c2 * dprev + c0 * d + c2 * dnext)
    out[..., 1::2] = 2.0 * c1 * (d + dnext)
    return out


def upsample_bilinear(d):
    """双线性（线性插值）路径：偶数位 d[k]，奇数位 (d[k]+d[k+1])/2。"""
    n = 2 * d.shape[-1]
    dnext = np.empty_like(d)
    dnext[..., :-1] = d[..., 1:]
    dnext[..., -1] = d[..., -1]
    out = np.empty(d.shape[:-1] + (n,))
    out[..., 0::2] = d
    out[..., 1::2] = 0.5 * (d + dnext)
    return out


def upsample2d(d, c5, mode):
    """4:2:0 重建：先水平（axis=1）后竖直（axis=0），同一 1D 核可分离使用。
    d 形状 (H/2, W/2)，输出 (H, W)。"""
    if mode == "bilinear":
        f = upsample_bilinear
        kw = None
    else:
        f = upsample_fir
        kw = c5

    def apply(plane, axis):
        sig = np.moveaxis(plane, axis, -1)
        up = f(sig, kw) if kw is not None else f(sig)
        return np.moveaxis(up, -1, axis)

    h = apply(d, 1)       # 水平 W/2 -> W
    return apply(h, 0)    # 竖直 H/2 -> H


def build_ls_1d(x):
    """对一条 1D 原信号 x（长度偶数），构造零插值模型的设计矩阵 Z 与目标 x。
    偶数位： xhat[2k]=2(c2 d[k-1]+c0 d[k]+c2 d[k+1])
    奇数位： xhat[2k+1]=2 c1(d[k]+d[k+1])
    每行 Z 对应 theta=(c0,c1,c2)，模型 xhat=2 Z theta。

    边界处理：d[-1]=d[0]、d[K]=d[K-1] 复制延伸。在这两个复制位置上的外推残差只含
    边界局部信息，不代表内部统计规律，故最小二乘只取内部位置 k=1..K-2（每条 256 长
    信号丢弃 2 个边界样本，占 0.8%）；上采样仍用复制外推处理边界（见 upsample_fir）。
    不做去均值：直流分量在和为一约束下被任意可行核精确重建（DC 残差恒为零）。
    """
    n = x.shape[-1]
    d = downsample2(x)
    kmax = d.shape[-1]
    dprev = np.empty_like(d)
    dprev[..., 1:] = d[..., :-1]
    dprev[..., 0] = d[..., 0]
    dnext = np.empty_like(d)
    dnext[..., :-1] = d[..., 1:]
    dnext[..., -1] = d[..., -1]

    inner = slice(1, kmax - 1)   # 仅内部位置，避开两端复制外推
    rows, targets = [], []
    # 偶数位 n=2k
    z_even = np.zeros((kmax, 3))
    z_even[:, 0] = d
    z_even[:, 2] = dprev + dnext
    rows.append(z_even[inner])
    targets.append(x[..., 2:2 * (kmax - 1):2])
    # 奇数位 n=2k+1
    z_odd = np.zeros((kmax, 3))
    z_odd[:, 1] = d + dnext
    rows.append(z_odd[inner])
    targets.append(x[..., 3:2 * (kmax - 1) + 1:2])
    Z = np.vstack(rows)
    t = np.hstack(targets)
    return Z, t


def solve_cls(A, b, C, h):
    """约束最小二乘：min 1/2 theta'A theta - b'theta  s.t. C theta = h。
    KKT 正规方程：
      [ A  C^T ] [theta] = [b]
      [ C   0  ] [lam  ]   [h]
    """
    p = A.shape[0]
    q = C.shape[0]
    K = np.block([[A, C.T], [C, np.zeros((q, q))]])
    rhs = np.concatenate([b, h])
    sol = np.linalg.solve(K, rhs)
    return sol[:p], sol[p:]


def theta_to_c5(theta):
    """theta=(c0,c1,c2) 且对称 -> (c-2,c-1,c0,c1,c2)。"""
    c0, c1, c2 = theta
    return np.array([c2, c1, c0, c1, c2])


def load_tier(tier):
    imgs = []
    for i in range(N_TRAIN + N_TEST):
        imgs.append(np.load(os.path.join(DATA_DIR, tier, f"{i:02d}.npy")))
    return imgs[:N_TRAIN], imgs[N_TRAIN:]


def collect_normal_eq(images, mid_c5):
    """在完整 4:2:0 可分离管线上收集 1D 正规方程统计 A,b（不做逐行去均值）。

    同一个 1D 核在两个空间维级联使用，两维的输入分布不同，必须分别按真实输入统计：
      水平维：目标=原色度面的行，输入=该行 2:1 盒式下采样 d；
      竖直维：目标=水平重建后的中间信号 mid 的列（转成行），输入=其列下采样。
    竖直维的中间信号用当前迭代核 mid_c5 生成；首版以双线性核初始化（足够接近最优点，
    一次统计即可，见 main 中的迭代一致性检查）。
    每行只取内部位置（build_ls_1d 内已剔除两端复制外推样本）。
    """
    A = np.zeros((3, 3))
    b = np.zeros(3)

    def add_rows(hi, A, b):
        # 不做去均值：直流分量在和为一约束下被任意可行核精确重建，且它在正规方程中
        # 为外抽头提供必要的二次正则；实验证实去均值会解出过大的 c2 并在含 DC 的
        # 真实信号上显著放大误差（见 results/1d_centered_check.txt）。
        for row in hi.reshape(-1, hi.shape[-1]):
            Z, t = build_ls_1d(row)
            A += Z.T @ Z
            b += 0.5 * (Z.T @ t)   # 模型 xhat=2Zθ，目标整体除 4 后 b=(1/2)Z'x
        return A, b

    for rgb in images:
        _, cb, cr = rgb_to_ycbcr(rgb)
        for plane in (cb, cr):
            hx = np.moveaxis(plane, 1, -1)                 # H x W，行沿水平维
            A, b = add_rows(hx, A, b)                       # 水平维
            dh = downsample2(hx)                            # H x W/2
            mid = upsample_fir(dh, mid_c5)                  # 水平重建中间信号 H x W
            A, b = add_rows(np.moveaxis(mid, 0, -1), A, b)  # 竖直维（列转行）
    return A, b


def chroma_pipeline(rgb, c5, mode):
    """完整 4:2:0 色度链路：Y 原样保留，Cb/Cr 双向 2:1 下采样后按指定核重建。"""
    y, cb, cr = rgb_to_ycbcr(rgb)
    rec = []
    for plane in (cb, cr):
        d_h = downsample2(np.moveaxis(plane, 1, -1))   # 水平 W -> W/2
        d = np.moveaxis(downsample2(np.moveaxis(d_h, 0, -1)), -1, 0)  # 竖直 H -> H/2
        hat = upsample2d(d, c5, mode)                  # 回到 H x W
        rec.append(hat)
    cb_hat, cr_hat = rec
    rgb_hat = ycbcr_to_rgb(y, cb_hat, cr_hat)
    return rgb_hat, (cb, cb_hat, cr, cr_hat)


def color_errors(rgb_ref, rgb_hat, planes):
    """色差：每像素 RGB 欧氏距离的平均与最大；附 Cb/Cr 面 RMSE/最大。"""
    diff = rgb_hat - rgb_ref.astype(np.float64)
    per_pix = np.sqrt(np.sum(diff ** 2, axis=-1))
    cb, cb_hat, cr, cr_hat = planes
    return {
        "rgb_mean": float(per_pix.mean()),
        "rgb_max": float(per_pix.max()),
        "cb_rmse": float(np.sqrt(np.mean((cb_hat - cb) ** 2))),
        "cr_rmse": float(np.sqrt(np.mean((cr_hat - cr) ** 2))),
        "cb_max": float(np.max(np.abs(cb_hat - cb))),
        "cr_max": float(np.max(np.abs(cr_hat - cr))),
    }


def main():
    os.makedirs(RES_DIR, exist_ok=True)
    C = np.array([[1.0, 2.0, 2.0]])   # c0+2c1+2c2 = 1
    h = np.array([1.0])
    theta_bi = np.array([0.5, 0.25, 0.0])
    c5_bi = theta_to_c5(theta_bi)

    # 自检：FIR 路径在双线性系数下必须与专用双线性路径一致
    demo = np.random.default_rng(0).standard_normal(16)
    d = downsample2(demo)
    assert np.allclose(upsample_fir(d, c5_bi), upsample_bilinear(d), atol=TOL)

    report = {"tolerance": TOL, "tiers": {}}
    rows_csv = []

    for tier in TIERS:
        train, test = load_tier(tier)
        # 自洽迭代：用上一轮核生成竖直维中间信号，直到抽头收敛（首轮用双线性）。
        theta = theta_bi.copy()
        for _ in range(10):
            A, b = collect_normal_eq(train, theta_to_c5(theta))
            theta_new, lam = solve_cls(A, b, C, h)
            if np.max(np.abs(theta_new - theta)) < 1e-10:
                theta = theta_new
                break
            theta = theta_new
        c5 = theta_to_c5(theta)

        # 无约束最小二乘（仅用于对照展示，基于最后一轮统计）
        theta_unc = np.linalg.solve(A, b)

        # 约束逐条验证
        checks = {
            "sum_to_one": float(abs(c5.sum() - 1.0)),
            "sym_outer": float(abs(c5[0] - c5[4])),
            "sym_inner": float(abs(c5[1] - c5[3])),
            "center_theta": float(abs(theta[2] - c5[0])),
        }
        # 训练集上的目标值对比（证明 CLS 不劣于双线性）
        q_cls = float(0.5 * theta @ A @ theta - b @ theta)
        q_bi = float(0.5 * theta_bi @ A @ theta_bi - b @ theta_bi)

        tier_block = {
            "A": A.tolist(), "b": b.tolist(),
            "theta_unconstrained": theta_unc.tolist(),
            "theta_cls": theta.tolist(),
            "lambda": float(lam[0]),
            "c5_cls": c5.tolist(),
            "c5_bilinear": c5_bi.tolist(),
            "constraint_residual": checks,
            "train_objective_cls": q_cls,
            "train_objective_bilinear": q_bi,
            "test": [],
        }

        for idx, rgb in enumerate(test):
            for mode, c5_use in (("bilinear", c5_bi), ("cls", c5)):
                rgb_hat, planes = chroma_pipeline(rgb, c5_use, mode)
                m = color_errors(rgb, rgb_hat, planes)
                m.update({"image": idx, "method": mode})
                tier_block["test"].append(m)
                rows_csv.append({
                    "tier": tier, "image": idx, "method": mode,
                    **{k: v for k, v in m.items() if k not in ("image", "method")},
                })
        report["tiers"][tier] = tier_block

    # ---- 汇总：每档色差按"平均/最大"两列，双线性并列 ----
    summary = []
    for tier in TIERS:
        t = report["tiers"][tier]
        row = {"tier": tier,
               "c5_cls": t["c5_cls"], "c5_bilinear": t["c5_bilinear"],
               "lambda": t["lambda"],
               "constraint_residual": t["constraint_residual"],
               "train_objective_cls": t["train_objective_cls"],
               "train_objective_bilinear": t["train_objective_bilinear"]}
        for mode in ("bilinear", "cls"):
            ms = [m for m in t["test"] if m["method"] == mode]
            row[f"{mode}_rgb_mean_avg"] = float(np.mean([m["rgb_mean"] for m in ms]))
            row[f"{mode}_rgb_max_worst"] = float(np.max([m["rgb_max"] for m in ms]))
            row[f"{mode}_chroma_rmse_avg"] = float(np.mean(
                [0.5 * (m["cb_rmse"] + m["cr_rmse"]) for m in ms]))
        row["mean_gain_pct"] = 100.0 * (
            row["bilinear_rgb_mean_avg"] - row["cls_rgb_mean_avg"]) \
            / row["bilinear_rgb_mean_avg"]
        summary.append(row)
    report["summary"] = summary
    with open(os.path.join(RES_DIR, "report.json"), "w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    with open(os.path.join(RES_DIR, "per_image.csv"), "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(rows_csv[0].keys()))
        w.writeheader()
        w.writerows(rows_csv)
    with open(os.path.join(RES_DIR, "summary.csv"), "w", newline="", encoding="utf-8") as f:
        fields = ["tier", "c5_cls", "c5_bilinear", "lambda",
                  "bilinear_rgb_mean_avg", "cls_rgb_mean_avg", "mean_gain_pct",
                  "bilinear_rgb_max_worst", "cls_rgb_max_worst",
                  "bilinear_chroma_rmse_avg", "cls_chroma_rmse_avg",
                  "train_objective_cls", "train_objective_bilinear",
                  "constraint_residual"]
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        for r_ in summary:
            w.writerow({k: (json.dumps(r_[k], ensure_ascii=False)
                            if isinstance(r_[k], (list, dict)) else r_[k])
                        for k in fields})

    # 控制台汇总
    hdr = f"{'tier':6s} {'method':9s} {'RGB平均色差':>12s} {'RGB最大色差':>12s} {'色度RMSE均':>12s}"
    print("\n" + hdr + "\n" + "-" * len(hdr))
    for r_ in summary:
        print(f"{r_['tier']:6s} {'bilinear':9s} "
              f"{r_['bilinear_rgb_mean_avg']:12.4f} {r_['bilinear_rgb_max_worst']:12.4f} "
              f"{r_['bilinear_chroma_rmse_avg']:12.4f}")
        print(f"{'':6s} {'CLS核':9s} "
              f"{r_['cls_rgb_mean_avg']:12.4f} {r_['cls_rgb_max_worst']:12.4f} "
              f"{r_['cls_chroma_rmse_avg']:12.4f}   平均增益 {r_['mean_gain_pct']:+.4f}%")
    print("\n抽头向量与约束/训练目标见 results/summary.csv、results/report.json")


if __name__ == "__main__":
    main()
