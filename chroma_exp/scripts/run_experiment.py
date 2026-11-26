"""驱动实验（正确的采样相位模型）。

模型
----
带块相位 δ_b 的 4:2:0 面积降采样把每块内容平移 -δ_b；重建端在名义网格
上放大、不做相位补偿，故净漂移 r(x)=rec(x)-f(x)≈-δ_b·∇f(x) 被保留，
表现为整体偏色。参考 f 取「零相位降采样+名义放大」（重建端假定的理想
网格），与重建同核，隔离相位这一单一误差源。

实验 1：均匀相位扫描，证明漂移 ∝ 相位并定标比例系数
  每通道全场统一 δ∈{-0.5,-0.25,0.25,0.5}，块内平均漂移 r_c 对 δ 最小
  二乘拟合 k_c，与平均合并梯度 g_c=mean(gx+gy) 对照；理论 k_c=-g_c。

实验 2：块级相位场，证明相位可由块内色度梯度最小二乘估计并给误差界
  两套场：smooth（低通相关，物理可实现）、iid（每块独立，压力测试）。
  估计仅用可观测量——名义重建图自身梯度 ĝ 与残差 r，块内逐像素 LS：
      β̂=Σ r ĝ /Σ ĝ²，物理 δ̂=-β̂
  窗 W4=块内 4x4 色度点（全分辨率 8x8，win=1）；W8=2x2 块（16x16，win=2）。
  报告逐通道相位 RMSE/MAE/最大绝对误差（经验误差界）、可辨识块比例、
  漂移预测-实测斜率，以及闭环修正前后逐通道残余色差（取最大）。

输出 results/ 下 CSV 与 JSON，全部 float64。
"""
import csv
import json
import os
import sys
import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import chroma as ch

_HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(_HERE, "..", "data")
RES = os.path.join(_HERE, "..", "results")
GROUPS = ["A_linear", "B_smooth", "C_natural"]
CHANNELS = ["R", "G", "B"]
KERNELS = ["bilinear", "bicubic"]
WINDOWS = [(1, "W4_8x8"), (2, "W8_16x16")]
SWEEP = [-0.5, -0.25, 0.25, 0.5]
NB = ch.FULL // ch.BLK
GTHRESH = 1e-2


def load(name):
    return np.load(os.path.join(DATA, name + ".npy"))


def block_mean(a):
    return ch.block_aggregate(a, ch.BLK)


def nominal_reference(ref, kind):
    """零相位降采样 + 同核名义放大，作为重建端理想参考。"""
    return ch.reconstruct(ch.subsample_blockwise(ref, 0.0), kind)


# --------------------------------------------------------------------------
# 实验 1
# --------------------------------------------------------------------------
def run_uniform_sweep(img, kind, m=4):
    """均匀相位扫描，证明 e≈δ·g 并定标比例系数（梯度）。

    对每个 δ 在内部区域 [m:-m] 做逐像素最小二乘 e 对 g=(gx+gy) 回归，
    斜率 s(δ) 应等于 δ（这是「漂移与相位成正比、比例系数为梯度」的
    直接检验，不受边界钳制与梯度变号影响）。同时给：
      g_in   内部平均合并梯度（该通道的比例系数）；
      k_drift 内部平均漂移对 δ 的线性拟合斜率，应≈g_in。
    """
    rows = {}
    for ci, cname in enumerate(CHANNELS):
        ref = img[:, :, ci]
        nom = nominal_reference(ref, kind)
        gx, gy = ch.true_grad(nom)
        gfull = gx + gy
        g_in = gfull[m:-m, m:-m]
        g_flat = g_in.ravel()
        g_mean = float(g_in.mean())
        deltas, drifts, slopes = [], [], []
        for d in SWEEP:
            rec = ch.reconstruct(ch.subsample_blockwise(ref, d), kind)
            e = (rec - nom)[m:-m, m:-m]
            denom = float(g_flat @ g_flat)
            s = float(g_flat @ e.ravel()) / denom if denom > 1e-12 else float("nan")
            slopes.append(s)
            drifts.append(float(e.mean()))
            deltas.append(float(d))
        deltas = np.array(deltas)
        drifts = np.array(drifts)
        k_drift = float(np.dot(deltas, drifts) / np.dot(deltas, deltas))
        rows[cname] = {
            "g_interior": g_mean,
            "slope_per_delta": dict(zip([float(x) for x in deltas],
                                        [float(s) for s in slopes])),
            "slope_over_delta": [float(slopes[i] / deltas[i]) for i in range(len(deltas))],
            "k_drift": k_drift,
            "k_over_g": k_drift / g_mean if abs(g_mean) > 1e-3 else float("nan"),
        }
    return rows


