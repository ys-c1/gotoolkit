"""绘图：图像与三类区域误差图、ACF、标度律与归一化曲线。

全部图表遵循 dataviz 规范：类别色固定（蓝=平坦/橙=边缘/绿=纹理）、
直接标签不单独依赖颜色、单轴、细网格、表面留白。
"""
from __future__ import annotations

import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import viz_setup as vs  # noqa: E402,F401
import correlation as cr  # noqa: E402
from matplotlib.ticker import ScalarFormatter  # noqa: E402

plt = vs.plt
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "artifacts", "aqnoise")
ANCHOR_Q = 50


def readable_log_ticks(ax, axis: str, values: list[int]):
    """对数轴用普通整数刻度，避免科学计数法标签重叠；关闭次要刻度标签。"""
    getter = ax.set_xticks if axis == "x" else ax.set_yticks
    getter(values)
    fmt = ScalarFormatter()
    fmt.set_scientific(False)
    if axis == "x":
        ax.xaxis.set_major_formatter(fmt)
        ax.xaxis.set_minor_formatter(plt.NullFormatter())
    else:
        ax.yaxis.set_major_formatter(fmt)
        ax.yaxis.set_minor_formatter(plt.NullFormatter())


def save(fig, name: str):
    path = os.path.join(OUT, name)
    fig.savefig(path, bbox_inches="tight", facecolor=vs.SURFACE)
    plt.close(fig)
    print("saved", path)


def plot_images(store: dict, labels: np.ndarray, nb_h: int, nb_w: int):
    img = store["img"]
    q = store[f"q{ANCHOR_Q}"]
    resid = q["resid"]
    recon = q["recon"]

    # 类别图（按块活动度三分位）
    lab_img = np.kron(labels.reshape(nb_h, nb_w), np.ones((8, 8)))

    fig, axes = plt.subplots(1, 3, figsize=(13.5, 4.4))
    for ax in axes:
        ax.set_xticks([]); ax.set_yticks([]); ax.grid(False)
    axes[0].imshow(img, cmap="gray", vmin=0, vmax=255)
    axes[0].set_title("原图（512×512，灰度）")
    axes[1].imshow(recon, cmap="gray", vmin=0, vmax=255)
    axes[1].set_title(f"重建图（块DCT+AQ，Q={ANCHOR_Q}）")
    cmap_lab = plt.matplotlib.colors.ListedColormap([vs.COLORS["flat"], vs.COLORS["edge"], vs.COLORS["texture"]])
    axes[2].imshow(lab_img, cmap=cmap_lab, vmin=0, vmax=2)
    axes[2].set_title("区域分类（块活动度三分位）")
    handles = [plt.Line2D([0], [0], marker="s", linestyle="", color=vs.COLORS[n],
                          markersize=9, label=vs.CLASS_CN[n]) for n in cr.CLASS_NAMES]
    axes[2].legend(handles=handles, loc="lower right", frameon=True, fontsize=9)
    fig.tight_layout()
    save(fig, "01_image_recon_class.png")

    # 每类区域一张误差图：掩膜外灰底；色标按该类误差自适应（不跨类共用）。
    # 左=浮点量化残差（uint8 舍入前，本征噪声），中=32x32 放大（4x4 块方阵），
    # 右=8bit 链路末端残差。
    zoom_blocks = 4
    zoom_px = zoom_blocks * 8
    resid_u8 = q["resid_u8"]
    for c, name in enumerate(cr.CLASS_NAMES):
        mask = cr.class_block_mask(labels, c, nb_h, nb_w, img.shape[0], img.shape[1])
        err = np.where(mask, resid, np.nan)
        err_u8 = np.where(mask, resid_u8, np.nan)
        cls_vmax = max(1e-3, np.nanmax(np.abs(err)))
        fig, axes = plt.subplots(1, 3, figsize=(13.6, 4.4),
                                 gridspec_kw={"width_ratios": [1, 1, 1]})
        for ax in axes:
            ax.set_xticks([]); ax.set_yticks([]); ax.grid(False)
        axes[0].set_facecolor("#eceae3")
        im0 = axes[0].imshow(err, cmap="RdBu_r", vmin=-cls_vmax, vmax=cls_vmax)
        axes[0].set_title(f"{vs.CLASS_CN[name]} 浮点量化残差（舍入前，全图掩膜）")
        # 放大窗两级选择：优先完全由本类块组成的 4x4 块方阵；否则按类内占比+能量回退。
        lab = labels.reshape(nb_h, nb_w)
        block_energy = (resid.reshape(nb_h, 8, nb_w, 8).transpose(0, 2, 1, 3) ** 2)
        block_energy = block_energy.mean(axis=(2, 3))
        block_energy = np.where(lab == c, block_energy, np.nan)
        candidates = []
        for bi in range(nb_h - zoom_blocks):
            for bj in range(nb_w - zoom_blocks):
                win_lab = lab[bi:bi + zoom_blocks, bj:bj + zoom_blocks]
                frac = float((win_lab == c).mean())
                win_eng = block_energy[bi:bi + zoom_blocks, bj:bj + zoom_blocks]
                mean_eng = np.nanmean(win_eng) if np.isfinite(win_eng).any() else 0.0
                candidates.append((frac, mean_eng, bi, bj))
        candidates.sort(key=lambda t: (round(t[0], 6), t[1]), reverse=True)
        _, _, bi, bj = candidates[0]
        y0, x0 = bi * 8, bj * 8
        crop = err[y0:y0 + zoom_px, x0:x0 + zoom_px]
        axes[1].set_facecolor("#eceae3")
        im1 = axes[1].imshow(crop, cmap="RdBu_r", vmin=-cls_vmax, vmax=cls_vmax,
                             interpolation="nearest")
        axes[1].set_title(f"{vs.CLASS_CN[name]} 放大 {zoom_px}×{zoom_px}（块({bi},{bj})起）")
        vmax_u8 = max(1.0, np.nanmax(np.abs(err_u8)))
        axes[2].set_facecolor("#eceae3")
        axes[2].imshow(err_u8, cmap="RdBu_r", vmin=-vmax_u8, vmax=vmax_u8)
        axes[2].set_title(f"{vs.CLASS_CN[name]} 8bit 链路末端残差（舍入后）")
        cb = fig.colorbar(im1, ax=axes[:2], shrink=0.82, pad=0.02)
        cb.set_label("浮点残差（灰度级）")
        fig.tight_layout()
        save(fig, f"02_error_{name}.png")


