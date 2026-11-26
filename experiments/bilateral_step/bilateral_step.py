#!/usr/bin/env python3
"""Bilateral filter on an ideal step edge: exact 1D computation and metrics.

Model (1D, the vertical 2D step reduces to this exactly):
  s[x] = a for x < e, b for x >= e, Delta = b - a > 0.
Bilateral output:
  y[x] = (sum_{k in [x-r, x+r]} w_s(|k-x|) w_r(|s[k]-s[x]|) s[k])
         / (sum_k w_s(...) w_r(...))
with w_s = Gaussian truncated at r, sigma_s = r/c (c=2); w_r = Gaussian
in intensity, parameter sigma_r. Cross-side gray-difference weight
rho = w_r(Delta).

Metrics:
  eta  gradient retention ratio = (y[e]-y[e-1]) / Delta (original gradient).
  W    smoothing width (px): sum over both sides of the half-height width,
       measured by linear interpolation of the filtered profile at level
       a+Delta/2 (dark side) / b-Delta/2 (bright side).
  L    total normalized leakage mass = sum_x |y-s| / Delta (pixels), twice
       the single-side sum by symmetry.

All numbers are exact evaluations of the finite-kernel filter, not
asymptotics. Run with: python3 -I bilateral_step.py
"""
import csv
import json
import os

import numpy as np

# ---------------------------------------------------------------- setup
OUT = os.environ.get("OUT_DIR", "/tmp/bilateral_step")
N = 128
EDGE = N // 2          # edge between pixel e-1 (dark) and e (bright)
A, B = 64.0, 192.0     # dark / bright levels
DELTA = B - A          # 128
RADII = [2, 4, 8]
C = 2.0                # truncation: r = c * sigma_s
SIGMA_R_FIXED = 60.0   # fixed range width across radii -> rho constant
TOL = 1e-12


def ideal_step():
    s = np.full(N, A, dtype=np.float64)
    s[EDGE:] = B
    return s


def gauss_spatial(r, c=C):
    sigma_s = r / c
    k = np.arange(-r, r + 1, dtype=np.float64)
    w = np.exp(-(k ** 2) / (2.0 * sigma_s ** 2))
    return k, w


def bilateral_profile(s, r, sigma_r):
    """Exact bilateral filter on the 1D step (full arithmetic, float64)."""
    k, ws = gauss_spatial(r)
    y = np.empty_like(s)
    for x in range(N):
        idx = np.clip(x + k.astype(int), 0, N - 1)
        wr = np.exp(-((s[idx] - s[x]) ** 2) / (2.0 * sigma_r ** 2))
        w = ws * wr
        y[x] = np.dot(w, s[idx]) / w.sum()
    return y


def gaussian_profile(s, r):
    """Plain Gaussian spatial filter (same w_s, no range term)."""
    k, ws = gauss_spatial(r)
    y = np.empty_like(s)
    for x in range(N):
        idx = np.clip(x + k.astype(int), 0, N - 1)
        y[x] = np.dot(ws, s[idx]) / ws.sum()
    return y


def half_height_width(profile):
    """Smoothing width = half-height width of the leakage bump on each side.

    Dark-side leakage  l_d(x) = (y[x]-a)/Delta, x < EDGE, peaks at EDGE-1.
    Bright side        l_b(x) = (b-y[x])/Delta, x >= EDGE, peaks at EDGE.
    On each side the point where leakage falls to half its peak is located by
    linear interpolation; side width runs from the edge center (EDGE-0.5)
    to that point. Total width W = w_dark + w_bright (pixels).
    """
    edge_center = EDGE - 0.5
    # ---- dark side, walk inward from the edge pixel EDGE-1
    leak_d = np.array([(profile[x] - A) / DELTA for x in range(EDGE)])
    peak_d = leak_d[EDGE - 1]
    half_d = peak_d / 2.0
    x_dark = None
    for x in range(EDGE - 2, -1, -1):
        if leak_d[x] <= half_d < leak_d[x + 1]:
            t = (half_d - leak_d[x]) / (leak_d[x + 1] - leak_d[x])
            x_dark = x + t
            break
    if x_dark is None:  # plateau pixel already at/under half: use edge pixel
        x_dark = float(EDGE - 1)
    w_dark = edge_center - x_dark
    # ---- bright side, walk outward from the edge pixel EDGE
    leak_b = np.array([(B - profile[x]) / DELTA for x in range(EDGE, N)])
    peak_b = leak_b[0]
    half_b = peak_b / 2.0
    x_bright = None
    for j in range(1, len(leak_b)):
        if leak_b[j] <= half_b < leak_b[j - 1]:
            t = (leak_b[j - 1] - half_b) / (leak_b[j - 1] - leak_b[j])
            x_bright = EDGE + (j - 1) + t
            break
    if x_bright is None:
        x_bright = float(EDGE)
    w_bright = x_bright - edge_center
    return float(w_dark), float(w_bright), float(w_dark + w_bright)


