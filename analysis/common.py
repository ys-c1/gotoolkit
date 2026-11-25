"""共享工具：P5 PGM 读写、方块感指标、块重建。"""
import numpy as np


def write_pgm(path, img: np.ndarray) -> None:
    """写 P5 二进制 PGM（单字节灰度）。"""
    h, w = img.shape
    with open(path, "wb") as f:
        f.write(f"P5\n{w} {h}\n255\n".encode("ascii"))
        f.write(img.astype(np.uint8).tobytes())


def read_pgm(path: str) -> np.ndarray:
    """读 P5 PGM，兼容本仓库 gtk 写出的头格式。"""
    with open(path, "rb") as f:
        data = f.read()
    if data[:2] != b"P5":
        raise ValueError(f"不是 P5 PGM: {path}")
    i = 2

    def next_int():
        nonlocal i
        while i < len(data) and data[i] in b" \t\n\r":
            i += 1
        start = i
        while i < len(data) and 48 <= data[i] <= 57:
            i += 1
        return int(data[start:i])

    w, h, maxv = next_int(), next_int(), next_int()
    i += 1  # 跳过头与数据之间的单个分隔符
    pix = np.frombuffer(data[i:i + w * h], dtype=np.uint8).reshape(h, w)
    if maxv != 255:
        raise ValueError(f"仅支持 maxval=255，实际 {maxv}")
    return pix.copy()


def block_means(img: np.ndarray, block: int) -> np.ndarray:
    """B×B 块均值，四舍五入到整数灰度，返回块网格（不含边缘不足块）。"""
    h, w = img.shape
    nbh, nbw = h // block, w // block
    crop = img[: nbh * block, : nbw * block]
    grid = crop.reshape(nbh, block, nbw, block).mean(axis=(1, 3))
    return np.rint(grid).astype(np.int16)


def reconstruct(grid: np.ndarray, block: int) -> np.ndarray:
    """把块均值网格放大回全分辨率的分段常数图。"""
    return np.repeat(np.repeat(grid, block, axis=0), block, axis=1)


def blockiness(decoded: np.ndarray, block: int) -> tuple[float, float, float]:
    """方块感 Q：相邻块边界两侧灰度跳变绝对值的一阶矩，单位灰度级。

    Q = ( Σ_垂直边界 Σ_y |d[y,kB-1]-d[y,kB]|
         + Σ_水平边界 Σ_x |d[kB-1,x]-d[kB,x]| ) / 边界像素对总数

    同时返回垂直、水平两个分量（同为灰度级）。
    """
    h, w = decoded.shape
    nbh, nbw = h // block, w // block
    d = decoded.astype(np.int16)
    v_sum = sum(
        float(np.abs(d[:, kB - 1] - d[:, kB]).sum())
        for kB in range(block, nbw * block, block)
    )
    h_sum = sum(
        float(np.abs(d[kB - 1, :] - d[kB, :]).sum())
        for kB in range(block, nbh * block, block)
    )
    n_v = nbh * block * (nbw - 1)      # 垂直边界上的像素对数
    n_h = nbw * block * (nbh - 1)      # 水平边界上的像素对数
    qv = v_sum / n_v
    qh = h_sum / n_h
    return (v_sum + h_sum) / (n_v + n_h), qv, qh
