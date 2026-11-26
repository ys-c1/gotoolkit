"""从 results.json 生成报告用的 Markdown 汇总表。

表 1：比例系数（漂移-相位），逐通道，两核三组。
表 2：块级相位估计 W4 vs W8，相位 RMSE/MAE/最大误差、可辨识块、结构界。
表 3：逐通道漂移量（实测块漂移绝对均值）与估计相位预测漂移之差。
表 4：修正前后残余色差（取最大）。
"""
import json
import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
RES = os.path.join(_HERE, "..", "results")
GROUPS = ["A_linear", "B_smooth", "C_natural"]
CH = ["R", "G", "B"]
KERNELS = ["bilinear", "bicubic"]


def main():
    d = json.load(open(os.path.join(RES, "results.json")))
    out = []

    # 表 1：比例系数
    out.append("## 表1 漂移-相位比例系数（均匀扫描，内部区域）\n")
    out.append("| 图像 | 核 | 通道 | 比例系数 g=mean(gx+gy) | 漂移拟合 k | k/g |")
    out.append("|---|---|---|---|---|---|")
    for g in GROUPS:
        for k in KERNELS:
            for c in CH:
                s = d["sweep"][g][k][c]
                out.append(f"| {g} | {k} | {c} | {s['g_interior']:.4f} | "
                           f"{s['k_drift']:.4f} | {s['k_over_g']:.4f} |")
    out.append("")

    # 表 2：W4 vs W8 相位估计（smooth 与 iid）
    for ftype in ["smooth", "iid"]:
        out.append(f"## 表2 相位估计误差（{ftype} 相位场，窗口 W4=8x8 vs W8=16x16）\n")
        out.append("| 图像 | 核 | 窗口 | 相位RMSE | 相位MAE | 最大绝对误差 | 内部实测最大 | 结构界(内部,最大) | 可辨识块 | 界成立 |")
        out.append("|---|---|---|---|---|---|---|---|---|---|")
        for g in GROUPS:
            for k in KERNELS:
                for win in ["W4_8x8", "W8_16x16"]:
                    b = d["blockwise"][g][k][f"{ftype}_{win}"]
                    sb = max(b["per_channel"][c]["interior_struct_bound_max"]
                             for c in CH if "interior_struct_bound_max" in b["per_channel"][c])
                    im = max(b["per_channel"][c].get("interior_phase_maxabs", float("nan"))
                             for c in CH)
                    holds = all(b["per_channel"][c].get("struct_bound_holds_interior", False)
                                for c in CH)
                    out.append(f"| {g} | {k} | {win} | {b['phase_rmse']:.4f} | "
                               f"{b['phase_mae']:.4f} | {b['phase_maxabs']:.4f} | "
                               f"{im:.4f} | {sb:.4f} | {b['n_valid_common']}/256 | "
                               f"{'是' if holds else '否'} |")
        out.append("")

    # 表 3：逐通道漂移量 vs 估计相位预测漂移之差（smooth W4，代表配置）
    out.append("## 表3 逐通道漂移量与估计相位预测漂移之差（smooth 场, W4, bilinear）\n")
    out.append("| 图像 | 通道 | 实测块漂移\\|·\\|均值 | 预测漂移\\|·\\|均值 | 块漂移RMSE差 | 漂移斜率(理想1) | 相位RMSE |")
    out.append("|---|---|---|---|---|---|---|")
    for g in GROUPS:
        b = d["blockwise"][g]["bilinear"]["smooth_W4_8x8"]
        for c in CH:
            pc = b["per_channel"][c]
            out.append(f"| {g} | {c} | {pc.get('meas_drift_absmean', float('nan')):.4f} | "
                       f"{pc.get('pred_drift_absmean', float('nan')):.4f} | "
                       f"{pc.get('drift_block_rmse', float('nan')):.4f} | "
                       f"{pc.get('drift_pred_meas_slope', float('nan')):.4f} | "
                       f"{pc.get('phase_rmse', float('nan')):.4f} |")
    out.append("")

    # 表 4：残余色差（取最大），修正前后
    out.append("## 表4 残余色差（三通道取最大，灰度级），修正前 vs 闭环修正后\n")
    out.append("| 图像 | 核 | 场 | 窗口 | 修正前max | 修正前RMS | 修正后max | 修正后RMS | RMS降幅 |")
    out.append("|---|---|---|---|---|---|---|---|---|")
    for g in GROUPS:
        for k in KERNELS:
            for ftype in ["smooth", "iid"]:
                for win in ["W4_8x8", "W8_16x16"]:
                    b = d["blockwise"][g][k][f"{ftype}_{win}"]
                    rm0 = max(b["resid_max"].values())
                    rr0 = max(b["resid_rms"].values())
                    rm1 = max(b["corrected_resid_max"].values())
                    rr1 = max(b["corrected_resid_rms"].values())
                    red = rr0 / rr1 if rr1 > 0 else float("inf")
                    out.append(f"| {g} | {k} | {ftype} | {win} | {rm0:.3f} | "
                               f"{rr0:.3f} | {rm1:.3f} | {rr1:.3f} | {red:.1f}x |")
    out.append("")

    text = "\n".join(out)
    with open(os.path.join(RES, "tables.md"), "w") as f:
        f.write(text)
    print(text)


if __name__ == "__main__":
    main()
