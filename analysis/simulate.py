"""Raster error-diffusion simulation + output-error spectrum measurement."""
import numpy as np

# offsets (dx, dy) matching design.SUPPORT
OFFSETS = [(1, 0), (0, 1), (1, 1), (-1, 1)]


def quantize(u, q, lmax):
    return min(max(round(u / q), 0), lmax) * q


def error_diffuse(x01, h, levels=32):
    """Raster-scan 2-D error diffusion with feedback kernel h (len 4).

    Tap order matches design.SUPPORT / Go Kernel fields:
      h[0] (1,0) right, h[1] (0,1) below, h[2] (1,1) below-right,
      h[3] (-1,1) below-left.
    Row-vectorized: the right-neighbour tap is propagated via a running
    scalar; the three taps touching the previous row are gathered by slices.
    """
    H, W = x01.shape
    q = 1.0 / (levels - 1)
    lmax = levels - 1
    e = np.zeros((H, W))
    y = np.zeros((H, W))
    h_r, h_b, h_br, h_bl = h
    for row in range(H):
        prev = row - 1
        u = x01[row].copy()
        if prev >= 0:
            u += h_b * e[prev]
            u[1:] += h_bl * e[prev, :-1]            # (-1,1) below-left
            u[:-1] += h_br * e[prev, 1:]            # (1,1) below-right
        carry = 0.0
        for col in range(W):
            uc = u[col] + h_r * carry
            yo = quantize(uc, q, lmax)
            ec = yo - uc
            y[row, col] = yo
            e[row, col] = ec
            carry = ec
    return y, e


def low_band_fraction(err, rb, windowed=True):
    """Fraction of DFT energy inside disk radius rb (Nyquist = 1)."""
    H, W = err.shape
    if windowed:
        win = np.hanning(H)[:, None] * np.hanning(W)[None, :]
        sig = err * win
    else:
        sig = err
    F = np.fft.fftshift(np.fft.fft2(sig))
    P = np.abs(F) ** 2
    fy = np.fft.fftshift(np.fft.fftfreq(H)) * 2.0
    fx = np.fft.fftshift(np.fft.fftfreq(W)) * 2.0
    gx, gy = np.meshgrid(fx, fy)
    rr = np.sqrt(gx ** 2 + gy ** 2)
    return float(P[rr <= rb].sum() / P.sum())
