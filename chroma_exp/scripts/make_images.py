"""生成三组受控 RGB 图像（128x128，满幅，无截断）。

组 A：分段线性梯度（恒定向量梯度，用于严格验证线性律）
组 B：平滑非线性（低阶曲率，用于验证 Taylor 余项与误差界）
组 C：自然类合成图（边缘+高斯斑+高频纹理，用于验证窗宽权衡）

图像同时存 PPM(P6，便于与仓库 PGM 编解码风格对齐) 与 npy。
所有计算在 float64 下进行，数值钳制仅在最后保存字节时发生。
"""
import os
import sys
import numpy as np

W = H = 128


def _grid():
    x = np.arange(W, dtype=np.float64)
    y = np.arange(H, dtype=np.float64)
    xx, yy = np.meshgrid(x, y)  # xx 列方向(x)，yy 行方向(y)
    return xx, yy


def group_a_linear():
    """每通道一个 2D 线性平面 f = a*x + b*y + c，梯度处处恒定。"""
    xx, yy = _grid()
    # 系数选成覆盖整幅仍落在 [16,240]，避免钳制
    R = 40.0 + 0.55 * xx + 0.35 * yy
    G = 70.0 + 0.70 * xx + 0.50 * yy
    B = 90.0 + 0.45 * xx + 0.60 * yy
    return np.stack([R, G, B], axis=-1)


def group_b_smooth():
    """低频正弦 + 弱二次项，二阶导数有界但非零。"""
    xx, yy = _grid()
    nx, ny = xx / (W - 1), yy / (H - 1)
    R = 128.0 + 55.0 * np.sin(2 * np.pi * 0.9 * nx) * np.cos(2 * np.pi * 0.6 * ny)
    G = 128.0 + 60.0 * np.sin(2 * np.pi * 1.3 * nx + 0.7) + 12.0 * (nx - 0.5) ** 2
    B = 128.0 + 50.0 * np.cos(2 * np.pi * 1.1 * ny) + 18.0 * (ny - 0.5) * (nx - 0.5)
    return np.stack([R, G, B], axis=-1)


def group_c_natural():
    """高斯斑 + 软边缘 + 局部高频纹理，模拟自然图的多尺度结构。"""
    xx, yy = _grid()

    def gauss(cx, cy, sx, sy, amp):
        return amp * np.exp(-(((xx - cx) / sx) ** 2 + ((yy - cy) / sy) ** 2) / 2.0)

    base = np.full((H, W), 110.0)
    R = base + gauss(40, 50, 18, 22, 90.0) - gauss(95, 85, 14, 14, 55.0)
    G = base + 25.0 + gauss(70, 60, 26, 20, 80.0) + gauss(35, 95, 12, 12, 45.0)
    B = base - 10.0 + gauss(90, 40, 20, 16, 70.0)
    # 软边缘（sigmoid 型）
    edge = 1.0 / (1.0 + np.exp(-(xx - 64) / 4.0))
    R += 40.0 * edge
    G += 25.0 * (1.0 - edge)
    # 右下角 32x32 高频棋盘纹理（仅 G、B，制造可辨识的高频色度）
    tex = np.where(((xx // 4) + (yy // 4)) % 2 == 0, 18.0, -18.0)
    mask = ((xx >= 96) & (yy >= 96)).astype(np.float64)
    G += tex * mask
    B -= tex * mask
    return np.stack([R, G, B], axis=-1)


GROUPS = {
    "A_linear": group_a_linear,
    "B_smooth": group_b_smooth,
    "C_natural": group_c_natural,
}


def write_ppm(path, img):
    arr = np.clip(np.rint(img), 0, 255).astype(np.uint8)
    header = f"P6\n{W} {H}\n255\n".encode()
    with open(path, "wb") as f:
        f.write(header)
        f.write(arr.tobytes())


def main(out_dir):
    os.makedirs(out_dir, exist_ok=True)
    for name, fn in GROUPS.items():
        img = fn()
        assert img.shape == (H, W, 3)
        rng = img.reshape(-1, 3).min(axis=0), img.reshape(-1, 3).max(axis=0)
        np.save(os.path.join(out_dir, name + ".npy"), img)
        write_ppm(os.path.join(out_dir, name + ".ppm"), img)
        print(f"{name}: shape={img.shape} min={rng[0].round(2)} max={rng[1].round(2)} "
              f"clipped={int((img<0).sum()+(img>255).sum())}")


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "..", "data"))
