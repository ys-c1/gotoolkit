"""受控诊断：DCT 死区量化噪声的本征 ACF，剥离 AQ 与图像结构。"""
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import codec_exp as ce
import correlation as cr


def gaussian_blocks(kind: str, nb: int = 20000, rng=None):
    rng = np.random.default_rng(0) if rng is None else rng
    if kind == "white":  # 独立同分布白噪声
        return rng.standard_normal((nb, 8, 8)) * 20.0
    if kind == "smooth":  # 单低频模式（类平坦）
        b = np.zeros((nb, 8, 8))
        b[:, 0, 0] = rng.standard_normal(nb) * 40.0
        b[:, 0, 1] = rng.standard_normal(nb) * 8.0
        b[:, 1, 0] = rng.standard_normal(nb) * 8.0
        return ce.block_idct(b)
    if kind == "mid":  # 中频模式（类纹理）
        b = np.zeros((nb, 8, 8))
        for k, l, amp in [(2, 2, 18), (3, 1, 14), (1, 3, 14), (3, 3, 10)]:
            b[:, k, l] = rng.standard_normal(nb) * amp
        return ce.block_idct(b)
    if kind == "edge":  # 单块内阶跃 + 平滑
        b = np.zeros((nb, 8, 8))
        pos = rng.integers(2, 6, nb)
        for i in range(nb):
            b[i, :, pos[i]:] = 60.0
        return b
    raise ValueError(kind)


def run(kind, quality, aq=0.0):
    blocks = gaussian_blocks(kind)
    coeffs = ce.block_dct(blocks)
    qt = ce.base_quant_table(quality)
    if aq == 0.0:
        steps = np.broadcast_to(qt, coeffs.shape).copy()
    else:
        act = ce.block_activity(blocks)
        m = ce.aq_multipliers(act, aq)
        steps = m[:, None, None] * qt[None]
    q = ce.deadzone_quantize(coeffs, steps)
    rec = ce.block_idct(q * steps)
    resid = rec - blocks
    acf = cr.block_acf(resid)
    cl = cr.correlation_length(acf)
    rms = np.sqrt(np.mean(resid ** 2))
    print(f"--- {kind:7s} Q={quality:3d} aq={aq} rms={rms:6.3f} "
          f"L_env={cl['L_env'] if np.isnan(cl['L_env']) else round(cl['L_env'],3)} "
          f"L_int={cl['L_int']:.3f} L_zero={cl['L_zero'] if np.isnan(cl['L_zero']) else round(cl['L_zero'],3)}")
    print("  h:", np.round(acf[0], 3))
    print("  v:", np.round(acf[1], 3))


for kind in ("white", "smooth", "mid", "edge"):
    for q in (90, 50, 20):
        run(kind, q)
