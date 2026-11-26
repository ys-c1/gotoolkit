#!/usr/bin/env python3
"""Four-panel figure for the bilateral step-edge experiment.

(1) filtered profiles with half-height markers
(2) smoothing width and gradient retention vs radius
(3) leakage mass scaling L ~ kappa*rho*r
(4) 1/r tuning: fixed rho vs rho proportional to 1/r
Run with: python3 -I make_plots.py
"""
import csv
import json
import os

import sys

# Run under `python3 -I`: cwd and user site are excluded, so add the two
# trusted package locations explicitly. Nothing from cwd is importable.
for _p in ("/usr/lib/python3/dist-packages",
           "/home/ys/.local/lib/python3.14/site-packages"):
    if _p not in sys.path:
        sys.path.insert(0, _p)

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

OUT = os.environ.get("OUT_DIR", "/tmp/bilateral_step")
BLUE, ORANGE, AQUA = "#2a78d6", "#eb6834", "#1baf7a"
GRAY, INK, MUTED, GRID = "#898781", "#0b0b0b", "#52514e", "#e1e0d9"
SURFACE = "#fcfcfb"
plt.rcParams.update({
    "font.family": "sans-serif",
    "font.sans-serif": ["DejaVu Sans"],
    "font.size": 10,
    "text.color": INK,
    "axes.edgecolor": "#c3c2b7",
    "axes.labelcolor": MUTED,
    "xtick.color": MUTED,
    "ytick.color": MUTED,
    "axes.grid": True,
    "grid.color": GRID,
    "grid.linewidth": 0.8,
    "axes.spines.top": False,
    "axes.spines.right": False,
    "figure.facecolor": SURFACE,
    "axes.facecolor": SURFACE,
})

data = json.load(open(os.path.join(OUT, "metrics.json")))
with open(os.path.join(OUT, "profiles.csv")) as f:
    rows = list(csv.DictReader(f))
x = [int(r["x"]) for r in rows]
prof = {k: [float(r[k]) for r in rows] for k in rows[0]}
EDGE = data["constants"]["edge"]
RADII = [2, 4, 8]
colors = {2: BLUE, 4: ORANGE, 8: AQUA}

fig, axes = plt.subplots(2, 2, figsize=(12.5, 9))
zoom = slice(EDGE - 14, EDGE + 14)

# ---- (1) profiles with half-height markers
ax = axes[0][0]
ax.step(x[zoom], prof["ideal"][zoom], color=INK, lw=2,
        where="mid", label="ideal step")
for r in RADII:
    y = prof[f"bilateral_r{r}"]
    m = data["bilateral_fixed"][RADII.index(r)]
    ax.plot(x[zoom], y[zoom], color=colors[r], lw=1.8,
            label=f"bilateral r={r}")
    # half-height markers on both sides
    for xh, yh in ((EDGE - 0.5 - m["width_dark_px"],
                    64 + 128 * m["peak_leakage"] / 2),
                   (EDGE - 0.5 + m["width_bright_px"],
                    192 - 128 * m["peak_leakage"] / 2)):
        ax.plot(xh, yh, "o", ms=7, mfc="white", mec=colors[r], mew=1.8)
ax.annotate("", xy=(EDGE - 0.5 + data["bilateral_fixed"][2]["width_bright_px"],
                    108),
            xytext=(EDGE - 0.5 - data["bilateral_fixed"][2]["width_dark_px"],
                    108),
            arrowprops=dict(arrowstyle="<->", color=AQUA, lw=1.4))
ax.text(EDGE - 0.5, 112, "W (half-height width of the leakage bump)",
        ha="center", color=AQUA, fontsize=9)
ax.set_title("Filtered profiles at the step edge", color=INK, loc="left")
ax.set_xlabel("pixel x")
ax.set_ylabel("gray level")
ax.legend(frameon=False, fontsize=8.5, loc="upper left")

