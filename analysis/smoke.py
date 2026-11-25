"""冒烟测试：单次编解码 + 三类区域相关统计。"""
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import codec_exp as ce
import correlation as cr


def main():
    img = ce.synthesize_test_image()
    print("img", img.shape, img.min(), img.max())
    out = ce.encode_decode(img, quality=50, aq_strength=1.0)
    print("blocks", out["steps"].shape[0], "recon range", out["recon"].min(), out["recon"].max())
    resid = out["recon_u8"].astype(np.float64) - img
    print("resid rms", np.sqrt(np.mean(resid**2)))

    labels = cr.classify_blocks(out["activity"])
    rb = resid.reshape(64, 8, 64, 8).transpose(0, 2, 1, 3).reshape(-1, 8, 8)
    for c, name in enumerate(cr.CLASS_NAMES):
        idx = labels == c
        acf = cr.block_acf(rb[idx])
        cl = cr.correlation_length(acf)
        print(name, "n=", idx.sum(),
              "mult=%.3f" % out["mult"][idx].mean(),
              "s_eff=%.2f" % cr.effective_step(out["steps"][idx]),
              "rms=%.3f" % cr.block_rms_noise(rb[idx]),
              "L_env=%.3f" % cl["L_env"], "L_efold=%.3f" % cl["L_efold"],
              "L_int=%.3f" % cl["L_int"], "L_zero=%.3f" % cl["L_zero"],
              "centr=%.3f" % cr.spectral_centroid(out["qcoeffs"][idx]),
              "ACzero=%.3f" % cr.ac_zero_fraction(out["qcoeffs"][idx]))
        print("   acf h", np.round(acf[0], 4))
        print("   acf v", np.round(acf[1], 4))


if __name__ == "__main__":
    main()