def plot_acf(series: dict, anchor_idx: int):
    lags = np.arange(8)
    # 锚点档三类 ACF 总览（主口径 + FFT 复核 + bootstrap 带）
    fig, ax = plt.subplots(figsize=(8.6, 5.4))
    for c, name in enumerate(cr.CLASS_NAMES):
        color = vs.COLORS[name]
        r = np.array(series[c]["acf"][anchor_idx])
        lo = np.array(series[c]["acf_lo"][anchor_idx])
        hi = np.array(series[c]["acf_hi"][anchor_idx])
        rfft = np.array(series[c]["acf_fft"][anchor_idx])
        ax.fill_between(lags, lo, hi, color=color, alpha=0.15, linewidth=0)
        ax.plot(lags, r, "-o", color=color, lw=2, ms=6,
                label=f"{vs.CLASS_CN[name]} 块内ACF（95% bootstrap带）")
        ax.plot(lags, rfft, "--", color=color, lw=1.4, alpha=0.85,
                label=f"{vs.CLASS_CN[name]} FFT复核口径")
    ax.axhline(0, color=vs.INK_MUTED, lw=1)
    ax.axhline(np.exp(-1), color="#898781", lw=1, ls=":")
    ax.annotate("exp(-1)=0.368（e-fold 参考线）", xy=(5.0, 0.40), color=vs.INK_MUTED, fontsize=9)
    ax.set_xlabel("位移 lag（像素）")
    ax.set_ylabel("自相关系数 r(lag)")
    ax.set_title(f"三类区域压缩噪声自相关（Q={ANCHOR_Q}，估计窗口 lag0..7，拟合窗口 lag1..4）")
    ax.set_xticks(lags)
    ax.legend(fontsize=9, ncol=1, loc="upper right")
    fig.tight_layout()
    save(fig, "03_acf_classes.png")

    # 每类跨质量档的 ACF 演化（小 multiples）
    fig, axes = plt.subplots(1, 3, figsize=(13.2, 4.2), sharey=True)
    for c, name in enumerate(cr.CLASS_NAMES):
        ax = axes[c]
        cmap = plt.cm.Blues if name == "flat" else (plt.cm.Oranges if name == "edge" else plt.cm.Greens)
        nq = len(series[c]["acf"])
        for qi in range(nq):
            r = np.array(series[c]["acf"][qi])
            ax.plot(lags, r, "-o", lw=1.6, ms=4,
                    color=cmap(0.35 + 0.55 * qi / max(1, nq - 1)),
                    label=f"Q={_quality_of(qi)}")
        ax.axhline(0, color=vs.INK_MUTED, lw=1)
        ax.axhline(np.exp(-1), color="#898781", lw=1, ls=":")
        ax.set_title(vs.CLASS_CN[name], color=vs.COLORS[name])
        ax.set_xticks(lags)
        ax.set_xlabel("lag（像素）")
        if c == 0:
            ax.set_ylabel("r(lag)")
        ax.legend(fontsize=8, title="质量档", title_fontsize=8)
    fig.suptitle("各区域 ACF 随质量档（步长）的演化：步长越大，正相关拖尾越长", y=1.02)
    fig.tight_layout()
    save(fig, "04_acf_by_quality.png")


_QUALITY_LIST = (80, 65, 50, 35, 20, 10)


def _quality_of(qi: int) -> int:
    return _QUALITY_LIST[qi]


