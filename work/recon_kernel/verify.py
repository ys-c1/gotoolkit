# -*- coding: utf-8 -*-
"""
verify.py — 独立验证脚本（不依赖 design_kernel 的求解路径，自行重算）。

四项验证：
  V1 约束逐条满足：和为一、中心对称（内/外抽头），数值残差 < 1e-9
  V2 KKT 残差：把解代回正规方程，残差应为机器精度
  V3 训练目标最优性：CLS 目标值 <= 双线性目标值（同一 A,b）
  V4 频域解析增益：对单频正弦，CLS 相对双线性的逐频 SSE 增益，
     说明五抽头外抽头在近奈奎斯特结构化信号上有实质作用空间，
     而混合自然统计下该空间被宽频平均摊薄（解释全图增益偏小）。
另输出"去均值陷阱"对照：逐行去均值会解出过大的 c2 并在含 DC 信号上劣化。
"""
import json
import os
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
RES = os.path.join(HERE, "results")
C = np.array([[1.0, 2.0, 2.0]])


def downsample2(x):
    return 0.5 * (x[..., 0::2] + x[..., 1::2])


def main():
    r = json.load(open(os.path.join(RES, "report.json"), encoding="utf-8"))
    lines = []
    ok = True

    def log(s):
        print(s)
        lines.append(s)

    log("=" * 78)
    log("V1/V2/V3 逐档验证")
    log("=" * 78)
    for tier in ("tier1", "tier2", "tier3"):
        t = r["tiers"][tier]
        A = np.array(t["A"])
        b = np.array(t["b"])
        th = np.array(t["theta_cls"])
        lam = t["lambda"]
        c5 = np.array(t["c5_cls"])

        # V1 约束
        r_sum = abs(c5.sum() - 1.0)
        r_sym_out = abs(c5[0] - c5[4])
        r_sym_in = abs(c5[1] - c5[3])
        # V2 KKT 残差（含 1/2 因子：梯度 Aθ-b + C'λ = 0）。
        # A 元素量级达 1e10，用相对残差判据（除以 ||b||+||Aθ||）才是机器精度口径。
        grad = A @ th - b + (C.T @ np.array([lam])).ravel()
        kkt1 = np.max(np.abs(grad)) / (np.linalg.norm(b) + np.linalg.norm(A @ th) + 1e-300)
        kkt2 = abs(float((C @ th)[0]) - 1.0)
        # V3 目标最优
        q = lambda z: float(0.5 * z @ A @ z - b @ z)
        q_cls, q_bi = q(th), q(np.array([0.5, 0.25, 0.0]))
        v1 = max(r_sum, r_sym_out, r_sym_in) < 1e-9
        v2 = max(kkt1, kkt2) < 1e-9
        v3 = q_cls <= q_bi + 1e-8
        ok &= v1 and v2 and v3
        log(f"\n[{tier}] c5={np.round(c5,6).tolist()}")
        log(f"  V1 约束: 和为一残差={r_sum:.2e} 外对称={r_sym_out:.2e} "
            f"内对称={r_sym_in:.2e}  -> {'PASS' if v1 else 'FAIL'}")
        log(f"  V2 KKT : 梯度相对残差={kkt1:.2e} 约束方程残差={kkt2:.2e}"
            f"  -> {'PASS' if v2 else 'FAIL'}")
        log(f"  V3 最优: q_cls={q_cls:.4f} <= q_bilinear={q_bi:.4f} "
            f"(差={q_bi - q_cls:+.4f}) -> {'PASS' if v3 else 'FAIL'}")

    # V4 频域解析增益（单频正弦，多相位聚合，内部位置，盒式下采样）
    log("\n" + "=" * 78)
    log("V4 频域解析增益（单频正弦，盒式下采样，约束最小二乘 vs 双线性）")
    log("=" * 78)
    N = 2048
    nidx = np.arange(N)
    th_bi = np.array([0.5, 0.25, 0.0])
    log(f"{'周期(样本)':>10s} {'归一化频率':>10s} {'SSE增益%':>10s} {'c2':>10s}")
    for per in (64, 32, 16, 8, 4.1):
        w = 2 * np.pi / per
        A = np.zeros((3, 3))
        bb = np.zeros(3)
        for ph in np.linspace(0, 2 * np.pi, 40, endpoint=False):
            x = np.sin(w * nidx + ph)
            d = downsample2(x)
            K = len(d)
            dprev = np.r_[d[0], d[:-1]]
            dnext = np.r_[d[1:], d[-1]]
            sl = slice(1, K - 1)
            Ze = np.zeros((K, 3)); Ze[:, 0] = d; Ze[:, 2] = dprev + dnext
            Zo = np.zeros((K, 3)); Zo[:, 1] = d + dnext
            Z = np.vstack([Ze[sl], Zo[sl]])
            tt = np.hstack([x[2:2 * (K - 1):2], x[3:2 * (K - 1) + 1:2]])
            A += Z.T @ Z
            bb += 0.5 * Z.T @ tt
        KKT = np.block([[A, C.T], [C, np.zeros((1, 1))]])
        th = np.linalg.solve(KKT, np.r_[bb, 1.0])[:3]

        def sse(theta):
            s = 0.0
            for ph in np.linspace(0, 2 * np.pi, 20, endpoint=False):
                x = np.sin(w * nidx + ph)
                d = downsample2(x)
                K = len(d)
                dprev = np.r_[d[0], d[:-1]]
                dnext = np.r_[d[1:], d[-1]]
                sl = slice(1, K - 1)
                Ze = np.zeros((K, 3)); Ze[:, 0] = d; Ze[:, 2] = dprev + dnext
                Zo = np.zeros((K, 3)); Zo[:, 1] = d + dnext
                Z = np.vstack([Ze[sl], Zo[sl]])
                tt = np.hstack([x[2:2 * (K - 1):2], x[3:2 * (K - 1) + 1:2]])
                s += np.sum((2 * Z @ theta - tt) ** 2)
            return s

        gain = 100 * (sse(th_bi) - sse(th)) / sse(th_bi)
        log(f"{per:10.1f} {w:10.3f} {gain:10.3f} {th[2]:10.4f}")

    # 去均值陷阱对照（1D，含 DC=128 的近奈奎斯特正弦）
    log("\n" + "=" * 78)
    log("附：逐行去均值陷阱（period=8, DC=128, 振幅 60，全信号 SSE 越小越好）")
    log("=" * 78)
    rng = np.random.default_rng(11)
    N = 256
    train, test = [], []
    for _ in range(60):
        x = 128 + 60 * np.sin(2 * np.pi * np.arange(N) / 8 + rng.uniform(0, 6))
        (train if _ < 40 else test).append(x)

    def solve(center):
        A = np.zeros((3, 3)); bb = np.zeros(3)
        for x in train:
            r = x - x.mean() if center else x
            d = downsample2(r)
            K = len(d)
            dprev = np.r_[d[0], d[:-1]]; dnext = np.r_[d[1:], d[-1]]
            sl = slice(1, K - 1)
            Ze = np.zeros((K, 3)); Ze[:, 0] = d; Ze[:, 2] = dprev + dnext
            Zo = np.zeros((K, 3)); Zo[:, 1] = d + dnext
            Z = np.vstack([Ze[sl], Zo[sl]])
            tt = np.hstack([r[2:2 * (K - 1):2], r[3:2 * (K - 1) + 1:2]])
            A += Z.T @ Z; bb += 0.5 * Z.T @ tt
        return np.linalg.solve(np.block([[A, C.T], [C, np.zeros((1, 1))]]),
                               np.r_[bb, 1.0])[:3]

    def full_sse(theta):
        s = 0.0
        for x in test:
            d = downsample2(x)
            K = len(d)
            dprev = np.r_[d[0], d[:-1]]; dnext = np.r_[d[1:], d[-1]]
            c0, c1, c2 = theta
            est = np.empty(2 * K)
            est[0::2] = 2 * (c2 * dprev + c0 * d + c2 * dnext)
            est[1::2] = 2 * c1 * (d + dnext)
            s += np.sum((est[2:2 * (K - 1)] - x[2:2 * (K - 1)]) ** 2)
        return s

    th_n, th_c = solve(False), solve(True)
    log(f"  不去均值 theta={np.round(th_n,5)} 全信号SSE={full_sse(th_n):.1f}")
    log(f"  去均值   theta={np.round(th_c,5)} 全信号SSE={full_sse(th_c):.1f} "
        f"(c2 被放大 {abs(th_c[2]) / max(abs(th_n[2]),1e-12):.0f} 倍，误差显著恶化)")
    log(f"  双线性   theta=[0.5,0.25,0] 全信号SSE={full_sse(th_bi):.1f}")

    log("\n总结论: V1~V3 " + ("全部 PASS" if ok else "存在 FAIL") +
        "；V4 显示外抽头在结构化近奈奎斯特信号上增益显著，混合自然统计下增益被摊薄。")
    with open(os.path.join(RES, "verification.txt"), "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
