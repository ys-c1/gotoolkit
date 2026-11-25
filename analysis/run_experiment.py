"""主实验：四档块长 x 三组图，测体积与方块感；交叉校验 Go/Python 重建。"""
import csv
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import numpy as np

from common import block_means, blockiness, read_pgm, reconstruct

ROOT = os.path.join(os.path.dirname(__file__), "..")
GTK = os.path.join(ROOT, "build", "gtk")
IMG_DIR = os.path.join(ROOT, "artifacts", "images")
WORK = os.path.join(ROOT, "artifacts", "work")
OUT_CSV = os.path.join(ROOT, "artifacts", "results.csv")

BLOCKS = [2, 4, 8, 16]
IMAGES = [
    ("img1_gradient.pgm", "渐变场"),
    ("img2_shapes.pgm", "分块几何"),
    ("img3_texture.pgm", "周期纹理"),
]


def run(*args: str) -> None:
    subprocess.run([GTK, *args], check=True, capture_output=True, text=True)


def main() -> None:
    os.makedirs(WORK, exist_ok=True)
    rows = []
    for fname, label in IMAGES:
        src = os.path.join(IMG_DIR, fname)
        original = read_pgm(src)
        stem = fname.removesuffix(".pgm")
        for b in BLOCKS:
            enc_path = os.path.join(WORK, f"{stem}_b{b}.gtbk")
            dec_path = os.path.join(WORK, f"{stem}_b{b}_dec.pgm")
            run("encode", "--format", "gtbk", "--block", str(b),
                "--in", src, "--out", enc_path)
            run("decode", "--format", "gtbk",
                "--in", enc_path, "--out", dec_path)
            volume = os.path.getsize(enc_path)
            decoded = read_pgm(dec_path)

            # 校验 1：GTBK 增强层（残差位打包）应完全抵消块均值量化，
            # 即 Go 解码图与原图逐像素一致（本数据残差均落在 [-127,127]）。
            if not np.array_equal(decoded, original):
                diff = int(np.abs(
                    decoded.astype(np.int16) - original.astype(np.int16)).max())
                raise AssertionError(
                    f"{fname} B={b} Go 解码非无损，最大误差 {diff}")

            # 校验 2：Go 重建出的块均值网格，必须等于 Python 独立实现的块均值。
            h, w = original.shape
            nbh, nbw = h // b, w // b
            go_grid = decoded[: nbh * b, : nbw * b].reshape(
                nbh, b, nbw, b).mean(axis=(1, 3))
            py_grid = block_means(original, b).astype(np.float64)
            if not np.allclose(go_grid, py_grid, atol=0.5):
                raise AssertionError(
                    f"{fname} B={b} Go/Python 块均值网格不一致")

            # 方块感在块均值基底层上测量：这是 GTBK 产生块效应的唯一来源，
            # 增强层只补块内细节、不改变块间不连续的定义。
            base = reconstruct(py_grid.astype(np.int16), b)
            q, qv, qh = blockiness(base.astype(np.uint8), b)
            # 对照：同一批边界在（无损的）解码图上的跳变，仅含自然边缘能量。
            q_dec, _, _ = blockiness(decoded, b)
            rows.append({
                "image": fname,
                "label": label,
                "block": b,
                "volume_bytes": volume,
                "blockiness_gray": q,
                "blockiness_v": qv,
                "blockiness_h": qh,
                "boundary_jump_decoded_gray": q_dec,
            })
            print(f"{label} B={b:2d}  V={volume:7d} B  Q={q:7.3f} 灰度 "
                  f"(垂直 {qv:.3f} / 水平 {qh:.3f})  解码图对照 {q_dec:.3f}")

    with open(OUT_CSV, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)
    print(f"\n写出 {OUT_CSV}（{len(rows)} 行）")


if __name__ == "__main__":
    main()
