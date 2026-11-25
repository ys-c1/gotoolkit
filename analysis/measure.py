"""
Fringe-band measurement protocols.

Two complementary quantities, both measured on the fine output grid:

W_influence (the theorem's object): a fine column is contaminated iff it
receives strictly positive kernel weight from a sample lying on the far
side of the true edge. The contaminated columns are exactly those inside
(k_L - s, k_R + s), the union of the two flanking samples' supports. Its
subpixel span is r + 2s; the integer-column count differs from the span by
at most 1 px (open interval).

W_visible (what the eye sees): contiguous columns with |chat - c| > eps,
bridged across unity-gain punctures. Smaller, and phase/content dependent.
"""
import sys
import numpy as np

sys.path.insert(0, "/home/ys/feedback/uidreczz28dpNBzBitz/analysis")
from chroma_model import step_signal, chain_1d, tent_weights, box_weights


def influence_mask_1d(edge, r, s, N, kind="tent"):
    """Boolean mask: fine columns reached by a wrong-side flanking sample."""
    wfun = tent_weights if kind == "tent" else box_weights
    kL_idx = int(np.floor(edge / r))          # last sample on the low side
    kR_idx = kL_idx + 1                       # first sample on the high side
    offsets = np.arange(-int(np.ceil(s)) - 1, int(np.ceil(s)) + 2)
    mask = np.zeros(N, dtype=bool)
    for k in (kL_idx, kR_idx):
        center = k * r
        for u in offsets:
            n = center + u
            if 0 <= n < N and wfun(np.array([float(u)]), s)[0] > 0.0:
                mask[n] = True
    return mask, kL_idx * r, kR_idx * r


def measure_influence(edge, r, s, N, kind="tent"):
    """Return (subpixel span r+2s, integer contaminated columns)."""
    mask, pL, pR = influence_mask_1d(edge, r, s, N, kind)
    span = (pR + s) - (pL - s)
    return span, int(mask.sum())


def measure_visible(c, chat, eps=0.02):
    """Bridged threshold-visible fringe columns and subpixel span."""
    N = len(c)
    err = np.abs(chat - c)
    mask = err > eps
    if not mask.any():
        return 0.0, 0
    idx = np.flatnonzero(mask)
    lo, hi = idx[0], idx[-1]
    ncols = hi - lo + 1
    if lo > 0 and err[lo] > err[lo - 1]:
        left = lo - err[lo] / (err[lo] - err[lo - 1])
    else:
        left = float(lo)
    if hi < N - 1 and err[hi] > err[hi + 1]:
        right = hi + err[hi] / (err[hi] - err[hi + 1])
    else:
        right = float(hi) + 1.0
    return right - left, ncols


def worst_phase(r, s, kind="tent", eps=0.02, phases=401, N=2048):
    """Worst over edge phases of the visible fringe; influence is invariant."""
    w_vis_w, vis_cols = 0.0, 0
    for e in np.linspace(0.0, r, phases, endpoint=False):
        edge = N // 2 + e
        c = step_signal(N, edge)
        chat = chain_1d(c, r, s, kind=kind)
        w, ncols = measure_visible(c, chat, eps)
        if w > w_vis_w:
            w_vis_w, vis_cols = w, ncols
    edge0 = N // 2
    span, infl_cols = measure_influence(edge0, r, s, N, kind)
    return span, infl_cols, w_vis_w, vis_cols


if __name__ == "__main__":
    print(f"{'r':>2} {'s':>4} | {'r+2s':>5} {'span':>6} {'cols':>4} "
          f"{'span-px':>7} | visible(2%) worst")
    ok_all = True
    for r in (2, 3, 4):
        for s in (r + 0.5, r + 1.0, r + 1.5):
            span, ncols, wvis, vcols = worst_phase(r, s)
            pred = r + 2 * s
            # the check: integer columns vs span, within 1 px
            d = abs(ncols - span)
            ok = d <= 1.0 and abs(span - pred) < 1e-9
            ok_all &= ok
            print(f"{r:2d} {s:4.1f} | {pred:5.1f} {span:6.1f} {ncols:4d} "
                  f"{d:7.1f} | {wvis:6.2f}px / {vcols} cols  {'PASS' if ok else 'FAIL'}")
    print("1px RULE:", "PASS" if ok_all else "FAIL")
    # below-coverage regime demonstration
    print("\ncoverage holes (s <= r/2): gaps between consecutive supports ->")
    for r, s in ((2, 1.0), (4, 1.5)):
        N = 64
        edge = N // 2
        chat = chain_1d(step_signal(N, edge), r, s, kind="tent")
        gap = r - 2 * s
        print(f"  r={r} s={s}: inter-support gap = r-2s = {gap:.1f} px "
              f"(>0 means uncovered columns); r+2s={r+2*s} is then a loose, "
              f"unmeasurable bound, so this regime is excluded")
