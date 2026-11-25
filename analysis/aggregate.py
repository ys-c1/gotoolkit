"""归一化检验：相关系数衰减与「距离/步长」比的关系（诚实呈现部分塌缩）。

两种归一化：
  (A) 字面 x = d/s；(B) 有效 x = d/s^α，α 取该类 L–s 标度拟合值。
每个质量档单独连线（不池化），以暴露「模式死亡既拉伸 ACF、又改变其形状」：
汇总量 L 的标度律 R²≈0.99，但整条曲线的点对塌缩只是部分的。
"""
from __future__ import annotations

import csv
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import viz_setup as vs  # noqa: E402,F401
import correlation as cr  # noqa: E402

plt = vs.plt
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "artifacts", "aqnoise")
FIT_LAGS = (1, 2, 3, 4)
QUALITY_LIST = (80, 65, 50, 35, 20, 10)


def exp_fit_r2(x, y):
    m = y > 1e-9
    A = np.vstack([-x[m], np.ones(m.sum())]).T
    (beta, intercept), *_ = np.linalg.lstsq(A, np.log(y[m]), rcond=None)
    pred = -beta * x[m] + intercept
    ly = np.log(y[m])
    r2 = 1 - np.sum((ly - pred) ** 2) / np.sum((ly - ly.mean()) ** 2)
    return beta, intercept, r2


def panel(ax, series, c, name, xform, title_xlabel):
    color = vs.COLORS[name]
    cmap = plt.cm.Blues if name == "flat" else (plt.cm.Oranges if name == "edge" else plt.cm.Greens)
    xs_all, ys_all = [], []
    for qi, s in enumerate(series[c]["s"]):
        r = np.abs(np.array(series[c]["acf"][qi])[list(FIT_LAGS)])
        x = xform(np.array(FIT_LAGS, dtype=float), s)
        xs_all.append(x); ys_all.append(r)
        ax.plot(x, r, "-o", lw=1.6, ms=5,
                color=cmap(0.4 + 0.55 * qi / (len(QUALITY_LIST) - 1)),
                label=f"Q={QUALITY_LIST[qi]}")
    X, Y = np.concatenate(xs_all), np.concatenate(ys_all)
    beta, intercept, r2 = exp_fit_r2(X, Y)
    xx = np.linspace(0, X.max() * 1.05, 60)
    ax.plot(xx, np.exp(-beta * xx + intercept), "--", color="#333333", lw=1.7,
            label=f"池化指数拟合 R²={r2:.2f}")
    ax.axhline(0, color=vs.INK_MUTED, lw=1)
    ax.set_title(vs.CLASS_CN[name], color=color)
    ax.set_xlabel(title_xlabel)
    ax.legend(fontsize=7.5, ncol=2)
    return beta, intercept, r2


def main():
    data = np.load(os.path.join(OUT, "results.npz"), allow_pickle=True)
    series = data["series"].item()
    fit_rows = list(data["fit_rows"])
    rows = []

    # 图 A：字面 d/s
    fig, axes = plt.subplots(1, 3, figsize=(13.6, 4.4), sharey=True)
    for c, name in enumerate(cr.CLASS_NAMES):
        beta, intercept, r2 = panel(axes[c], series, c, name,
                                    lambda d, s: d / s, "归一化位移 d / s_eff")
        rows.append({"class": name, "beta_d_over_s": beta, "R2_d_over_s": r2})
        if c == 0:
            axes[c].set_ylabel("|r(lag)|")
    fig.suptitle("字面归一化 d/s：各质量档呈扇形展开，仅部分塌缩（α≈0.32 的亚线性后果）", y=1.03)
    fig.tight_layout()
    fig.savefig(os.path.join(OUT, "07_collapse.png"), bbox_inches="tight", facecolor=vs.SURFACE)
    plt.close(fig)

    # 图 B：有效 d/s^α
    fig, axes = plt.subplots(1, 3, figsize=(13.6, 4.4), sharey=True)
    for c, name in enumerate(cr.CLASS_NAMES):
        alpha = next(f for f in fit_rows if f["class"] == name
                     and f["metric"] == "L_efold")["alpha"]
        beta, intercept, r2 = panel(
            axes[c], series, c, name,
            lambda d, s, a=alpha: d / (s ** a), f"归一化位移 d / s^α（α={alpha:.2f}）")
        rows[c].update({"alpha": alpha, "beta_d_over_s_alpha": beta,
                        "R2_d_over_s_alpha": r2, "intercept_eff": intercept})
        if c == 0:
            axes[c].set_ylabel("|r(lag)|")
    fig.suptitle("有效归一化 d/s^α：塌缩改善但仍不完全——模式死亡同时改变 ACF 形状", y=1.03)
    fig.tight_layout()
    fig.savefig(os.path.join(OUT, "08_collapse_effective.png"), bbox_inches="tight",
                facecolor=vs.SURFACE)
    plt.close(fig)
    print("saved 07/08")

    with open(os.path.join(OUT, "normalized_fit.csv"), "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=["class", "alpha", "beta_d_over_s",
                                               "R2_d_over_s", "beta_d_over_s_alpha",
                                               "R2_d_over_s_alpha", "intercept_eff"])
        writer.writeheader()
        writer.writerows(rows)
    for r in rows:
        print(f"{r['class']:7s} α={r['alpha']:.2f} d/s R2={r['R2_d_over_s']:.3f} "
              f"d/s^α R2={r['R2_d_over_s_alpha']:.3f}")


if __name__ == "__main__":
    main()
