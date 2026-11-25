"""全量实验驱动。

输出全部写入 artifacts/aqnoise/：
  source.png / recon_q*.png / error_q*.png   原图、重建图、放大误差图（每类区域各一份误差图）
  acf_q*.png                                  三类区域 ACF 曲线（主口径 + FFT 复核）
  scaling.png                                 L-步长关系、L/s 比值、d/s 归一化曲线
  summary_table.csv / scaling_fit.csv         汇总表与标度拟合参数
  results.npz                                 全部数值结果，供复核复算

口径（与 correlation.py 模块文档一致，任何数字只允许来自同一套定义）：
  分类：原图块活动度三分位，标签全程固定；
  ACF ：8x8 块内、去块均值、lag0..7，水平/竖直分别估计后平均；
  相关长度：包络 |r| 的 lag1..4 指数拟合 L_env，辅以 e-fold 插值 L_efold、积分长 L_int；
  不确定度：对块做 500 次 bootstrap 重采样，报告 2.5/97.5 百分位。
"""
from __future__ import annotations

import csv
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import codec_exp as ce
import correlation as cr

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "artifacts", "aqnoise")
QUALITIES = (80, 65, 50, 35, 20, 10)
ANCHOR_Q = 50
N_BOOT = 500
BOOT_SEED = 7717
RNG = np.random.default_rng(BOOT_SEED)


def blocks_of(field2d: np.ndarray, nb_h: int, nb_w: int) -> np.ndarray:
    return field2d.reshape(nb_h, 8, nb_w, 8).transpose(0, 2, 1, 3).reshape(-1, 8, 8)


def boot_acf_interval(resid_blocks: np.ndarray, idx: np.ndarray, n_boot: int = N_BOOT) -> dict:
    """对类内块重采样，得到 ACF 每条 lag 与 L_env/L_efold/L_int 的 95% 区间。"""
    sub = resid_blocks[idx]
    n = len(sub)
    boot_curves = []
    boot_lenv, boot_lefold, boot_lint = [], [], []
    for _ in range(n_boot):
        pick = RNG.integers(0, n, n)
        acf_b = cr.block_acf(sub[pick])
        cl_b = cr.correlation_length(acf_b)
        boot_curves.append(acf_b.mean(axis=0))
        boot_lenv.append(cl_b["L_env"])
        boot_lefold.append(cl_b["L_efold"])
        boot_lint.append(cl_b["L_int"])
    curves = np.array(boot_curves)
    return {
        "curve_lo": np.nanquantile(curves, 0.025, axis=0),
        "curve_hi": np.nanquantile(curves, 0.975, axis=0),
        "L_env_lo": np.nanquantile(boot_lenv, 0.025),
        "L_env_hi": np.nanquantile(boot_lenv, 0.975),
        "L_efold_lo": np.nanquantile(boot_lefold, 0.025),
        "L_efold_hi": np.nanquantile(boot_lefold, 0.975),
        "L_int_lo": np.nanquantile(boot_lint, 0.025),
        "L_int_hi": np.nanquantile(boot_lint, 0.975),
    }


def power_law_fit(x: np.ndarray, y: np.ndarray) -> tuple[float, float, float]:
    """log y = a + alpha log x 最小二乘，返回 (alpha, intercept, R^2)。"""
    m = np.isfinite(x) & np.isfinite(y) & (y > 0)
    lx, ly = np.log(x[m]), np.log(y[m])
    A = np.vstack([lx, np.ones_like(lx)]).T
    (alpha, intercept), *_ = np.linalg.lstsq(A, ly, rcond=None)
    pred = alpha * lx + intercept
    ss_res = np.sum((ly - pred) ** 2)
    ss_tot = np.sum((ly - ly.mean()) ** 2)
    r2 = 1.0 - ss_res / ss_tot if ss_tot > 0 else np.nan
    return float(alpha), float(intercept), float(r2)