# --------------------------------------------------------------------------
# 实验 2
# --------------------------------------------------------------------------
def smooth_phase_field(rng):
    raw = rng.standard_normal((NB, NB))
    ax = np.arange(NB) - NB / 2
    ker = np.exp(-(ax ** 2) / (2 * 2.5 ** 2))
    ker /= ker.sum()
    sm = np.apply_along_axis(lambda v: np.convolve(v, ker, mode="same"), 0, raw)
    sm = np.apply_along_axis(lambda v: np.convolve(v, ker, mode="same"), 1, sm)
    return sm / np.max(np.abs(sm)) * 0.5


def iid_phase_field(rng):
    return rng.uniform(-0.5, 0.5, size=(NB, NB))


def run_blockwise(img, kind, delta_field, win):
    est_stack = np.full((NB, NB, 3), np.nan)
    valid_each = np.zeros((NB, NB, 3), dtype=bool)
    resid_max, resid_rms = {}, {}
    corr_max, corr_rms = {}, {}
    per_chan = {}
    for ci, cname in enumerate(CHANNELS):
        ref = img[:, :, ci]
        nom = nominal_reference(ref, kind)
        rec = ch.reconstruct(ch.subsample_blockwise(ref, delta_field), kind)
        err = rec - nom
        # 可观测量：名义重建图自身梯度
        ogx, ogy = ch.true_grad(nom)
        d_hat, energy = ch.estimate_phase_pixels(
            err, ogx, ogy, win, ch.BLK, GTHRESH)
        est_stack[:, :, ci] = d_hat
        m = ~np.isnan(d_hat)
        valid_each[:, :, ci] = m
        rblk, gblk = block_mean(err), block_mean(ogx + ogy)
        if m.sum() > 0:
            phase_err = d_hat[m] - delta_field[m]
            pred_blk = d_hat[m] * gblk[m]      # 本约定 e≈+δ g
            meas_blk = rblk[m]
            denom = float(pred_blk @ pred_blk)
            slope = (float(pred_blk @ meas_blk) / denom
                     if denom > 1e-12 else float("nan"))
            # 两类误差界（逐块），仅在内部块检验（边界块受钳制支配）
            d2 = ch.directional_second_deriv(nom)
            gfull = ogx + ogy
            curv_bound = np.full((NB, NB), np.nan)
            struct_bound = np.full((NB, NB), np.nan)
            pad = win // 2
            for by in range(NB):
                for bx in range(NB):
                    r0 = max(0, by - pad) * ch.BLK
                    r1 = min(NB, by + pad + 1) * ch.BLK
                    c0 = max(0, bx - pad) * ch.BLK
                    c1 = min(NB, bx + pad + 1) * ch.BLK
                    gw = gfull[r0:r1, c0:c1]
                    ew = err[r0:r1, c0:c1]
                    npx = gw.size
                    ge = float(gw.ravel() @ gw.ravel())
                    curv_bound[by, bx] = ch.curvature_error_bound(
                        abs(float(delta_field[by, bx])),
                        d2[r0:r1, c0:c1], ge, npx)
                    # 结构界 |δ̂-δ|≤‖η‖₂/‖g‖₂；η=e-δ_true g（受控实验用真值）
                    if ge > 1e-12:
                        eta = ew.ravel() - float(delta_field[by, bx]) * gw.ravel()
                        struct_bound[by, bx] = float(
                            np.sqrt(eta @ eta) / np.sqrt(ge))
            actual = np.abs(d_hat - delta_field)
            interior = np.zeros((NB, NB), dtype=bool)
            interior[1:-1, 1:-1] = True
            mi = m & interior
            struct_holds = (float(np.nanmax(actual[mi]))
                            <= float(np.nanmax(struct_bound[mi])) + 1e-9)
            per_chan[cname] = {
                "phase_rmse": float(np.sqrt(np.mean(phase_err ** 2))),
                "phase_mae": float(np.mean(np.abs(phase_err))),
                "phase_maxabs": float(np.max(np.abs(phase_err))),
                "n_valid": int(m.sum()),
                "interior_phase_maxabs": float(np.nanmax(actual[mi])),
                "interior_curv_bound_max": float(np.nanmax(curv_bound[mi])),
                "interior_struct_bound_max": float(np.nanmax(struct_bound[mi])),
                "struct_bound_holds_interior": bool(struct_holds),
                "drift_pred_meas_slope": slope,
                "drift_block_rmse": float(np.sqrt(np.mean((meas_blk - pred_blk) ** 2))),
                "meas_drift_absmean": float(np.mean(np.abs(meas_blk))),
                "pred_drift_absmean": float(np.mean(np.abs(pred_blk))),
            }
        else:
            per_chan[cname] = {"phase_rmse": float("nan"), "n_valid": 0}
        resid_max[cname] = float(np.max(np.abs(err)))
        resid_rms[cname] = float(np.sqrt(np.mean(err ** 2)))
        # 闭环修正残余色差
        corrected = ch.apply_correction(rec, d_hat, kind)
        cerr = corrected - nom
        corr_max[cname] = float(np.max(np.abs(cerr)))
        corr_rms[cname] = float(np.sqrt(np.mean(cerr ** 2)))
    common = valid_each.all(axis=2)
    if common.sum() > 0:
        ph = est_stack[common, :] - delta_field[common, None]
        phase_rmse = float(np.sqrt(np.mean(ph ** 2)))
        phase_mae = float(np.mean(np.abs(ph)))
        phase_maxabs = float(np.max(np.abs(ph)))
    else:
        phase_rmse = phase_mae = phase_maxabs = float("nan")
    return {
        "phase_rmse": phase_rmse, "phase_mae": phase_mae,
        "phase_maxabs": phase_maxabs,
        "n_valid_common": int(common.sum()), "n_total": NB * NB,
        "resid_max": resid_max, "resid_rms": resid_rms,
        "corrected_resid_max": corr_max, "corrected_resid_rms": corr_rms,
        "per_channel": per_chan,
    }