def plot_scaling(series: dict, fit_rows: list):
    fig, axes = plt.subplots(1, 3, figsize=(14.2, 4.6))

    # (1) L vs s 双对数
    ax = axes[0]
    for c, name in enumerate(cr.CLASS_NAMES):
        s = np.array(series[c]["s"])
        L = np.array(series[c]["L_efold"], dtype=float)
        color = vs.COLORS[name]
        ax.plot(s, L, "-o", color=color, lw=2, ms=7, label=vs.CLASS_CN[name])
        # 拟合线
        fit = next(f for f in fit_rows if f["class"] == name and f["metric"] == "L_efold")
        xx = np.linspace(s.min() * 0.9, s.max() * 1.05, 50)
        yy = np.exp(fit["log_intercept"]) * xx ** fit["alpha"]
        ax.plot(xx, yy, "--", color=color, lw=1.3, alpha=0.8,
                label=f"{vs.CLASS_CN[name]} 拟合 α={fit['alpha']:.2f}, R²={fit['R2']:.2f}")
    ax.set_xscale("log"); ax.set_yscale("log")
    readable_log_ticks(ax, "x", [20, 50, 100, 200, 400])
    readable_log_ticks(ax, "y", [1, 2, 3])
    ax.set_ylim(0.7, 3.4)
    ax.set_xlabel("区域有效量化步长 s_eff（灰度级）")
    ax.set_ylabel("相关长度 L_efold（像素）")
    ax.set_title("L–s 标度律：L = C·s^α（类内跨质量档）")
    ax.legend(fontsize=8.5)

    # (2) L/s 比值随 s
    ax = axes[1]
    for c, name in enumerate(cr.CLASS_NAMES):
        s = np.array(series[c]["s"])
        L = np.array(series[c]["L_efold"], dtype=float)
        ax.plot(s, L / s, "-o", color=vs.COLORS[name], lw=2, ms=7, label=vs.CLASS_CN[name])
    ax.set_xscale("log")
    readable_log_ticks(ax, "x", [20, 50, 100, 200, 400])
    ax.set_xlabel("s_eff（灰度级）")
    ax.set_ylabel("L_efold / s_eff（像素/灰度级）")
    ax.set_title("相关长度–步长比值：平坦区系统性最高")
    ax.legend(fontsize=9)

    # (3) 归一化曲线 r vs d/s（按题意的比值口径），锚点档
    ax = axes[2]
    for c, name in enumerate(cr.CLASS_NAMES):
        s = np.array(series[c]["s"])
        anchor = 2  # Q=50
        r = np.array(series[c]["acf"][anchor])
        ax.plot(np.arange(8) / s[anchor], r, "-o", color=vs.COLORS[name], lw=2, ms=6,
                label=f"{vs.CLASS_CN[name]}（s={s[anchor]:.1f}）")
    ax.axhline(0, color=vs.INK_MUTED, lw=1)
    ax.set_xlabel("归一化位移 d / s_eff（像素/灰度级）")
    ax.set_ylabel("r(lag)")
    ax.set_title(f"Q={ANCHOR_Q}：相关系数衰减 vs 距离/步长")
    ax.legend(fontsize=9)
    fig.tight_layout()
    save(fig, "05_scaling.png")


def plot_mechanism(records: list):
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(10.4, 4.3))
    for c, name in enumerate(cr.CLASS_NAMES):
        rows = sorted((r for r in records if r["class"] == name), key=lambda r: r["quality"])
        s = [r["step_eff"] for r in rows]
        ax1.plot(s, [r["ac_zero_frac"] for r in rows], "-o",
                 color=vs.COLORS[name], lw=2, ms=6, label=vs.CLASS_CN[name])
        ax2.plot(s, [r["spectral_centroid"] for r in rows], "-o",
                 color=vs.COLORS[name], lw=2, ms=6, label=vs.CLASS_CN[name])
    ax1.set_xscale("log"); ax2.set_xscale("log")
    readable_log_ticks(ax1, "x", [20, 50, 100, 200, 400])
    readable_log_ticks(ax2, "x", [20, 50, 100, 200, 400])
    ax1.set_xlabel("s_eff（灰度级）"); ax2.set_xlabel("s_eff（灰度级）")
    ax1.set_ylabel("AC 系数死区占比")
    ax2.set_ylabel("存活非零系数的频谱质心（频率半径）")
    ax1.set_title("机制 1：步长增大 → 更多模式落入死区")
    ax2.set_title("机制 2：存活模式向低频集中 → 空间相关变长")
    ax1.legend(fontsize=9); ax2.legend(fontsize=9)
    fig.tight_layout()
    save(fig, "06_mechanism.png")


def main():
    data = np.load(os.path.join(OUT, "results.npz"), allow_pickle=True)
    store = data["store"].item()
    series = data["series"].item()
    records = list(data["records"])
    fit_rows = list(data["fit_rows"])
    labels = store["labels"]
    nb_h, nb_w = (d // 8 for d in store["img"].shape)

    plot_images(store, labels, nb_h, nb_w)
    plot_acf(series, anchor_idx=2)
    plot_scaling(series, fit_rows)
    plot_mechanism(records)


if __name__ == "__main__":
    main()
