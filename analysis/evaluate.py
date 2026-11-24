"""Final evaluation: coefficients, pointwise low-band |NTF|, spectra, MSE."""
import json
import sys
import numpy as np

sys.path.insert(0, "/home/ys/feedback/uidreczz28dSNbtWfVZ/analysis")
from design import A_LO, FS, SUPPORT, design_h, ntf_magnitude, _grid, H_response
from images import make_images
from simulate import error_diffuse, low_band_fraction

LEVELS = 32
RB = 0.15
TAPS = ["h1 (1,0) 右邻", "h2 (0,1) 正下", "h3 (1,1) 右下", "h4 (-1,1) 左下"]


def realized_ntf_energy(h, n=256):
    """Mean |NTF|^2 over the 2-D grid: 1 means noise-energy neutral."""
    rg = _grid(n)
    return float(np.mean(np.abs(1 + H_response(h, np.pi * rg)) ** 2))


def stability_probe(h, n=128):
    """Worst-case boundedness: flat levels offset half a step + noisy inputs."""
    q = 1.0 / (LEVELS - 1)
    worst = 0.0
    for k in range(LEVELS - 1):
        level = (k + 0.5) * q                    # halfway between levels
        x = np.full((n, n), level)
        _, e = error_diffuse(x, h, LEVELS)
        worst = max(worst, np.abs(e).max())
    rng = np.random.default_rng(11)
    x = rng.random((n, n))                       # i.i.d. full-range input
    _, e = error_diffuse(x, h, LEVELS)
    worst = max(worst, np.abs(e).max())
    t = np.linspace(0, 1, n)
    x = np.tile(t, (n, 1))                       # steep ramp
    _, e = error_diffuse(x, h, LEVELS)
    worst = max(worst, np.abs(e).max())
    return worst, q


def main():
    imgs = make_images()
    rbs = [0.15, 0.25, 0.35]
    report = {}

    # ---- pointwise |NTF| in the low band, radial points r=0..0.15 ----
    rr = np.array([0.0, 0.025, 0.05, 0.075, 0.10, 0.125, 0.15])
    print("== 低频段 |NTF(e^{jw})| 逐点值（r=|w|/pi；目标 a=%.2f）==" % A_LO)
    print("r       ", "  ".join(f"{r:7.3f}" for r in rr))
    for name, h in [("FS", FS)] + [(f"rb={b}", design_h(b)[0]) for b in rbs]:
        vals_x = [ntf_magnitude(h, np.array([r]), np.array([0.0]))[0] for r in rr]
        vals_d = [ntf_magnitude(h, np.array([r / np.sqrt(2)]),
                                np.array([r / np.sqrt(2)]))[0] for r in rr]
        print(f"{name:7s} (x轴)  ", "  ".join(f"{v:7.4f}" for v in vals_x))
        print(f"{name:7s} (对角) ", "  ".join(f"{v:7.4f}" for v in vals_d))
    print()

    # ---- full per-kernel report ----
    q = 1.0 / (LEVELS - 1)
    print("== 反馈滤波器系数（4 阶，支撑 (dx,dy)）与闭合误差 ==")
    for rb in rbs:
        h, G, flo = design_h(rb)
        e_ntf = realized_ntf_energy(h)
        yall, mses, fracs = [], [], []
        per_img = {}
        for iname, img in imgs.items():
            y, _ = error_diffuse(img, h, LEVELS)
            mse = float(np.mean((y - img) ** 2))
            frac = low_band_fraction(y - img, RB)
            mses.append(mse)
            fracs.append(frac)
            per_img[iname] = {"mse": mse, "low_frac": frac}
        worst, qstep = stability_probe(h)
        report[f"rb={rb}"] = {
            "r_b": rb, "G": G, "low_grid_fraction": flo,
            "h": {tap: float(c) for tap, c in zip(TAPS, h)},
            "sum_h": float(h.sum()), "dc_gain": float(abs(1 + h.sum())),
            "mean_ntf_energy": e_ntf,
            "mean_mse": float(np.mean(mses)),
            "mean_low_frac": float(np.mean(fracs)),
            "max_abs_error_over_q": float(worst / qstep),
            "per_image": per_img,
        }
        print(f"[r_b={rb}] G={G:.5f}  Σh={h.sum():+.5f}  |NTF(0)|={abs(1+h.sum()):.4f}"
              f"  实测 mean|NTF|^2={e_ntf:.4f}")
        for tap, c in zip(TAPS, h):
            print(f"  {tap}: {c:+.6f}")
        print(f"  平均MSE={np.mean(mses)*1e3:.4f}e-3  低频占比(r<=0.15)={np.mean(fracs):.4f}"
              f"  max|e|/q={worst/qstep:.2f}")
        print()

    # ---- stock FS row ----
    mses, fracs, per_img = [], [], {}
    for iname, img in imgs.items():
        y, _ = error_diffuse(img, FS, LEVELS)
        mse = float(np.mean((y - img) ** 2))
        frac = low_band_fraction(y - img, RB)
        mses.append(mse)
        fracs.append(frac)
        per_img[iname] = {"mse": mse, "low_frac": frac}
    worst, qstep = stability_probe(FS)
    report["FS"] = {
        "h": {tap: float(c) for tap, c in zip(TAPS, FS)},
        "sum_h": float(FS.sum()), "dc_gain": 2.0,
        "mean_ntf_energy": realized_ntf_energy(FS),
        "mean_mse": float(np.mean(mses)),
        "mean_low_frac": float(np.mean(fracs)),
        "max_abs_error_over_q": float(worst / qstep),
        "per_image": per_img,
    }
    print(f"[FS 原核] mean|NTF|^2={realized_ntf_energy(FS):.4f} "
          f"平均MSE={np.mean(mses)*1e3:.4f}e-3  "
          f"低频占比={np.mean(fracs):.4f}  max|e|/q={worst/qstep:.2f}")

    # ---- plain quantizer ----
    mses, fracs = [], []
    for img in imgs.values():
        yp = np.rint(img / q) * q
        mses.append(np.mean((yp - img) ** 2))
        fracs.append(low_band_fraction(yp - img, RB))
    report["plain"] = {"mean_mse": float(np.mean(mses)),
                       "mean_low_frac": float(np.mean(fracs))}
    print(f"[直接量化] 平均MSE={np.mean(mses)*1e3:.4f}e-3  "
          f"低频占比={np.mean(fracs):.4f}  (白噪声理论占比={RB**2/4:.4f})")

    with open("/home/ys/feedback/uidreczz28dSNbtWfVZ/analysis/report.json", "w") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)


if __name__ == "__main__":
    main()