def main():
    os.makedirs(OUT, exist_ok=True)
    img = ce.synthesize_test_image()
    h, w = img.shape
    nb_h, nb_w = h // 8, w // 8
    img_u8 = ce.to_uint8(img)

    base = ce.encode_decode(img, quality=ANCHOR_Q, aq_strength=1.0)
    labels = cr.classify_blocks(base["activity"])
    np.save(os.path.join(OUT, "_labels.npy"), labels.reshape(nb_h, nb_w))

    # 结果容器
    records = []
    fit_rows = []
    store = {"img": img, "labels": labels, "qualities": np.array(QUALITIES)}
    per_class_series = {c: {"s": [], "L_env": [], "L_efold": [], "L_int": [],
                            "r1": [], "acf": [], "acf_lo": [], "acf_hi": [],
                            "acf_fft": [], "rms": []} for c in range(3)}

    for q in QUALITIES:
        out = ce.encode_decode(img, quality=q, aq_strength=1.0)
        # 主分析残差：量化器浮点重建与原图之差（uint8 舍入前），
        # 这是量化噪声的本征形态；DC 常量偏置由 ACF 的块均值中心化消除。
        resid = out["recon"] - img
        # 链路末端残差：8bit 舍入后的实际像素差，用于误差图对照。
        resid_u8 = out["recon_u8"].astype(np.float64) - img_u8.astype(np.float64)
        rb = blocks_of(resid, nb_h, nb_w)

        store_q = {"steps": out["steps"], "mult": out["mult"], "activity": out["activity"],
                   "resid": resid, "resid_u8": resid_u8, "recon": out["recon_u8"]}
        store[f"q{q}"] = store_q

        for c in range(3):
            idx = labels == c
            acf = cr.block_acf(rb[idx])
            cl = cr.correlation_length(acf)
            ci = boot_acf_interval(rb, idx)
            mask = cr.class_block_mask(labels, c, nb_h, nb_w, h, w)
            acf_fft = cr.masked_acf_2d(resid, mask)
            s_eff = cr.effective_step(out["steps"][idx])
            rms = cr.block_rms_noise(rb[idx])
            ratio = cl["L_efold"] / s_eff

            rec = {
                "quality": q, "class": cr.CLASS_NAMES[c], "class_cn": cr.CLASS_CN[cr.CLASS_NAMES[c]],
                "n_blocks": int(idx.sum()),
                "aq_mult_mean": float(out["mult"][idx].mean()),
                "step_eff": s_eff,
                "rms_noise": rms,
                "r_lag1": float(cl["curve"][1]),
                "r_lag2": float(cl["curve"][2]),
                "r_lag4": float(cl["curve"][4]),
                "L_env": cl["L_env"], "L_env_lo": ci["L_env_lo"], "L_env_hi": ci["L_env_hi"],
                "L_efold": cl["L_efold"], "L_efold_lo": ci["L_efold_lo"], "L_efold_hi": ci["L_efold_hi"],
                "L_int": cl["L_int"], "L_int_lo": ci["L_int_lo"], "L_int_hi": ci["L_int_hi"],
                "L_over_s": ratio,
                "ac_zero_frac": cr.ac_zero_fraction(out["qcoeffs"][idx]),
                "spectral_centroid": cr.spectral_centroid(out["qcoeffs"][idx]),
            }
            records.append(rec)

            ser = per_class_series[c]
            ser["s"].append(s_eff)
            ser["L_env"].append(cl["L_env"])
            ser["L_efold"].append(cl["L_efold"])
            ser["L_int"].append(cl["L_int"])
            ser["r1"].append(cl["curve"][1])
            ser["acf"].append(acf.mean(axis=0))
            ser["acf_lo"].append(ci["curve_lo"])
            ser["acf_hi"].append(ci["curve_hi"])
            ser["acf_fft"].append(acf_fft.mean(axis=0))
            ser["rms"].append(rms)

    # 标度律拟合：L = C s^alpha（每类），分别对三个长度口径拟合
    for c in range(3):
        ser = per_class_series[c]
        s = np.array(ser["s"])
        for key in ("L_env", "L_efold", "L_int"):
            y = np.array(ser[key], dtype=float)
            alpha, intercept, r2 = power_law_fit(s, y)
            fit_rows.append({"class": cr.CLASS_NAMES[c],
                             "class_cn": cr.CLASS_CN[cr.CLASS_NAMES[c]],
                             "metric": key, "alpha": alpha,
                             "log_intercept": intercept, "R2": r2,
                             "n_points": int(np.isfinite(y).sum())})

    # ---- 落表 ----
    with open(os.path.join(OUT, "summary_table.csv"), "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(records[0].keys()))
        writer.writeheader()
        writer.writerows(records)
    with open(os.path.join(OUT, "scaling_fit.csv"), "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(fit_rows[0].keys()))
        writer.writeheader()
        writer.writerows(fit_rows)

    # 供绘图脚本使用
    np.savez(os.path.join(OUT, "results.npz"),
             store=np.array(store, dtype=object),
             series=np.array(per_class_series, dtype=object),
             records=np.array(records, dtype=object),
             fit_rows=np.array(fit_rows, dtype=object),
             allow_pickle=True)
    print("wrote", len(records), "rows to", OUT)
    for r in records:
        if r["quality"] == ANCHOR_Q:
            print(f"{r['class_cn']} s={r['step_eff']:7.2f} rms={r['rms_noise']:5.2f} "
                  f"r1={r['r_lag1']:.3f} L_efold={r['L_efold']:.3f} "
                  f"[{r['L_efold_lo']:.3f},{r['L_efold_hi']:.3f}] L/s={r['L_over_s']:.5f} "
                  f"ACzero={r['ac_zero_frac']:.3f} centroid={r['spectral_centroid']:.3f}")
    for fr in fit_rows:
        print(f"fit {fr['class_cn']:6s} {fr['metric']:8s} alpha={fr['alpha']:+.3f} R2={fr['R2']:.3f}")


if __name__ == "__main__":
    main()
