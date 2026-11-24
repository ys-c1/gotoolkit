"""五档桶数实测 + 预测曲线校验 + 基线对照。

输出:
  data/metrics.json      全部数值结果
  data/cells.csv         每格 p, v, w 与各档分配
  figures/*.png          预测曲线 / 覆盖率色差 / 基线对照 / 分配转移
  report/report.md       中文报告(由数值结果生成)
"""

from __future__ import annotations

import json
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from lab import allocate, gamut as gamut_mod, image_gen, kmeans

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA = os.path.join(ROOT, "data")
FIG = os.path.join(ROOT, "figures")

GRID_BITS = 2
KS = [64, 128, 256, 512, 1024]
MICRO_NS = [1, 2, 3, 4, 6, 8, 10, 12]
COVER_TAU = 16.0          # 覆盖率门槛: 逐点 ΔE <= 16
SEED_BASE = 20261125
ANCHOR_K = 64             # 闭式曲线只在最小档标定一次

# (格编号, 桶数) -> (中心, SSE), 微基准/实测/基线共享
_KM_CACHE: dict[tuple[int, int], tuple[np.ndarray, float]] = {}


def micro_benchmark(g: gamut_mod.Gamut) -> tuple[np.ndarray, np.ndarray, np.ndarray, np.ndarray]:
    """每格在 MICRO_NS 档桶数下实测 SSE, 逐格拟合 r_i (SSE_i ~ a_i·n^{-r_i})。

    返回 (a_i 单桶 SSE, r_i 逐格指数, 中位 r, 完整 SSE 表)。
    """
    c = g.c
    a = np.zeros(c)
    r = np.zeros(c)
    sse_table = np.zeros((c, len(MICRO_NS)))
    log_n = np.log(np.array(MICRO_NS, dtype=np.float64))
    for i, cell in enumerate(g.cells):
        for j, m in enumerate(MICRO_NS):
            kk = min(m, np.unique(cell.pixels, axis=0).shape[0])
            _, sse = kmeans.kmeans(
                cell.pixels,
                kk,
                seed=SEED_BASE + cell.idx * 101 + m,
                cache=_KM_CACHE,
                cache_key=(cell.idx, kk),
            )
            sse_table[i, j] = sse
        a[i] = sse_table[i, 0]
        valid = sse_table[i] > 0
        if valid.sum() >= 2:
            slope = np.polyfit(log_n[valid], np.log(sse_table[i, valid]), 1)[0]
            r[i] = -slope
        else:
            r[i] = 1.0  # 仅一个有效点时回退到 1/n
    return a, r, float(np.median(r)), sse_table


