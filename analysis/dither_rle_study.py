#!/usr/bin/env python3
"""抖动强度 vs RLE 体积 / 游程长度 / 噪声 —— 确定性可复现研究。

仓库内部 RLE（internal/codec/rle.go）：8 字节头 + 每段 (count,value) 两字节，
count 上限 255。本脚本用 numpy 精确复刻该字节公式，已在 64x64 与 600 像素样本上
与 Go 编解码器逐字节对齐（docs/dither-rle.md §0）。

量化：8 级 {0,36,73,109,146,182,219,255}，步长 q=255/7。阈值边界取相邻级中点。
抖动：RPDF，半宽 a=tau*q/2，抖动能量 E_d=a^2/3，抖动 RMS=sqrt(E_d)=a/sqrt(3)。

全部随机源用固定种子；同一脚本两次运行输出逐字节一致。只写 artifacts/ 下的产物。
"""
import json
import os
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.normpath(os.path.join(HERE, "..", "artifacts", "dither-rle"))
os.makedirs(OUT, exist_ok=True)

L = 8
Q = 255.0 / (L - 1)
LEVELS = np.round(np.linspace(0, 255, L)).astype(float)
BND = (LEVELS[:-1] + LEVELS[1:]) / 2.0


def quant(v):
    v = np.asarray(v, float)
    return LEVELS[np.clip(np.searchsorted(BND, v, side="right"), 0, L - 1)]


def boundary_dist(v):
    return float(np.min(np.abs(BND - v)))


def dither(v, tau, seed=7):
    if tau == 0:
        return quant(v)
    rng = np.random.default_rng(seed)
    d = (rng.random(v.shape) - 0.5) * tau * Q
    return quant(v.astype(float) + d)


def rle_stats(flat):
    """精确 RLE 字节模型：返回 (字节数, 游程数, 平均游程长度)。"""
    a = np.asarray(flat).ravel()
    if a.size == 0:
        return 8, 0, float("inf")
    ch = np.flatnonzero(np.diff(a) != 0) + 1
    starts = np.r_[0, ch]
    ends = np.r_[ch, a.size]
    lens = (ends - starts).astype(np.int64)
    recs = int(np.ceil(lens / 255.0).sum())
    return 8 + 2 * recs, int(lens.size), float(lens.mean())


def rms(a, ref):
    return float(np.sqrt(np.mean((a.astype(float) - ref.astype(float)) ** 2)))


def pgm_bytes(h, w):
    return len(f"P5\n{w} {h}\n255\n") + h * w


H, W = 128, 256
N = W * H
xs = np.arange(W) / (W - 1)
gradient = np.tile(np.round(255 * xs).astype(np.uint8), (H, 1))
flat130 = np.full((H, W), 130, np.uint8)   # δ=2.5
flat90 = np.full((H, W), 90, np.uint8)     # δ=1.0
# 跨边界 18 的近边界交替 17/20：τ=0 量化为 0/36 交替，L̄=1（每像素一段）
alt = np.tile(np.where(np.arange(W)[None, :] % 2 == 0, 17, 20).astype(np.uint8), (H, 1))


def mixed(alt_frac):
    img = np.tile(alt, (1, 1))
    img[:, : int(W * (1 - alt_frac))] = 130
    return img


CONTENTS = [
    ("gradient", gradient),
    ("flat130", flat130),
    ("flat90", flat90),
    ("alt17-20", alt),
    ("mixed50", mixed(0.5)),
]

TIERS = [0.10, 0.25, 0.50]


def sweep(img, taus):
    base = quant(img)
    b0, _, l0 = rle_stats(base)
    e0 = rms(base, img)
    rows = []
    for tau in taus:
        out = dither(img, tau)
        b, runs, lbar = rle_stats(out)
        e = rms(out, img)
        ed = (tau * Q / 2) ** 2 / 3.0
        rows.append(dict(
            tau=float(tau), bytes=b, runs=runs, Lbar=lbar,
            gain_pct=100.0 * (b0 - b) / b0,
            out_rms=e, excess_rms=float(np.sqrt(max(e * e - e0 * e0, 0.0))),
            dither_rms=float(np.sqrt(ed)), E_d=ed, Ed_x_Lbar=ed * lbar,
        ))
    return dict(base_bytes=b0, base_Lbar=l0, base_rms=e0, raw_bytes=pgm_bytes(H, W), rows=rows)


def flat_closed_form(v):
    """平场：p_switch=1/2·(1-δ²/(3E_d))，L̄=2/(1-δ²/(3E_d))，字节密度 C=2/L̄=1-δ²/(3E_d)。"""
    d = boundary_dist(v)
    return dict(delta=d, Ed_on=d * d / 3.0)


