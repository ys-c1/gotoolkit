"""Feedback filter design for error diffusion (numpy only).

Raster 2-D error diffusion with feedback kernel h on fixed causal support
    d_k in {(1,0),(0,1),(1,1),(-1,1)}  (dx, dy), raster scan:
    u[n] = x[n] + sum_k h_k e[n-d_k],  y = Q(u),  e = y - u.
Additive quantizer noise model (e = q, white) gives the output error
    y - x = NTF(w) q,  NTF(w) = 1 + H(w),  H(w)= sum h_k e^{-j w.d_k}.
White-noise target => desired |NTF| = 1.  A stock kernel (Floyd-Steinberg)
has sum h = 1, hence |NTF(0)| = 2 (DC noise amplified by 6 dB).

Target spectrum (normalized radius r, r=1 <=> Nyquist pi):
    M(r) = a                         r <= r_b   (suppressed low band)
    M(r) = 1 + G s(r)^2,  s = ((r-r_b)/(1-r_b))^2,  r > r_b
with fixed a = 0.5 (low-band power target 1/4 of white).  G is solved in
closed form from full-band energy neutrality  mean M^2 = 1, so the total
quantization noise is unchanged (MSE = sigma_q^2 * mean|NTF|^2).  A weighted
complex least-squares fit with equality constraint sum h = 0 (=> |NTF(0)|=1)
gives the 4-tap feedback coefficients.
"""
import numpy as np

SUPPORT = np.array([(1, 0), (0, 1), (1, 1), (-1, 1)], dtype=float)
FS = np.array([7 / 16, 5 / 16, 1 / 16, 3 / 16])  # stock kernel, sum = 1
A_LO = 0.5                                        # low-band target magnitude


def _grid(n=80):
    g = np.linspace(-1, 1, n, endpoint=False)
    gx, gy = np.meshgrid(g, g)
    return np.stack([gx.ravel(), gy.ravel()], axis=1)  # r in [-1,1)


def _target(r_b, rg):
    r = np.linalg.norm(rg, axis=1)
    s = np.zeros_like(r)
    hi = r > r_b
    s[hi] = ((r[hi] - r_b) / (1.0 - r_b)) ** 2
    flo = float(np.mean(r <= r_b))
    s1, s2 = np.mean(s), np.mean(s ** 2)
    # neutrality: flo*a^2 + (1-flo) + 2G S1 + G^2 S2 = 1
    c = flo * (1.0 - A_LO ** 2)
    G = (-s1 + np.sqrt(s1 ** 2 + s2 * c)) / s2
    M = np.where(r <= r_b, A_LO, 1.0 + G * s)
    return M, G, flo


def design_h(r_b, w_lo=10.0, reg=1e-10, n=80):
    """Weighted complex LS: fit NTF to real target M over the spectral grid."""
    rg = _grid(n)
    M, G, flo = _target(r_b, rg)
    A = np.exp(-1j * (np.pi * rg @ SUPPORT.T))     # (P,K)
    b = (M - 1.0).astype(complex)                  # A h = M - 1
    wt = np.where(np.linalg.norm(rg, axis=1) <= r_b, w_lo, 1.0)

    sw = np.sqrt(wt)
    Aw = A * sw[:, None]
    bw = b * sw
    K = Aw.conj().T @ Aw + reg * np.eye(4)
    f = Aw.conj().T @ bw
    h = np.linalg.solve(K, f).real
    return h, G, flo


def H_response(h, wg):
    return (h[None, :] * np.exp(-1j * (wg @ SUPPORT.T))).sum(axis=1)


def ntf_magnitude(h, rx, ry):
    """|NTF| at normalized-frequency points (r=1 <=> Nyquist pi)."""
    rg = np.stack([np.ravel(rx), np.ravel(ry)], axis=1)
    return np.abs(1 + H_response(h, np.pi * rg)).reshape(np.shape(rx))