def main():
    os.makedirs(RES, exist_ok=True)
    rng = np.random.default_rng(20261126)
    images = {n: load(n) for n in GROUPS}
    fields = {"smooth": smooth_phase_field(rng), "iid": iid_phase_field(rng)}
    for fn, fv in fields.items():
        np.save(os.path.join(RES, f"delta_{fn}.npy"), fv)

    sweep_rows, block_rows = [], []
    full = {"sweep": {}, "blockwise": {}}
    for name in GROUPS:
        img = images[name]
        full["sweep"][name] = {}
        full["blockwise"][name] = {}
        for kind in KERNELS:
            sw = run_uniform_sweep(img, kind)
            full["sweep"][name][kind] = sw
            for c in CHANNELS:
                for d in SWEEP:
                    sweep_rows.append([
                        name, kind, c, d, sw[c]["g_interior"],
                        sw[c]["slope_per_delta"][float(d)],
                        sw[c]["slope_over_delta"][SWEEP.index(d)],
                    ])
                sweep_rows.append([name, kind, c, "driftfit",
                                   sw[c]["g_interior"], sw[c]["k_drift"],
                                   sw[c]["k_over_g"]])
            full["blockwise"][name][kind] = {}
            for ftype, df in fields.items():
                for win, wlabel in WINDOWS:
                    br = run_blockwise(img, kind, df, win)
                    full["blockwise"][name][kind][f"{ftype}_{wlabel}"] = br
                    pc = br["per_channel"]
                    block_rows.append([
                        name, kind, ftype, wlabel,
                        br["phase_rmse"], br["phase_mae"], br["phase_maxabs"],
                        br["n_valid_common"], br["n_total"],
                        max(br["resid_max"].values()),
                        max(br["resid_rms"].values()),
                        max(br["corrected_resid_max"].values()),
                        max(br["corrected_resid_rms"].values()),
                        pc.get("R", {}).get("phase_rmse", float("nan")),
                        pc.get("G", {}).get("phase_rmse", float("nan")),
                        pc.get("B", {}).get("phase_rmse", float("nan")),
                        pc.get("R", {}).get("drift_pred_meas_slope", float("nan")),
                        pc.get("G", {}).get("drift_pred_meas_slope", float("nan")),
                        pc.get("B", {}).get("drift_pred_meas_slope", float("nan")),
                        pc.get("R", {}).get("interior_phase_maxabs", float("nan")),
                        pc.get("G", {}).get("interior_phase_maxabs", float("nan")),
                        pc.get("B", {}).get("interior_phase_maxabs", float("nan")),
                        pc.get("R", {}).get("interior_struct_bound_max", float("nan")),
                        pc.get("G", {}).get("interior_struct_bound_max", float("nan")),
                        pc.get("B", {}).get("interior_struct_bound_max", float("nan")),
                        all(pc.get(c, {}).get("struct_bound_holds_interior", False)
                            for c in CHANNELS),
                    ])

    with open(os.path.join(RES, "sweep.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["image", "kernel", "channel", "delta_or_metric",
                    "g_interior", "value", "ratio"])
        w.writerows(sweep_rows)
    with open(os.path.join(RES, "blockwise.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["image", "kernel", "field", "window",
                    "phase_rmse_px", "phase_mae_px", "phase_maxabs_px",
                    "n_valid_common", "n_total",
                    "resid_max_allchan", "resid_rms_maxchan",
                    "corr_resid_max_allchan", "corr_resid_rms_maxchan",
                    "phase_rmse_R", "phase_rmse_G", "phase_rmse_B",
                    "drift_slope_R", "drift_slope_G", "drift_slope_B",
                    "interior_phase_max_R", "interior_phase_max_G",
                    "interior_phase_max_B",
                    "interior_struct_bound_R", "interior_struct_bound_G",
                    "interior_struct_bound_B",
                    "struct_bound_holds_interior"])
        w.writerows(block_rows)
    with open(os.path.join(RES, "results.json"), "w") as f:
        json.dump(full, f, indent=2)
    print("wrote sweep.csv, blockwise.csv, results.json")


if __name__ == "__main__":
    main()
