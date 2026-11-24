#!/usr/bin/env python3
"""Report figures (English labels: no CJK font available in this environment)."""
import json
import numpy as np
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

ART = "/home/ys/feedback/uidreczz28dS25gV8KV/artifacts"
res = json.load(open(f"{ART}/ef_results.json"))
z = np.load(f"{ART}/ef_spectra.npz")
k = z["k"]
freq = k / 256.0

colors = {"beta0": "#565656", "half": "#E69F00",
          "star": "#0072B2", "ds1": "#009E73"}
labels = {"beta0": "beta=0 (original path)",
          "half": "beta=0.5*beta*",
          "star": "beta=beta* (whitening)",
          "ds1": "beta=1.0 (1st-order Delta-Sigma)"}

# ---------- Figure 1: row-averaged PSD ----------
fig, ax = plt.subplots(figsize=(8.2, 4.8), dpi=150)
for key in ["beta0", "half", "star", "ds1"]:
    P = z[key][: len(k)]
    P = P / P.sum()
    ax.loglog(freq, P, lw=1.8, color=colors[key], label=labels[key])
lf_edge = 1 / 16
ax.axvspan(freq[0], lf_edge, color="#D55E00", alpha=0.08)
ax.axvline(lf_edge, color="#D55E00", lw=1.2, ls="--")
ax.text(lf_edge * 1.08, 2e-4, "LF band: first 1/8", color="#D55E00", fontsize=9)
ax.set_xlabel("Normalized frequency f (cycles/sample)")
ax.set_ylabel("Normalized PSD")
ax.set_title("Quantization-noise row PSD under error feedback (mean of 32 trials)")
ax.grid(True, which="both", lw=0.4, alpha=0.4)
ax.legend(frameon=False, fontsize=8.5)
fig.tight_layout()
fig.savefig(f"{ART}/fig_spectra.png")
plt.close(fig)

# ---------- Figure 2: LF fraction vs MSE ----------
rows = res["rows"]
names = [labels[r["key"]] for r in rows]
lf = [r["lf_fraction"] * 100 for r in rows]
lf_std = [r["lf_fraction_std"] * 100 for r in rows]
mse = [r["mse"] for r in rows]
xpos = np.arange(len(rows))

fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4.4), dpi=150)
bars1 = ax1.bar(xpos, lf, yerr=lf_std, capsize=3, width=0.62,
                color=[colors[r["key"]] for r in rows], edgecolor="white")
ax1.axhline(12.5, color="#D55E00", ls="--", lw=1.3)
ax1.text(3.45, 13.6, "white-noise 12.5%", color="#D55E00", fontsize=8.5, ha="right")
ax1.set_xticks(xpos)
ax1.set_xticklabels([f"b={r['beta']:.3f}" for r in rows], fontsize=9)
ax1.set_ylabel("LF energy fraction (%)")
ax1.set_title("First-1/8-band energy fraction (lower = less dirty look)")
ax1.grid(axis="y", lw=0.4, alpha=0.4)
for b, v in zip(bars1, lf):
    ax1.text(b.get_x() + b.get_width() / 2, v + 1.2, f"{v:.1f}",
             ha="center", fontsize=9)

bars2 = ax2.bar(xpos, mse, width=0.62,
                color=[colors[r["key"]] for r in rows], edgecolor="white")
ax2.set_xticks(xpos)
ax2.set_xticklabels([f"b={r['beta']:.3f}" for r in rows], fontsize=9)
ax2.set_ylabel("Quantization error MSE")
ax2.set_title("Total noise MSE (the cost)")
ax2.grid(axis="y", lw=0.4, alpha=0.4)
for b, v in zip(bars2, mse):
    ax2.text(b.get_x() + b.get_width() / 2, v + 0.7, f"{v:.1f}",
             ha="center", fontsize=9)
fig.tight_layout()
fig.savefig(f"{ART}/fig_metrics.png")
plt.close(fig)
print("ok")