def measure(
    g: gamut_mod.Gamut, alloc: np.ndarray, rgb: np.ndarray
) -> dict:
    """实际量化并计算 SSE / RMS ΔE / 平均 ΔE / 覆盖率, 同时重建量化图。"""
    x = rgb.reshape(-1, 3).astype(np.float64)
    cell_side = g.cell_side
    cell_idx = np.clip((x // cell_side).astype(np.int64), 0, g.grid_levels - 1)
    cell_id = (
        cell_idx[:, 0] * g.grid_levels * g.grid_levels
        + cell_idx[:, 1] * g.grid_levels
        + cell_idx[:, 2]
    )
    id_to_pos = {c.idx: pos for pos, c in enumerate(g.cells)}

    quant = np.empty_like(x)
    sqerr = np.empty(x.shape[0], dtype=np.float64)
    palette_rows: list[tuple[int, np.ndarray]] = []
    for pos, cell in enumerate(g.cells):
        centers, _ = kmeans.kmeans(
            cell.pixels,
            int(alloc[pos]),
            seed=SEED_BASE + cell.idx * 7919,
            cache=_KM_CACHE,
            cache_key=(cell.idx, int(alloc[pos])),
        )
        palette_rows.append((cell.idx, centers))

    pal = dict(palette_rows)
    # 分块映射, 避免 (N,K) 大矩阵
    block = 8192
    for s in range(0, x.shape[0], block):
        e = min(x.shape[0], s + block)
        ids = cell_id[s:e]
        out = np.empty((e - s, 3), dtype=np.float64)
        err = np.empty(e - s, dtype=np.float64)
        for cid in np.unique(ids):
            mask = ids == cid
            centers = pal[int(cid)]
            q, terms = kmeans.quantize_with_palette(x[s:e][mask], centers)
            out[mask] = q
            err[mask] = terms
        quant[s:e] = out
        sqerr[s:e] = err

    sse = float(sqerr.sum())
    de = np.sqrt(sqerr)
    cover = float(np.mean(de <= COVER_TAU))
    return {
        "sse": sse,
        "rms_de": float(np.sqrt(np.mean(sqerr))),
        "mean_de": float(de.mean()),
        "p95_de": float(np.percentile(de, 95)),
        "coverage": cover,
        "quant": np.clip(np.rint(quant), 0, 255).astype(np.uint8).reshape(rgb.shape),
    }


def main() -> None:
    os.makedirs(DATA, exist_ok=True)
    os.makedirs(FIG, exist_ok=True)

    rgb = image_gen.make_image()
    image_gen.save_ppm(os.path.join(DATA, "source.ppm"), rgb)
    g = gamut_mod.Gamut(rgb, grid_bits=GRID_BITS)
    print(g.summary())

    w = g.weights()
    caps = np.minimum(g.counts(), KS[-1])

    # ---- 求解器交叉验证: 小规模上贪心 == DP ----
    small_c = min(8, g.c)
    small_k = 20
    n_g = allocate.greedy_allocate(w[:small_c], small_k)
    n_d = allocate.dp_allocate(w[:small_c], small_k)
    assert allocate.objective(w[:small_c], n_g) <= allocate.objective(
        w[:small_c], n_d
    ) + 1e-9
    dp_match = bool(np.allclose(
        allocate.objective(w[:small_c], n_g),
        allocate.objective(w[:small_c], n_d),
    ))
    print(f"[check] 贪心 vs DP 目标值一致: {dp_match}")

    # ---- 微基准标定 a_i 与逐格指数 r_i ----
    a, r_cell, r_hat, sse_table = micro_benchmark(g)
    print(f"[model] 逐格指数 r_i 中位={r_hat:.4f}, "
          f"范围=[{r_cell.min():.3f},{r_cell.max():.3f}]")

    rows = []
    naive_rows = []
    ponly_rows = []
    p_vec = np.array([c.p for c in g.cells], dtype=np.float64)
    prev_naive: np.ndarray | None = None
    for K in KS:
        alloc = allocate.greedy_allocate(w, K, caps)
        res = measure(g, alloc, rgb)
        # 预测: 实测 a_i、逐格 r_i, 分配方案决定每格 n_i
        pred_sse = float(np.sum(a / np.power(alloc.astype(np.float64), r_cell)))
        # 闭式 w/n 曲线(连续松弛), 只在 ANCHOR_K 标定常数 C
        cont = allocate.continuous_share(w, K)
        rows.append(
            {
                "K": K,
                "alloc": alloc,
                "sse": res["sse"],
                "pred_sse": pred_sse,
                "closed_sse": None,  # 后面统一填
                "rms_de": res["rms_de"],
                "mean_de": res["mean_de"],
                "p95_de": res["p95_de"],
                "coverage": res["coverage"],
                "cont_obj": float(np.sum(w / cont)),
            }
        )
        image_gen.save_ppm(os.path.join(DATA, f"weighted_K{K}.ppm"), res["quant"])

        # 对照 1: 只按像素占比 p 做贪心(忽略体积因子)
        a_p = allocate.greedy_allocate(p_vec, K, caps)
        pres = measure(g, a_p, rgb)
        ponly_rows.append(
            {
                "K": K,
                "sse": pres["sse"],
                "rms_de": pres["rms_de"],
                "mean_de": pres["mean_de"],
                "coverage": pres["coverage"],
                "gain_vs_p_pct": 100.0 * (pres["sse"] - res["sse"]) / pres["sse"],
            }
        )

        # 对照 2: Hamilton 按 w 比例(题目批评的"比例分桶"类做法)
        naive = allocate.hamilton_allocate(w, K, caps)
        nres = measure(g, naive, rgb)
        # Alabama 悖论检测: 哪些格在 K 翻倍后丢桶
        lost = []
        if prev_naive is not None:
            lost = [
                g.cells[i].idx
                for i in range(g.c)
                if naive[i] < prev_naive[i]
            ]
        naive_rows.append(
            {
                "K": K,
                "alloc": naive,
                "sse": nres["sse"],
                "rms_de": nres["rms_de"],
                "mean_de": nres["mean_de"],
                "coverage": nres["coverage"],
                "lost_cells": lost,
            }
        )
        prev_naive = naive

    # 闭式曲线 1: w/n 连续松弛 F(K)=C0·(Σw)^2/K, C0 只在 ANCHOR_K 标定一次
    anchor = next(r for r in rows if r["K"] == ANCHOR_K)
    c_const = anchor["sse"] / (float((w.sum() ** 2)) / ANCHOR_K)
    # 闭式曲线 2: 有效指数曲线 F(K)=C1/K^r_eff,
    # r_eff 由最小两档实测标定(不使用高档位信息), C1 在 ANCHOR_K 标定
    second = next(r for r in rows if r["K"] == KS[1])
    r_eff = float(
        np.log(anchor["sse"] / second["sse"]) / np.log(second["K"] / anchor["K"])
    )
    c1 = anchor["sse"] * ANCHOR_K ** r_eff
    for r in rows:
        r["closed_sse"] = c_const * float(w.sum() ** 2) / r["K"]
        r["curve_sse"] = c1 / r["K"] ** r_eff
        r["pred_resid_pct"] = 100.0 * (r["sse"] - r["pred_sse"]) / r["sse"]
        r["closed_resid_pct"] = 100.0 * (r["sse"] - r["closed_sse"]) / r["sse"]
        r["curve_resid_pct"] = 100.0 * (r["sse"] - r["curve_sse"]) / r["sse"]

    # 饱和点: 覆盖率首次 >= 99%
    sat = next((r["K"] for r in rows if r["coverage"] >= 0.99), None)

    # ---- cells.csv ----
    import csv

    with open(os.path.join(DATA, "cells.csv"), "w", newline="") as f:
        wr = csv.writer(f)
        wr.writerow(
            ["cell_idx", "n_pix", "p", "v", "w"]
            + [f"n_greedy_K{K}" for K in KS]
            + [f"n_hamilton_K{K}" for K in KS]
        )
        for i, cell in enumerate(g.cells):
            wr.writerow(
                [cell.idx, cell.n, f"{cell.p:.6f}", f"{cell.v:.2f}", f"{cell.w:.6f}"]
                + [int(rows[k]["alloc"][i]) for k in range(len(KS))]
                + [int(naive_rows[k]["alloc"][i]) for k in range(len(KS))]
            )

    metrics = {
        "grid_bits": GRID_BITS,
        "C_cells": g.c,
        "N_pixels": g.n_total,
        "K_levels": KS,
        "r_median": r_hat,
        "r_cell": r_cell.tolist(),
        "r_eff_calibrated": r_eff,
        "C1_calibrated": c1,
        "C_const_anchor": c_const,
        "anchor_K": ANCHOR_K,
        "cover_tau": COVER_TAU,
        "dp_match": dp_match,
        "saturation_K": sat,
        "weighted": [
            {k: v for k, v in r.items() if k != "alloc"} for r in rows
        ],
        "naive": [
            {k: v for k, v in r.items() if k != "alloc"} for r in naive_rows
        ],
        "p_only": ponly_rows,
        "allocations": {
            "greedy": {str(K): rows[k]["alloc"].tolist() for k, K in enumerate(KS)},
            "hamilton": {
                str(K): naive_rows[k]["alloc"].tolist() for k, K in enumerate(KS)
            },
        },
        "micro_sse": sse_table.tolist(),
        "micro_ns": MICRO_NS,
        "cell_stats": [
            {
                "idx": c.idx,
                "n": c.n,
                "p": c.p,
                "v": c.v,
                "w": c.w,
                "a_sse1": float(a[i]),
            }
            for i, c in enumerate(g.cells)
        ],
    }
    with open(os.path.join(DATA, "metrics.json"), "w") as f:
        json.dump(metrics, f, indent=2, ensure_ascii=False)
    print(f"[out] {os.path.join(DATA, 'metrics.json')}")

    # 控制台速览
    print("\nK     SSE(实测)   逐格预测  残差%   指数曲线  残差%   RMSΔE  覆盖率")
    for r in rows:
        print(
            f"{r['K']:5d} {r['sse']:11.1f} {r['pred_sse']:10.1f} "
            f"{r['pred_resid_pct']:6.2f} {r['curve_sse']:9.1f} "
            f"{r['curve_resid_pct']:6.2f} {r['rms_de']:6.2f} {r['coverage']*100:6.2f}"
        )
    print("\n基线(Hamilton) SSE 与丢桶格:")
    for r in naive_rows:
        print(
            f"K={r['K']:5d} SSE={r['sse']:11.1f} RMSΔE={r['rms_de']:6.2f} "
            f"覆盖率={r['coverage']*100:6.2f} 丢桶格={r['lost_cells'][:6]}"
        )
    print("\npv 加权相对纯 p 分配的 SSE 改善:")
    for r in ponly_rows:
        print(
            f"K={r['K']:5d} p-only SSE={r['sse']:11.1f} "
            f"pv 改善={r['gain_vs_p_pct']:6.2f}%  覆盖率 {r['coverage']*100:5.2f}"
        )


if __name__ == "__main__":
    main()
