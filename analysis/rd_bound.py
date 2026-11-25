#!/usr/bin/env python3
"""Reproduce the Gaussian residual rate-distortion comparison.

The source is X ~ N(0, sigma^2), MSE distortion, and the practical encoder is
a mid-tread uniform scalar quantizer k = round(X / Delta), X_hat = k * Delta,
followed by an entropy code. Delta is chosen so H(k) equals each target rate.

Run with: python3 -I analysis/rd_bound.py
"""

from __future__ import annotations

import heapq
import json
import math
from math import erf, exp, log2, pi, sqrt

SIGMA = 32.0
RATES = (1.0, 2.0, 3.0)
MAX_Q = 240


def phi(x: float) -> float:
    return exp(-x * x / 2.0) / sqrt(2.0 * pi)


def Phi(x: float) -> float:
    return 0.5 * (1.0 + erf(x / sqrt(2.0)))


def quantizer_stats(delta_std: float) -> tuple[float, float, list[float]]:
    """Return H(K), MSE for Z~N(0,1), and symbol probabilities."""
    entropy = 0.0
    mse = 0.0
    probs: list[float] = []

    for q in range(-MAX_Q, MAX_Q + 1):
        a = (q - 0.5) * delta_std
        b = (q + 0.5) * delta_std
        p = Phi(b) - Phi(a)
        if p <= 0.0:
            continue

        # Truncated first/second Gaussian moments over [a, b).
        m1 = phi(a) - phi(b)
        b_term = 0.0 if math.isinf(b) else b * phi(b)
        a_term = 0.0 if math.isinf(a) else a * phi(a)
        m2 = p - (b_term - a_term)

        recon = q * delta_std
        mse += m2 - 2.0 * recon * m1 + recon * recon * p
        entropy -= p * log2(p)
        probs.append(p)

    return entropy, mse, probs


def solve_delta(target_entropy: float) -> tuple[float, float, float, list[float]]:
    lo, hi = 0.05, 12.0
    for _ in range(120):
        mid = (lo + hi) / 2.0
        entropy, _, _ = quantizer_stats(mid)
        if entropy > target_entropy:
            lo = mid  # quantizer is too fine
        else:
            hi = mid
    delta = (lo + hi) / 2.0
    entropy, mse, probs = quantizer_stats(delta)
    return delta, entropy, mse, probs


def huffman_expected_length(probs: list[float]) -> float:
    heap = [[p, [i]] for i, p in enumerate(probs) if p > 1e-14]
    heapq.heapify(heap)
    lengths = [0] * len(probs)

    if len(heap) == 1:
        return 1.0

    while len(heap) > 1:
        a = heapq.heappop(heap)
        b = heapq.heappop(heap)
        for i in a[1] + b[1]:
            lengths[i] += 1
        heapq.heappush(heap, [a[0] + b[0], a[1] + b[1]])

    return sum(p * length for p, length in zip(probs, lengths))


def psnr(mse: float, peak: float = 255.0) -> float:
    return 10.0 * math.log10(peak * peak / mse)


def main() -> None:
    variance = SIGMA * SIGMA
    rows = []

    for rate in RATES:
        delta_std, entropy, mse_std, probs = solve_delta(rate)
        huffman_rate = huffman_expected_length(probs)

        d_enc = mse_std * variance
        d_lb = variance * 2.0 ** (-2.0 * rate)
        gap = d_enc - d_lb

        # Bound at the literal Huffman rate, kept separate from the main table.
        d_lb_huff = variance * 2.0 ** (-2.0 * huffman_rate)

        rows.append(
            {
                "target_bpp": rate,
                "compression_ratio": 8.0 / rate,
                "delta_gray_levels": delta_std * SIGMA,
                "entropy_bpp": entropy,
                "huffman_expected_bpp": huffman_rate,
                "huffman_redundancy_bpp": huffman_rate - entropy,
                "d_enc_mse": d_enc,
                "d_lb_mse": d_lb,
                "gap_abs_mse": gap,
                "gap_percent_vs_lb": 100.0 * gap / d_lb,
                "gap_percent_of_actual": 100.0 * gap / d_enc,
                "d_lb_at_huffman_rate_mse": d_lb_huff,
                "gap_at_huffman_rate_mse": d_enc - d_lb_huff,
                "gap_percent_vs_huffman_lb": 100.0 * (d_enc - d_lb_huff) / d_lb_huff,
                "psnr_enc_db": psnr(d_enc),
                "psnr_lb_db": psnr(d_lb),
            }
        )

    print(json.dumps({"sigma": SIGMA, "metric": "MSE", "rows": rows}, indent=2))


if __name__ == "__main__":
    main()
