"""
2-D separable chroma subsampling chain and test-image synthesis.

A fine RGB image is converted to YCbCr; Cb, Cr are decimated by r on each
axis (point sampling) and reconstructed with a separable tent kernel of
half-support s. Y is untouched by the chroma chain. Luma-loss experiments
run the same decimate/reconstruct chain directly on Y.
"""
import numpy as np

from chroma_model import reconstruct_1d


def rgb2ycbcr(rgb):
    M = np.array([[0.299, 0.587, 0.114],
                  [-0.168736, -0.331264, 0.5],
                  [0.5, -0.418688, -0.081312]])
    ycc = rgb @ M.T
    ycc[..., 1:] += 128.0
    return ycc


def ycbcr2rgb(ycc):
    M = np.array([[0.299, 0.587, 0.114],
                  [-0.168736, -0.331264, 0.5],
                  [0.5, -0.418688, -0.081312]])
    z = ycc.copy()
    z[..., 1:] -= 128.0
    return z @ np.linalg.inv(M.T)


def chain_2d(plane, r, s, kind="tent"):
    """Separable decimate (point) + reconstruct on a 2-D float plane."""
    H, W = plane.shape
    out = np.zeros_like(plane)
    # horizontal
    for i in range(H):
        d = plane[i, ::r]
        out[i] = reconstruct_1d(d, r, s, W, kind=kind)
    # vertical
    tmp = out.copy()
    for j in range(W):
        d = tmp[::r, j]
        out[:, j] = reconstruct_1d(d, r, s, H, kind=kind)
    return out


def subsample_rgb(rgb, r, s, kind="tent"):
    """Full RGB -> YCbCr -> chroma chain -> RGB (Y untouched)."""
    ycc = rgb2ycbcr(rgb)
    rec = ycc.copy()
    for c in (1, 2):
        rec[..., c] = chain_2d(ycc[..., c], r, s, kind=kind)
    out = ycbcr2rgb(rec)
    return np.clip(out, 0, 255), rec


# ------------------------------------------------------------- test images
def img_vertical_edge(N=240):
    """Hard red/green vertical edge: maximal chroma step, flat luma."""
    rgb = np.zeros((N, N, 3))
    rgb[:, : N // 2] = (200, 30, 30)
    rgb[:, N // 2:] = (30, 200, 30)
    return rgb


def img_diagonal_edge(N=240):
    """Blue/yellow edge tilted ~20 deg: fringe width varies along it."""
    yy, xx = np.mgrid[0:N, 0:N]
    slope = 0.35
    side = xx - (N / 2 + slope * (yy - N / 2))
    rgb = np.zeros((N, N, 3))
    rgb[side < 0] = (30, 60, 220)
    rgb[side >= 0] = (230, 220, 60)
    return rgb


def img_natural(N=240, seed=7):
    """Mixed content: colored shapes, a wedge, a fine luma grating."""
    rng = np.random.default_rng(seed)
    rgb = np.full((N, N, 3), 225.0)
    yy, xx = np.mgrid[0:N, 0:N]
    # magenta disk with hard rim
    disk = (xx - 80) ** 2 + (yy - 90) ** 2 < 52 ** 2
    rgb[disk] = (210, 40, 180)
    # cyan rectangle
    rgb[150:220, 30:120] = (30, 180, 190)
    # orange wedge (triangular)
    wedge = (xx > 140) & (yy > 130) & (yy < 130 + (xx - 140) * 1.4)
    rgb[wedge] = (235, 110, 30)
    # hard two-tone vertical bar: strong vertical chroma step for the
    # fringe-width measurement
    bar = (xx >= 170) & (xx < 205) & (yy >= 30) & (yy <= 120)
    rgb[bar] = (25, 150, 220)
    bar2 = (xx >= 205) & (xx < 240) & (yy >= 30) & (yy <= 120)
    rgb[bar2] = (240, 180, 30)
    # fine luma grating inside a neutral band (luma detail probe)
    grating = ((xx // 4) % 2 == 0)
    band = (yy >= 20) & (yy <= 60)
    rgb[band & grating] = (40, 40, 40)
    rgb[band & ~grating] = (210, 210, 210)
    # gentle chroma texture elsewhere
    rgb[..., 0] += 8 * np.sin(xx / 13.0)
    rgb[..., 2] += 8 * np.cos(yy / 17.0)
    return np.clip(rgb, 0, 255)


IMAGES = {"vertical": img_vertical_edge,
          "diagonal": img_diagonal_edge,
          "natural": img_natural}