# ---- (2) width and retention vs radius
ax = axes[0][1]
rs = RADII
W = [m["width_total_px"] for m in data["bilateral_fixed"]]
eta = [m["eta"] for m in data["bilateral_fixed"]]
Wg = [m["width_total_px"] for m in data["gaussian"]]
etag = [m["eta"] for m in data["gaussian"]]
ax.plot(rs, W, "-o", color=BLUE, lw=2, ms=8, label="W bilateral (px)")
ax.plot(rs, Wg, "--s", color=BLUE, lw=1.6, ms=7, mfc="white",
        label="W plain Gaussian (px)")
ax.set_xlabel("kernel radius r (px)")
ax.set_ylabel("smoothing width W (px)", color=BLUE)
ax.set_xticks(rs)
ax2 = ax.twinx()
ax2.plot(rs, eta, "-o", color=ORANGE, lw=2, ms=8,
         label="eta bilateral")
ax2.plot(rs, etag, "--s", color=ORANGE, lw=1.6, ms=7, mfc="white",
         label="eta plain Gaussian")
ax2.set_ylabel("gradient retention ratio eta", color=ORANGE)
ax2.set_ylim(0, 1.02)
ax2.spines["top"].set_visible(False)
ax2.grid(False)
ax.set_title("Smoothing width grows with r; retention does not recover",
             color=INK, loc="left")
lines = ax.get_lines() + ax2.get_lines()
ax.legend(lines, [l.get_label() for l in lines], frameon=False, fontsize=8.5,
          loc="center right")

# ---- (3) leakage scaling
ax = axes[1][0]
L = [m["leakage_mass_px"] for m in data["bilateral_fixed"]]
rho = data["constants"]["rho_fixed"]
ax.plot(rs, L, "-o", color=AQUA, lw=2, ms=8, label="measured leakage L")
# asymptote L = kappa_G * rho * r through the largest-r measurement
k = L[-1] / (rho * rs[-1])
ax.plot(rs, [k * rho * r for r in rs], "--", color=MUTED, lw=1.6,
        label=f"L = kappa*rho*r, kappa={k:.3f} (Gaussian c=2)")
for r, lv in zip(rs, L):
    ax.annotate(f"{lv:.3f}", (r, lv), textcoords="offset points",
                xytext=(0, 9), ha="center", color=INK, fontsize=9)
ax.set_xlabel("kernel radius r (px)")
ax.set_xticks(rs)
ax.set_ylabel("total leakage mass L (px)")
ax.set_title("Edge leakage accumulates linearly with r at fixed range width",
             color=INK, loc="left")
ax.legend(frameon=False, fontsize=8.5)

# ---- (4) 1/r tuning
ax = axes[1][1]
Lt = [m["leakage_mass_px"] for m in data["bilateral_tuned"]]
etat = [m["eta"] for m in data["bilateral_tuned"]]
ax.plot(rs, L, "-o", color=BLUE, lw=2, ms=8,
        label="L, fixed rho (sigma_r=60)")
ax.plot(rs, Lt, "-o", color=AQUA, lw=2, ms=8,
        label="L, rho(r)=rho0*2/r (1/r tuning)")
ax.set_xlabel("kernel radius r (px)")
ax.set_xticks(rs)
ax.set_ylabel("total leakage mass L (px)", color=BLUE)
ax2 = ax.twinx()
ax2.plot(rs, eta, "--s", color=ORANGE, lw=1.6, ms=7,
         label="eta fixed rho")
ax2.plot(rs, etat, "--D", color=ORANGE, lw=1.6, ms=7, mfc="white",
         label="eta 1/r tuning")
ax2.set_ylabel("gradient retention eta", color=ORANGE)
ax2.set_ylim(0.8, 1.0)
ax2.spines["top"].set_visible(False)
ax2.grid(False)
ax.set_title("rho ~ 1/r holds total leakage constant as r grows",
             color=INK, loc="left")
lines = ax.get_lines() + ax2.get_lines()
ax.legend(lines, [l.get_label() for l in lines], frameon=False, fontsize=8.5)

fig.suptitle("Bilateral filtering of an ideal step edge (Delta=128, "
             "rho=%.4f, Gaussian spatial, c=2)" % rho,
             fontsize=12.5, color=INK, x=0.02, ha="left")
fig.tight_layout(rect=(0, 0, 1, 0.97))
path = os.path.join(OUT, "bilateral_step_panels.png")
fig.savefig(path, dpi=160)
print("wrote", path)
