"""Explore candidate feedback kernels: design, simulate, measure."""
import sys
import time
import numpy as np

sys.path.insert(0, "/home/ys/feedback/uidreczz28dSNbtWfVZ/analysis")
from design import FS, design_h, ntf_magnitude, SUPPORT
from images import make_images
from simulate import error_diffuse, low_band_fraction

LEVELS = 32          # 8-bit -> 5-bit
RB = 0.15            # low band: disk radius 0.15 (r=1 <=> Nyquist pi)
WHITE_FRAC = RB ** 2 / 4.0


def main():
    imgs = make_images()
    specs = [0.15, 0.25, 0.35]
    kernels = [("FS(original)", FS, None)]
    for rb in specs:
        h, G, _ = design_h(rb)
        kernels.append((f"T(r_b={rb})", h, G))

    print(f"white-noise low-band fraction (r<={RB}): {WHITE_FRAC:.4f}\n")
    for name, h, G in kernels:
        tag = f" G={G:.4f}" if G is not None else ""
        print(f"{name:14s} h={np.round(h, 6)} sum={h.sum():+.1e} "
              f"|NTF(0)|={abs(1+h.sum()):.4f} sum|h|={abs(h).sum():.3f}{tag}")
    print()

    print(f"{'kernel':14s} {'image':10s} {'MSE*1e3':>9s} {'lowFrac':>9s}")
    rows = {}
    for name, h, _ in kernels:
        t0 = time.time()
        mses, fracs = [], []
        for iname, img in imgs.items():
            y, _ = error_diffuse(img, h, LEVELS)
            mse = np.mean((y - img) ** 2)
            frac = low_band_fraction(y - img, RB)
            mses.append(mse)
            fracs.append(frac)
            print(f"{name:14s} {iname:10s} {mse*1e3:9.4f} {frac:9.4f}")
        rows[name] = (np.mean(mses), np.mean(fracs))
        print(f"  [mean] MSE*1e3={np.mean(mses)*1e3:.4f} "
              f"lowFrac={np.mean(fracs):.4f}  ({time.time()-t0:.1f}s)")
    print()
    q = 1.0 / (LEVELS - 1)
    print(f"plain quantizer MSE*1e3 = {q*q/12*1e3:.4f}")
    # plain quantizer spectrum on ramp/natural for reference
    for iname in ("ramp", "natural"):
        img = imgs[iname]
        yp = np.rint(img / q) * q
        print(f"plain-quant {iname:10s} MSE*1e3={np.mean((yp-img)**2)*1e3:.4f} "
              f"lowFrac={low_band_fraction(yp-img, RB):.4f}")


if __name__ == "__main__":
    main()