def cap_flat(v):
    """强度上限：抖动半宽 a 首次触及最近量化边界，即 a*=δ，τ*=2δ/q，E_d*=δ²/3。

    a<δ 时该灰度一次也不会翻转（平场始终是一段，L̄=∞），抖动不伤可压缩性；
    a>δ 起翻转概率 p=1/2·(1-δ²/(3E_d))>0，平场开始碎裂、L̄ 从 ∞ 掉向 2。
    故 a*=δ 是"不破坏可压缩区"的最大强度，也就是题目要找的强度上限。
    """
    d = boundary_dist(v)
    return dict(delta=d, a_star=d, tau_star=2 * d / Q, E_d_star=d * d / 3.0,
                dither_rms_star=d / np.sqrt(3),
                note="a<δ 平场零翻转；a=δ 时翻转概率为零，L̄=∞；a>δ 起 L̄ 由 ∞ 掉向 2")


def flat_validation(v, cap):
    """上限两侧的验证曲线：字节密度 C=B/N 与噪声。"""
    img = np.full((H, W), v, np.uint8)
    b0, _, l0 = rle_stats(quant(img))
    C0 = b0 / N
    ts = cap["tau_star"]
    pts = [max(0.0, ts - 0.15), ts, min(1.0, ts + 0.15), min(1.0, ts + 0.30)]
    out = []
    for tau in pts:
        b, runs, lbar = rle_stats(dither(img, tau))
        out.append(dict(tau=float(tau), E_d=float((tau * Q / 2) ** 2 / 3),
                        C=float(b / N), dC=float(b / N - C0), Lbar=lbar,
                        dither_rms=float(np.sqrt((tau * Q / 2) ** 2 / 3)),
                        out_rms=rms(dither(img, tau), img)))
    return out


