#!/usr/bin/env python3
"""误差反馈（error feedback）白化实验。

场景：量化器加了小幅 RPDF 抖动（幅度 0.25Δ），抖动不足以打散缓变区的量化
轮廓，残差谱偏红（低频能量集中），画面"看着像脏"。

推导主线（详见 report.md）：
  闭环定义（行方向一维，每行状态复位）：
    u[n] = x[n] + d[n] - β·e[n-1]
    y[n] = Q(u[n])                     量化级 Δ=16，饱和 [0,255]
    e[n] = y[n] - u[n]                 量化器误差
  输出总噪声 n[n] = y[n]-x[n] = d[n] + e[n] - β·e[n-1]
  z 域噪声传递函数（e 视为加性量化噪声）：
    N(z) = D(z) + (1 - β z⁻¹) E(z)
  基线（β=0）噪声一阶 AR 建模 n0[n]=ρ n0[n-1]+w[n]，其谱
    S0(ω) ∝ 1/|1-ρ e^-jω|²
  令反馈零点对消 AR 极点 => β* = ρ = R[1]/R[0]（Yule-Walker 闭式解）。

实测四档：原路径 β=0、半档 0.5β*、白化解 β*、一阶 ΔΣ β=1.0。
指标：低频能量占比（前 1/8 正频频带，去直流）与量化误差 MSE，32 次实现平均。
"""
import json
import numpy as np

rng = np.random.default_rng(20261125)

H, W = 256, 256
yy, xx = np.mgrid[0:H, 0:W]
x = (xx + yy) * (255.0 / (W + H - 2))
x += 8.0 * np.sin(2 * np.pi * xx / 96.0) * np.sin(2 * np.pi * yy / 128.0)
x = np.clip(x, 0, 255)

DELTA = 16.0
DITHER_AMP = 0.25 * DELTA / 2.0          # 不足幅 RPDF：±Δ/8
N_TRIALS = 32


def quantize(v):
    return np.clip(np.round(v / DELTA) * DELTA, 0.0, 255.0)


def run_feedback(x, d, beta):
    h, w = x.shape
    y = np.empty_like(x)
    e_quant = np.empty_like(x)
    e_prev = np.zeros(h)
    for j in range(w):
        u = x[:, j] + d[:, j] - beta * e_prev
        q = quantize(u)
        e = q - u
        y[:, j] = q
        e_quant[:, j] = e
        e_prev = e
    return y, y - x, e_quant


def spectrum_metrics(n):
    E = np.fft.rfft(n, axis=1)
    P = (E * np.conj(E)).real.mean(axis=0)
    P[0] = 0.0
    n_bins = len(P) - 1
    k_lf = n_bins // 8
    return float(P[1:k_lf + 1].sum() / P[1:].sum()), P


def autocorr_lag1(n):
    return float((n[:, :-1] * n[:, 1:]).mean() / (n * n).mean())


# ---------- 1. 基线 β=0：谱与自相关 ----------
base_lf, base_mse, base_r1, Ps = [], [], [], []
for t in range(N_TRIALS):
    d = rng.uniform(-DITHER_AMP, DITHER_AMP, size=x.shape)
    _, n0, _ = run_feedback(x, d, 0.0)
    lf, P = spectrum_metrics(n0)
    base_lf.append(lf)
    base_mse.append(float((n0 ** 2).mean()))
    base_r1.append(autocorr_lag1(n0))
    Ps.append(P)
P0 = np.mean(Ps, axis=0)
lf0, mse0, r1 = np.mean(base_lf), np.mean(base_mse), np.mean(base_r1)

# ---------- 2. 白化解 ----------
# 主解：Yule-Walker 一阶（传递函数零点对消 AR 极点），纯闭式
beta_star = float(r1)
# 交叉验证：在实测基线上网格搜索使谱变异系数 CV=std/mean 最小的 β
k = np.arange(1, len(P0))
cosw = np.cos(2 * np.pi * k / W)
Sk = P0[k]
grid = np.linspace(-0.2, 1.2, 2801)
def cv(beta):
    s = Sk * (1 + beta ** 2 - 2 * beta * cosw)
    return float(s.std() / s.mean())
beta_grid = float(grid[int(np.argmin([cv(b) for b in grid]))])

# ---------- 3. 四档实测 ----------
gears = [
    ("beta0",  "β=0（原路径，无反馈）", 0.0),
    ("half",   "β=0.5β*（半档白化）", 0.5 * beta_star),
    ("star",   "β=β*（传递函数白化解）", beta_star),
    ("ds1",    "β=1.0（一阶ΔΣ全高通）", 1.0),
]
rows, Ps_gears = [], {}
for key, name, beta in gears:
    lfs, mses, maxabs = [], [], []
    Ps = []
    for t in range(N_TRIALS):
        d = rng.uniform(-DITHER_AMP, DITHER_AMP, size=x.shape)
        y, n, eq = run_feedback(x, d, beta)
        lf, P = spectrum_metrics(n)
        lfs.append(lf)
        mses.append(float((n ** 2).mean()))
        maxabs.append(float(np.abs(n).max()))
        Ps.append(P)
    rows.append({
        "key": key, "name": name, "beta": float(beta),
        "lf_fraction": float(np.mean(lfs)),
        "lf_fraction_std": float(np.std(lfs)),
        "mse": float(np.mean(mses)),
        "mse_white_theory": float(mse0 * (1 + beta ** 2)),
        "max_abs_noise": float(np.max(maxabs)),
    })
    Ps_gears[key] = np.mean(Ps, axis=0)

# ---------- 4. 稳定性探针 ----------
# NTF=1-βz⁻¹ 是 FIR（全零点），|β|<=1 时零极点都在单位圆内/上，BIBO 稳定。
# β>1 时若把量化器当线性增益，等效回路极点 β 落在圆外；实测内部状态是否发散。
stab = []
for beta in [1.0, 1.1, 1.25, 1.5, 2.0]:
    d = rng.uniform(-DITHER_AMP, DITHER_AMP, size=x.shape)
    _, n, eq = run_feedback(x, d, beta)
    stab.append({
        "beta": beta,
        "max_abs_internal_e": float(np.abs(eq).max()),
        "max_abs_output_noise": float(np.abs(n).max()),
        "all_finite": bool(np.isfinite(eq).all() and np.isfinite(n).all()),
    })

out = {
    "delta": DELTA, "dither_amp": DITHER_AMP,
    "image": "256x256 smooth ramp+sine", "trials": N_TRIALS,
    "white_noise_lf_theory": 1 / 8,
    "baseline": {"lf_fraction": float(lf0), "mse": float(mse0),
                 "R1_over_R0": float(r1)},
    "beta_star_yule_walker": beta_star,
    "beta_grid_cv_check": beta_grid,
    "rows": rows,
    "stability_probe": stab,
}
with open("/home/ys/feedback/uidreczz28dS25gV8KV/artifacts/ef_results.json", "w") as f:
    json.dump(out, f, ensure_ascii=False, indent=2)
np.savez("/home/ys/feedback/uidreczz28dS25gV8KV/artifacts/ef_spectra.npz",
         k=k, **Ps_gears)
print(json.dumps(out, ensure_ascii=False, indent=2))
