"""快速质量扫描：看每类内部 L 与步长随 Q 的变化关系。"""
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import codec_exp as ce
import correlation as cr

img = ce.synthesize_test_image()
h, w = img.shape
nb_h, nb_w = h // 8, w // 8
base = ce.encode_decode(img, quality=50, aq_strength=1.0)
labels = cr.classify_blocks(base["activity"])
print("class block counts:", {n: int((labels == c).sum()) for c, n in enumerate(cr.CLASS_NAMES)})

for q in (95, 80, 65, 50, 35, 20, 10):
    out = ce.encode_decode(img, quality=q, aq_strength=1.0)
    resid = out["recon_u8"].astype(np.float64) - img
    rb = resid.reshape(nb_h, 8, nb_w, 8).transpose(0, 2, 1, 3).reshape(-1, 8, 8)
    row = [f"Q={q:3d}"]
    for c in range(3):
        idx = labels == c
        cl = cr.correlation_length(cr.block_acf(rb[idx]))
        s = cr.effective_step(out["steps"][idx])
        row.append(f"{cr.CLASS_NAMES[c]:7s} s={s:7.2f} Lenv={cl['L_env'] if np.isnan(cl['L_env']) else round(cl['L_env'],2):>5} "
                   f"Lef={cl['L_efold'] if np.isnan(cl['L_efold']) else round(cl['L_efold'],2):>5} "
                   f"Lint={cl['L_int']:.2f} r1={cl['curve'][1]:.3f}")
    print("  ".join(row))