def cap_track():
    """把上限公式 τ*=2δ/q 与混合内容净增益实测峰值对照（8 种子平均）。"""
    taus = np.round(np.linspace(0, 0.6, 121), 4)
    res = []
    for vflat in (130, 120, 100, 90):
        d0 = boundary_dist(vflat)
        pred = 2 * d0 / Q
        img = np.tile(alt, (1, 1))
        img[:, : W // 2] = vflat
        b0 = rle_stats(quant(img))[0]
        Gs = []
        for t in taus:
            g = [100.0 * (b0 - rle_stats(dither(img, t, s))[0]) / b0 for s in range(1, 9)]
            Gs.append(float(np.mean(g)))
        Gs = np.array(Gs)
        i = int(Gs.argmax())
        res.append(dict(vflat=vflat, delta=d0, tau_pred=float(pred),
                        tau_meas=float(taus[i]), gain_max=float(Gs[i])))
    return res


def product_form_test(img, taus):
    """检验题面的乘积形式 G=f(E_d·L̄)：给出各候选自变量与 G 的相关系数。"""
    b0, _, _ = rle_stats(quant(img))
    E, Lb, G = [], [], []
    for t in taus:
        bs, ls = [], []
        for s in range(1, 9):
            b, _, l = rle_stats(dither(img, t, s))
            bs.append(b)
            ls.append(l)
        b = float(np.mean(bs))
        l = float(np.mean(ls))
        E.append((t * Q / 2) ** 2 / 3)
        Lb.append(l)
        G.append(100.0 * (b0 - b) / b0)
    E, Lb, G = np.array(E), np.array(Lb), np.array(G)
    prod = E * Lb
    inv = 1.0 / Lb

    def cc(a):
        return float(np.corrcoef(a, G)[0, 1]) if np.std(a) > 0 and np.std(G) > 0 else float("nan")

    return dict(corr_EdLbar_G=cc(prod), corr_Ed_G=cc(E), corr_Lbar_G=cc(Lb),
                corr_invLbar_G=cc(inv),
                EdLbar_min=float(prod.min()), EdLbar_max=float(prod.max()))


def main():
    cfg = dict(L=L, q=Q, levels=LEVELS.astype(int).tolist(), N=N, pgm_bytes=pgm_bytes(H, W),
               full_dither_rms=float(Q / (2 * np.sqrt(3))))
    result = {"config": cfg, "contents": {}, "tiers": TIERS,
              "cap_track": cap_track(), "validation": {}}

    fine = np.round(np.linspace(0, 1, 41), 4)
    for name, img in CONTENTS:
        sw = sweep(img, [0.0, 0.10, 0.25, 0.50, 0.75, 1.0])
        if name in ("flat130", "flat90"):
            v = 130 if name == "flat130" else 90
            sw["flat_form"] = flat_closed_form(v)
            sw["cap"] = cap_flat(v)
            result["validation"][name] = flat_validation(v, cap_flat(v))
        # 全量曲线（种子平均，抑制 RPDF 采样抖动）
        full = []
        for tau in fine:
            vals = [rle_stats(dither(img, tau, s)) for s in range(1, 9)]
            b = float(np.mean([x[0] for x in vals]))
            lbar = float(np.mean([x[2] for x in vals]))
            full.append(dict(tau=float(tau), bytes=b, Lbar=lbar,
                             gain_pct=100.0 * (sw["base_bytes"] - b) / sw["base_bytes"]))
        sw["full_curve"] = full
        sw["product_form"] = product_form_test(img, fine)
        result["contents"][name] = sw

    with open(os.path.join(OUT, "results.json"), "w") as f:
        json.dump(result, f, indent=1)

    # ---------------- 打印 ----------------
    print(f"q={cfg['q']:.4f}  levels={cfg['levels']}  N={cfg['N']}  PGM={cfg['pgm_bytes']}  "
          f"full-dither RMS={cfg['full_dither_rms']:.4f}")
    for name, sw in result["contents"].items():
        print(f"\n### {name}  (τ=0: bytes={sw['base_bytes']} L̄={sw['base_Lbar']:.3f} "
              f"RMS={sw['base_rms']:.3f})")
        if sw.get("cap"):
            c = sw["cap"]
            print(f"   强度上限: δ={c['delta']:.2f} a*={c['a_star']:.3f} "
                  f"τ*=2δ/q={c['tau_star']:.4f} E_d*={c['E_d_star']:.3f} "
                  f"ditherRMS*={c['dither_rms_star']:.3f}")
        print(f"   {'tau':>5} {'bytes':>7} {'G%':>8} {'runs':>7} {'Lbar':>9} "
              f"{'dithRMS':>8} {'outRMS':>7} {'exRMS':>7} {'E_d·Lbar':>10}")
        for r in sw["rows"]:
            print(f"   {r['tau']:5.2f} {r['bytes']:7.0f} {r['gain_pct']:8.2f} {r['runs']:7d} "
                  f"{r['Lbar']:9.3f} {r['dither_rms']:8.3f} {r['out_rms']:7.3f} "
                  f"{r['excess_rms']:7.3f} {r['Ed_x_Lbar']:10.3f}")

    print("\n### 强度上限 τ*=2δ/q 与混合内容净增益峰值对照")
    print(f"{'flat_v':>7} {'δ':>6} {'τ*预测':>9} {'τ*实测峰值':>11} {'Gmax%':>8}")
    for r in result["cap_track"]:
        print(f"{r['vflat']:7d} {r['delta']:6.2f} {r['tau_pred']:9.4f} {r['tau_meas']:11.4f} {r['gain_max']:8.2f}")

    print("\n### 强度上限两侧验证曲线（平场）")
    for name, pts in result["validation"].items():
        print(f"-- {name} --")
        print(f"   {'side':>5} {'tau':>6} {'E_d':>8} {'C=B/N':>8} {'ΔC':>8} {'Lbar':>9} {'dithRMS':>8} {'outRMS':>7}")
        cap = result["contents"][name]["cap"]["tau_star"]
        for p in pts:
            side = "上限" if abs(p["tau"] - cap) < 1e-9 else ("下侧" if p["tau"] < cap else "上侧")
            print(f"   {side:>5} {p['tau']:6.3f} {p['E_d']:8.3f} {p['C']:8.4f} {p['dC']:8.4f} "
                  f"{p['Lbar']:9.3f} {p['dither_rms']:8.3f} {p['out_rms']:7.3f}")

    print("\n### 乘积形式/倒数形式的相关系数（G 的合法自变量是 1/L̄，不是 E_d·L̄）")
    print(f"{'content':10} {'corr(E_d·L̄,G)':>14} {'corr(E_d,G)':>12} {'corr(L̄,G)':>11} "
          f"{'corr(1/L̄,G)':>13} {'E_d·L̄ 范围':>18}")
    for name, sw in result["contents"].items():
        p = sw["product_form"]
        print(f"{name:10} {p['corr_EdLbar_G']:+14.4f} {p['corr_Ed_G']:+12.4f} "
              f"{p['corr_Lbar_G']:+11.4f} {p['corr_invLbar_G']:+13.4f} "
              f"[{p['EdLbar_min']:.1f}, {p['EdLbar_max']:.1f}]")

    print("\nwrote", os.path.join(OUT, "results.json"))


if __name__ == "__main__":
    main()