def metrics(s, y, rho):
    eta = (y[EDGE] - y[EDGE - 1]) / DELTA
    w_dark, w_bright, width = half_height_width(y)
    # leakage mass, restricted to the edge band where deviation is visible
    band = np.abs(y - s) > TOL
    leakage = float(np.sum(np.abs(y[band] - s[band])) / DELTA)
    # peak per-pixel leakage at the edge pixels, normalized
    peak = float(max(y[EDGE - 1] - A, B - y[EDGE]) / DELTA)
    return {
        "rho": rho,
        "eta": float(eta),
        "width_dark_px": w_dark,
        "width_bright_px": w_bright,
        "width_total_px": width,
        "leakage_mass_px": leakage,
        "peak_leakage": peak,
    }


def write_pgm(path, img):
    h, w = img.shape
    q = np.clip(np.rint(img), 0, 255).astype(np.uint8)
    with open(path, "wb") as f:
        f.write(f"P5\n{w} {h}\n255\n".encode())
        f.write(q.tobytes())


def main():
    os.makedirs(OUT, exist_ok=True)
    s = ideal_step()
    rho_fixed = float(np.exp(-(DELTA ** 2) / (2.0 * SIGMA_R_FIXED ** 2)))

    rows = []          # profile rows
    summary = []       # metric rows
    profiles = {"x": np.arange(N), "ideal": s}

    # ---- bilateral, fixed sigma_r (rho constant), three radii
    for r in RADII:
        y = bilateral_profile(s, r, SIGMA_R_FIXED)
        profiles[f"bilateral_r{r}"] = y
        m = metrics(s, y, rho_fixed)
        m.update({"filter": "bilateral", "r": r, "sigma_r": SIGMA_R_FIXED})
        summary.append(m)
        write_pgm(os.path.join(OUT, f"bilateral_r{r}.pgm"),
                  np.tile(y[None, :], (N, 1)))

    # ---- bilateral, 1/r tuning: rho(r) = rho(2) * 2 / r
    rho0 = rho_fixed * RADII[0]  # rho*r target constant = rho(2)*2
    tuned = []
    for r in RADII:
        rho_r = rho0 / r
        sigma_r = DELTA / np.sqrt(-2.0 * np.log(rho_r))
        y = bilateral_profile(s, r, sigma_r)
        profiles[f"tuned_r{r}"] = y
        m = metrics(s, y, rho_r)
        m.update({"filter": "bilateral_tuned_1_over_r", "r": r,
                  "sigma_r": float(sigma_r)})
        tuned.append(m)

    # ---- plain Gaussian (range weight = 1), same radii, as contrast
    for r in RADII:
        y = gaussian_profile(s, r)
        profiles[f"gauss_r{r}"] = y
        m = metrics(s, y, 1.0)
        m.update({"filter": "gaussian", "r": r, "sigma_r": None})
        summary.append(m)
        write_pgm(os.path.join(OUT, f"gauss_r{r}.pgm"),
                  np.tile(y[None, :], (N, 1)))

    write_pgm(os.path.join(OUT, "step_ideal.pgm"),
              np.tile(s[None, :], (N, 1)))

    with open(os.path.join(OUT, "profiles.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(list(profiles.keys()))
        for i in range(N):
            w.writerow([profiles[k][i] for k in profiles])

    with open(os.path.join(OUT, "metrics.json"), "w") as f:
        json.dump({"constants": {"N": N, "edge": EDGE, "A": A, "B": B,
                                 "Delta": DELTA, "c": C,
                                 "sigma_r_fixed": SIGMA_R_FIXED,
                                 "rho_fixed": rho_fixed,
                                 "rho_r_product_target": rho0},
                   "bilateral_fixed": summary[:3],
                   "bilateral_tuned": tuned,
                   "gaussian": summary[3:]}, f, indent=2)

    # ---- console table
    def line(m):
        return (f"r={m['r']:>2}  rho={m['rho']:.5f}  "
                f"W_dark={m['width_dark_px']:6.3f}  "
                f"W_bright={m['width_bright_px']:6.3f}  "
                f"W_total={m['width_total_px']:6.3f} px   "
                f"eta={m['eta']:.4f}  L={m['leakage_mass_px']:6.3f} px")

    print(f"rho at fixed sigma_r={SIGMA_R_FIXED}: {rho_fixed:.6f}\n")
    print("bilateral, fixed range width:")
    for m in summary[:3]:
        print("  " + line(m))
    print("\nbilateral, 1/r tuned range width:")
    for m in tuned:
        print("  " + line(m))
    print("\nplain Gaussian (full smoothing, rho=1):")
    for m in summary[3:]:
        print("  " + line(m))


if __name__ == "__main__":
    main()
