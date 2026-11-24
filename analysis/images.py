"""Synthetic 8-bit-equivalent test images in [0,1], 256x256."""
import numpy as np


def make_images(n=256, seed=7):
    rng = np.random.default_rng(seed)
    t = np.linspace(0, 1, n)
    xx, yy = np.meshgrid(t, t)

    flat = np.full((n, n), 0.37)

    grad = 0.15 + 0.7 * xx                       # horizontal ramp

    smooth = (0.5 + 0.25 * np.sin(2 * np.pi * 2 * xx)
              + 0.15 * np.cos(2 * np.pi * 3 * yy))

    # low-frequency Gaussians + mild texture
    img = 0.5 * np.ones((n, n))
    for cx, cy, amp, sig in [(0.3, 0.35, 0.35, 0.12),
                             (0.75, 0.65, -0.3, 0.18),
                             (0.6, 0.2, 0.15, 0.08)]:
        img += amp * np.exp(-((xx - cx) ** 2 + (yy - cy) ** 2) / (2 * sig ** 2))
    img += 0.03 * rng.standard_normal((n, n))
    natural = np.clip(img, 0, 1)

    return {"flat0.37": flat, "ramp": grad, "smooth": smooth,
            "natural": np.clip(smooth, 0, 1) if False else natural}
