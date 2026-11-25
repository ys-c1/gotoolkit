"""检查平坦块量化后到底哪些系数非零、为何残差块比例固定。"""
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import codec_exp as ce
import correlation as cr

img = ce.synthesize_test_image()
out = ce.encode_decode(img, quality=50, aq_strength=1.0)
lab = cr.classify_blocks(out["activity"])
flat = lab == 0
print("flat blocks:", flat.sum())
q = out["qcoeffs"][flat]            # (n,8,8) 量化后的整数系数
nz_per_block = (q.reshape(len(q), -1) != 0).sum(axis=1)
print("nonzero coeffs per block: min", nz_per_block.min(),
      "median", np.median(nz_per_block), "max", nz_per_block.max())
print("blocks with >1 nonzero coeff:", (nz_per_block > 1).sum(),
      " exactly 1:", (nz_per_block == 1).sum(), " zero:", (nz_per_block == 0).sum())
# 非零系数落在哪些频率位置
pos = np.argwhere(q != 0)
freqs = pos[:, 1:] * 1
from collections import Counter
cnt = Counter(map(tuple, freqs))
print("nonzero coefficient frequency positions (k,l):count:", cnt.most_common(8))
# 残差恰好为零的块：重建块与原块逐元素相等
rb = (out["recon_u8"].astype(float) - ce.to_uint8(img).astype(float))
rb = rb.reshape(64, 8, 64, 8).transpose(0, 2, 1, 3).reshape(-1, 8, 8)[flat]
zero_resid = np.all(rb == 0, axis=(1, 2))
print("flat blocks with exactly zero residual:", zero_resid.sum(), "/", len(flat))
# 有非零系数但残差为零的块
mask = (nz_per_block > 0) & zero_resid
print("blocks nonzero-coeff but zero-pixel-residual:", mask.sum())
# DC 系数与步长
dc = q[:, 0, 0]
print("DC quantized nonzero:", (dc != 0).sum(), "DC step range:",
      out["steps"][flat][:, 0, 0].min(), out["steps"][flat][:, 0, 0].max())
