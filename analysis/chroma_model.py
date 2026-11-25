"""
Chroma subsampling / reconstruction model.

1-D chain (fine pixel grid, spacing = 1 output pixel):
    c[n]  : true chroma signal on the fine grid (length N)
    decimation factor r : chroma sample kept every r fine pixels
    d[k]  = c[r k]                       (point sampling, no prefilter)
    c_hat[n] = sum_k d[k] w_s(n - r k) / sum_k w_s(n - r k)
where w_s(u) is a compact-support reconstruction kernel of half-support s.

The prefilter variant (box averaging, width r) models the standard 4:2:0
front-end and is provided for the general proof.

All functions operate on float arrays on the fine (output-pixel) grid.
"""

import numpy as np


# ---------------------------------------------------------------- kernels
def tent_weights(offsets, s):
    """Tent (linear) kernel of half-support s, sampled at integer offsets."""
    w = np.maximum(1.0 - np.abs(offsets) / s, 0.0)
    return w


def box_weights(offsets, s):
    """Box kernel of half-support s (|u| < s => 1)."""
    return (np.abs(offsets) < s).astype(np.float64)


KERNELS = {"tent": tent_weights, "box": box_weights}


def reconstruct_1d(d, r, s, N, kind="tent"):
    """Reconstruct a fine-grid signal from samples at positions r*k.

    Normalized convolution: weights are renormalized at every fine position,
    so DC gain is 1 wherever the kernel reaches at least one sample.
    Sparse splat implementation: each sample only touches its own support.
    """
    wfun = KERNELS[kind]
    num = np.zeros(N)
    denom = np.zeros(N)
    offsets = np.arange(-int(np.ceil(s)) - 1, int(np.ceil(s)) + 2)
    wfull = wfun(offsets.astype(np.float64), s)
    for k, val in enumerate(d):
        center = k * r
        for u, w in zip(offsets, wfull):
            if w <= 0.0:
                continue
            n = center + int(u)
            if 0 <= n < N:
                num[n] += w * val
                denom[n] += w
    out = np.zeros(N)
    ok = denom > 0
    out[ok] = num[ok] / denom[ok]
    return out


def chain_1d(c, r, s, kind="tent", prefilter=False):
    """Full decimate -> reconstruct chain on a fine-grid 1-D signal."""
    N = len(c)
    if prefilter:
        # box average of width r before keeping every r-th sample
        pad = r // 2
        cp = np.pad(c, (pad, r - 1 - pad), mode="edge")
        kernel = np.ones(r) / r
        smoothed = np.convolve(cp, kernel, mode="valid")[:N]
        d = smoothed[::r]
    else:
        d = c[::r]
    return reconstruct_1d(d, r, s, N, kind=kind)


# --------------------------------------------------------------- ESF / fringe
def step_signal(N, edge):
    """Unit step u[n-edge], edge can be subpixel (linear interpolation)."""
    x = np.arange(N, dtype=np.float64)
    z = x - edge
    f = np.clip(z + 0.5, 0.0, 1.0)  # pixel-center step with subpixel edge
    f[z >= 0.5] = 1.0
    f[z <= -0.5] = 0.0
    return f


def fringe_width_esf(r, s, kind="tent", prefilter=False,
                     eps=0.02, N=1024, phase_steps=101):
    """Worst-case fringe width from the edge-spread function.

    Sweeps the true edge position e over one decimation cell [0, r).
    At each phase, reconstructs the step and measures the contiguous
    interval around the true edge whose reconstructed value differs from
    0/1 by more than eps, in fine-pixel (output-pixel) units.

    Returns (W_esf, W_cols, worst_edge, left, right):
      W_esf  : subpixel-accurate full contaminated-band width
      W_cols : number of integer fine-grid columns inside the band
      worst_edge, left, right describe the worst phase.
    """
    worst = (-1.0, None)
    for e in np.linspace(0.0, r, phase_steps, endpoint=False):
        edge = N // 2 + e
        c = step_signal(N, edge)
        ch = chain_1d(c, r, s, kind=kind, prefilter=prefilter)
        bad = np.abs(ch - c) > eps
        idx = np.flatnonzero(bad)
        if len(idx) == 0:
            continue
        lo, hi = idx[0], idx[-1]
        # subpixel extent: refine by linear interpolation of |error| vs eps
        err = np.abs(ch - c)
        if lo > 0 and err[lo] > err[lo - 1]:
            left = lo - (err[lo] - eps) / (err[lo] - err[lo - 1])
        else:
            left = float(lo)
        if hi < N - 1 and err[hi] > err[hi + 1]:
            right = hi + (err[hi] - eps) / (err[hi] - err[hi + 1])
        else:
            right = float(hi) + 1.0
        w = right - left
        if w > worst[0]:
            worst = (w, (e, left, right, hi - lo + 1))
    W_esf, (e, left, right, ncols) = worst
    return W_esf, ncols, N // 2 + e, left, right


def esf_profile(r, s, edge, kind="tent", prefilter=False, N=512, half=24):
    """ESF samples around an edge for plotting."""
    e = N // 2 + edge
    c = step_signal(N, e)
    ch = chain_1d(c, r, s, kind=kind, prefilter=prefilter)
    i0 = N // 2 - half
    x = np.arange(i0, N // 2 + half) - N // 2
    return x, c[i0:i0 + 2 * half], ch[i0:i0 + 2 * half]
