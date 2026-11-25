"""诊断平坦类块的残差能量分布与放大窗选择。"""
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

d = np.load("../artifacts/aqnoise/results.npz", allow_pickle=True)
store = d["store"].item()
lab = store["labels"].reshape(64, 64)
resid = store["q50"]["resid"]
be = (resid.reshape(64, 8, 64, 8).transpose(0, 2, 1, 3) ** 2).mean(axis=(2, 3))
flat_eng = np.where(lab == 0, be, np.nan)
print("flat block energy: nonzero frac =", np.mean(flat_eng[lab == 0] > 1e-12),
      "median =", np.nanmedian(flat_eng), "max =", np.nanmax(flat_eng))
# 纯平坦 8x8 方阵里按平均能量排序
cand = []
for bi in range(56):
    for bj in range(56):
        if (lab[bi:bi + 8, bj:bj + 8] == 0).all():
            cand.append((flat_eng[bi:bi + 8, bj:bj + 8].mean(), bi, bj))
cand.sort(reverse=True)
print("top 5 pure-flat windows:")
for e, bi, bj in cand[:5]:
    w = flat_eng[bi:bi + 8, bj:bj + 8]
    print(f"  ({bi},{bj}) mean={e:.4f} nonzero_blocks={(w > 1e-12).sum()}/64")
# 看最佳窗口内逐块能量
e, bi, bj = cand[0]
w = flat_eng[bi:bi + 8, bj:bj + 8]
print("best window energy matrix (rounded):")
print(np.round(w, 3))
