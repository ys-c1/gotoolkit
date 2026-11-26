#!/usr/bin/env python3
"""Analytic predictions vs measured finite-kernel values (clean version).

Dark pixel at distance d>=0 left of the edge (d=0 = edge pixel). Window
offsets -r..r split at index j=r (offset 0):
  same side (dark, range weight 1): offsets -r .. -d-1, spatial mass a_d
  cross side (light, range weight rho): offsets -d .. r, spatial mass b_d
Filtered dark leakage normalized by Delta:
  l(d) = rho * b_d / (a_d + rho * b_d)
Bright side is the mirror image. Run with: python3 -I theory_check.py
"""
import json
import os

import numpy as np

OUT = os.environ.get("OUT_DIR", "/tmp/bilateral_step")
data = json.load(open(os.path.join(OUT, "metrics.json")))
C = data["constants"]["c"]
RADII = [2, 4, 8]


def weights(r, gaussian):
    if gaussian:
        sigma_s = r / C
        k = np.arange(-r, r + 1, dtype=np.float64)
        return np.exp(-(k ** 2) / (2.0 * sigma_s ** 2))
    return np.ones(2 * r + 1)


def masses(ws, r, d):
    """Spatial masses for a dark pixel at distance d (edge pixel is d=0).

    Index j maps to offset (j-r). The center offset 0 belongs to the pixel's
    own (dark) side. Windowed image indices run EDGE-1-d-r .. EDGE-1-d+r;
    a windowed index is bright once (EDGE-1-d)+offset >= EDGE, i.e.
    offset >= d+1. Hence:
      same  side: offsets -r .. d   -> indices 0 .. r+d
      cross side: offsets d+1 .. r  -> indices r+d+1 .. 2r
    """
    a = float(ws[: r + d + 1].sum())
    b = float(ws[r + d + 1:].sum())
    return a, b


def predict(r, rho, gaussian):
    ws = weights(r, gaussian)
    a0, b0 = masses(ws, r, 0)
    peak = rho * b0 / (a0 + rho * b0)
    eta = 1.0 - 2.0 * peak
    leak_side = 0.0
    for d in range(0, r + 1):  # d=0 edge pixel included; l(r)=0 (no cross mass)
        a, b = masses(ws, r, d)
        leak_side += rho * b / (a + rho * b)
    leakage = 2.0 * leak_side
    target, prev_d, prev_v, half = peak / 2.0, 0, peak, None
    for d in range(1, r + 1):
        a, b = masses(ws, r, d)
        v = rho * b / (a + rho * b)
        if v <= target:
            half = prev_d + (prev_v - target) / (prev_v - v)
            break
        prev_d, prev_v = d, v
    if half is None:
        half = float(r)
    width = 2.0 * (0.5 + half)
    return {"peak": peak, "eta": eta, "leakage": leakage, "width": width}


for name, gaussian in [("box-spatial exact", False),
                       ("Gaussian-spatial model", True)]:
    print(f"=== {name} vs measurements (fixed rho) ===")
    print(f"{'r':>2} {'rho':>8} | {'eta pred/meas':>16} "
          f"{'L pred/meas':>16} {'W pred/meas':>16}")
    for m in data["bilateral_fixed"]:
        p = predict(m["r"], m["rho"], gaussian)
        print(f"{m['r']:>2} {m['rho']:>8.4f} | "
              f"{p['eta']:>7.4f}/{m['eta']:.4f} "
              f"{p['leakage']:>8.3f}/{m['leakage_mass_px']:.3f} "
              f"{p['width']:>8.3f}/{m['width_total_px']:.3f}")
    print()

print("=== L/(rho*r) tends to a kernel-shape constant kappa ===")
for gaussian, tag in [(False, "box "), (True, "gauss")]:
    ks = [predict(r, data["constants"]["rho_fixed"], gaussian)["leakage"]
          / (data["constants"]["rho_fixed"] * r) for r in RADII]
    print(f"{tag} kappa: {[f'{v:.4f}' for v in ks]}")
ks = [x["leakage_mass_px"] / (x["rho"] * x["r"])
      for x in data["bilateral_fixed"]]
print(f"measured kappa: {[f'{v:.4f}' for v in ks]}")

print("\n=== width linearity W = alpha*r + beta (px) ===")
rs = np.array(RADII, float)
for key, label in [("bilateral_fixed", "bilateral (measured)"),
                   ("gaussian", "plain Gaussian (measured)")]:
    w = np.array([x["width_total_px"] for x in data[key]])
    a, b = np.polyfit(rs, w, 1)
    print(f"{label:<26} W = {a:.3f}*r + {b:.3f}")

print("\n=== 1/r tuning: rho*r held constant at "
      f"{data['constants']['rho_r_product_target']:.4f} ===")
for x in data["bilateral_tuned"]:
    print(f"r={x['r']} rho={x['rho']:.5f} sigma_r={x['sigma_r']:.2f} "
          f"L={x['leakage_mass_px']:.3f} eta={x['eta']:.4f}")

print("\n=== continuum asymptotics (r -> infinity, rho fixed, small) ===")
# box: l(u) ~ rho*(1-u)/(1+u), u=d/r
kappa_box = 2 * (2 * np.log(2) - 1)
print(f"box:    kappa_B={kappa_box:.4f} (box-model r=8: "
      f"{predict(8, data['constants']['rho_fixed'], False)['leakage'] / (data['constants']['rho_fixed'] * 8):.4f})"
      f"  u*=1/3  W slope=2/3={2/3:.4f}")
# gaussian c=2: ratio of tail/head truncated-normal masses
u = np.linspace(0, 1, 400001)
du = u[1] - u[0]
fv = np.exp(-(C ** 2) * u ** 2 / 2)
F = np.concatenate([[0], np.cumsum((fv[:-1] + fv[1:]) / 2) * du])
imid = F[-1]
ratio = (imid - F) / (imid + F)
i = np.argmax(ratio <= 0.5)
u_star = u[i - 1] + (0.5 - ratio[i - 1]) / (ratio[i] - ratio[i - 1]) * du
kappa_g = 2 * np.trapezoid(ratio, u)
print(f"gauss:  kappa_G={kappa_g:.4f} (measured trend 0.465->0.516)  "
      f"u*={u_star:.4f}  W slope={2 * u_star:.4f} (measured 0.401)")
