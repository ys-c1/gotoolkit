"""测试图合成与块 DCT + 自适应量化（AQ）有损编解码器。

仅依赖 numpy。所有计算在 float64 下进行，最终像素 round + clip 到 uint8。
图像尺寸固定为 8 的倍数，块（block）是全部分析的基本单元。
"""
from __future__ import annotations

import numpy as np

BLOCK = 8
RNG_SEED = 20261126


# --------------------------------------------------------------------------- #
# 测试图：一张 512x512 灰度图，含三类典型区域（平坦 / 边缘 / 纹理）
# --------------------------------------------------------------------------- #
def synthesize_test_image(size: int = 512) -> np.ndarray:
    """合成含平坦、边缘、纹理三类区域的灰度图（float64，范围 0..255）。

    布局（四个象限拼接，分类时按块的实测 AC 能量重新分三类，不依赖几何位置）：
      左上 平坦渐变；       右上 斜向边缘；
      左下 规则+随机纹理；  右下 混合区（边缘穿越纹理）
    """
    rng = np.random.default_rng(RNG_SEED)
    h = size
    img = np.zeros((h, h), dtype=np.float64)
    g = np.linspace(-1.0, 1.0, h, dtype=np.float64)

    # 左上：平坦区 = 常量 + 大尺度平滑随机场（长相关、平稳）+ 极缓斜坡。
    # 不用纯常量：纯常量在细步长下残差恒为 0，ACF 无定义；
    # 也不用强确定性斜坡：有限窗内样本 ACF 会出现趋势伪影（r(k)<-1）。
    flat_field = rng.standard_normal((h // 2, h // 2))
    # 9x9 平滑核：相关长度仍达数块（保证残差空间连片），又使每个 8x8 块内
    # 有可被量化的低频起伏，避免大多数块量化后残差恒为零。
    k_wide = np.ones(9) / 9.0

    def box_wide(a: np.ndarray) -> np.ndarray:
        a = np.apply_along_axis(lambda m: np.convolve(m, k_wide, mode="same"), 0, a)
        a = np.apply_along_axis(lambda m: np.convolve(m, k_wide, mode="same"), 1, a)
        return a

    flat_field = box_wide(box_wide(flat_field))
    flat_field /= flat_field.std()
    # 幅度 14：显著低于纹理区，三分位分类保持「平坦」；
    # 分析使用 uint8 舍入前的浮点残差，故无需靠幅度越过舍入阈值。
    flat = 128.0 + 14.0 * flat_field + 3.0 * g[: h // 2, None] + 2.0 * g[None, : h // 2]
    img[: h // 2, : h // 2] = flat

    # 右上：两条平滑斜边缘（tanh 过渡带，约 6~10px 宽）
    yy, xx = np.meshgrid(np.arange(h), np.arange(h), indexing="ij")
    edge1 = 90.0 * np.tanh((xx - yy - 60.0) / 4.0)
    edge2 = 60.0 * np.tanh((xx + yy - 640.0) / 3.0)
    img[: h // 2, h // 2 :] = 128.0 + edge1[: h // 2, h // 2 :] + edge2[: h // 2, h // 2 :]

    # 左下：纹理 = 多频多向正弦（含与 8px 块不对齐的周期）+ 短相关平滑随机场。
    # 不使用棋盘阶跃：其量化误差的周期振荡会主导样本 ACF，掩盖噪声本征结构。
    xx2 = xx[h // 2 :, : h // 2].astype(np.float64)
    yy2 = yy[h // 2 :, : h // 2].astype(np.float64)
    sine = (18.0 * np.sin(2 * np.pi * xx2 / 9.0 + 0.7)
            + 12.0 * np.sin(2 * np.pi * (xx2 + yy2) / 13.0)
            + 9.0 * np.sin(2 * np.pi * yy2 / 6.0 + 1.3)
            + 6.0 * np.sin(2 * np.pi * (xx2 - 2 * yy2) / 19.0))
    field = rng.standard_normal((h // 2, h // 2))
    k = np.ones(3) / 3.0

    def box(a: np.ndarray) -> np.ndarray:
        a = np.apply_along_axis(lambda m: np.convolve(m, k, mode="same"), 0, a)
        a = np.apply_along_axis(lambda m: np.convolve(m, k, mode="same"), 1, a)
        return a

    for _ in range(2):
        field = box(field)
    field /= field.std()
    img[h // 2 :, : h // 2] = 128.0 + sine + 12.0 * field

    # 右下：纹理基底上叠加一条强边缘（混合区，归入边缘/纹理之间）
    base = img[h // 2 :, : h // 2].copy()
    edge3 = 70.0 * np.tanh((xx[h // 2 :, h // 2 :].astype(np.float64)
                            - yy[h // 2 :, h // 2 :].astype(np.float64) - 40.0) / 3.5)
    img[h // 2 :, h // 2 :] = base + edge3

    return np.clip(img, 0.0, 255.0)


def to_uint8(img: np.ndarray) -> np.ndarray:
    return np.clip(np.rint(img), 0, 255).astype(np.uint8)


# --------------------------------------------------------------------------- #
# 块 DCT（正交 DCT-II，8x8），纯矩阵实现
# --------------------------------------------------------------------------- #
def dct_matrix(n: int = BLOCK) -> np.ndarray:
    """正交 DCT-II 基矩阵，行 k 为第 k 个频率基，C @ x 为正变换。"""
    k = np.arange(n)[:, None]
    m = np.arange(n)[None, :]
    c = np.cos(np.pi * (2 * m + 1) * k / (2 * n))
    c *= np.sqrt(2.0 / n)
    c[0, :] = 1.0 / np.sqrt(n)
    return c


_C = dct_matrix()


def block_dct(blocks: np.ndarray) -> np.ndarray:
    """blocks: (..., 8, 8) -> DCT 系数，同形状。"""
    return _C @ blocks @ _C.T


def block_idct(coeffs: np.ndarray) -> np.ndarray:
    return _C.T @ coeffs @ _C


# --------------------------------------------------------------------------- #
# 自适应量化（AQ）
# --------------------------------------------------------------------------- #
# 标准 JPEG Annex K 亮度量化表（base Q=50 的基准表）
JPEG_LUM_Q50 = np.array([
    [16, 11, 10, 16, 24, 40, 51, 61],
    [12, 12, 14, 19, 26, 58, 60, 55],
    [14, 13, 16, 24, 40, 57, 69, 56],
    [14, 17, 22, 29, 51, 87, 80, 62],
    [18, 22, 37, 56, 68, 109, 103, 77],
    [24, 35, 55, 64, 81, 104, 113, 92],
    [49, 64, 78, 87, 103, 121, 120, 101],
    [72, 92, 95, 98, 112, 100, 103, 99],
], dtype=np.float64)


def base_quant_table(quality: int) -> np.ndarray:
    """按 JPEG 惯例由 quality(1..100) 缩放基准表。"""
    q = np.clip(quality, 1, 100)
    if q < 50:
        scale = 5000.0 / q
    else:
        scale = 200.0 - 2.0 * q
    table = np.floor((JPEG_LUM_Q50 * scale + 50.0) / 100.0)
    return np.clip(table, 1.0, 255.0)


def block_activity(blocks: np.ndarray) -> np.ndarray:
    """块活动度：DCT 交流系数能量（去均值后的块内方差），形状 (nb,)。"""
    d = block_dct(blocks)
    ac = d.copy()
    ac[..., 0, 0] = 0.0
    return np.mean(ac * ac, axis=(-2, -1))


def aq_multipliers(activity: np.ndarray, strength: float = 1.0) -> np.ndarray:
    """AQ 乘子 m_b：等 SNR 型自适应量化，活动度越高（纹理越强）步长越粗。

    m_b = clip(exp(+strength * z(log activity)), m_min, m_max)，z 为标准化。
    平坦块 m<1（细量化、步长小），纹理块 m>1（粗量化、步长大）。
    这与「心理视觉优化」方向相反：后者对平坦区给粗步长；本实验需要可检验的
    局部步长差异，故采用与局部信号尺度同向的等 SNR 分配。
    """
    log_a = np.log(activity + 1e-12)
    z = (log_a - log_a.mean()) / (log_a.std() + 1e-12)
    m = np.exp(strength * z)
    # 下限 0.7：保证平坦块仍得到最小步长，同时在中等质量档有稳定可观测残差，
    # 避免大量平坦块完全无损导致误差图与 ACF 失去样本。
    return np.clip(m, 0.7, 2.0)


def deadzone_quantize(coeff: np.ndarray, step: np.ndarray) -> np.ndarray:
    """死区均匀量化（JPEG 风格 round）：q = round(c / s)。"""
    return np.round(coeff / step)


def encode_decode(img: np.ndarray, quality: int, aq_strength: float = 1.0):
    """完整编解码，返回重建图、每块实际步长表、每块活动度与 AQ 乘子。

    返回 dict：
      recon       (H, W) float64 重建图（未做最终 uint8 裁剪前）
      recon_u8    (H, W) uint8
      steps       (nb, 8, 8) 每块每系数的实际量化步长 m_b * Q[k,l]
      activity    (nb,) 每块 AC 能量
      mult        (nb,) AQ 乘子
      blocks      (nb, 8, 8) 原始块
    """
    h, w = img.shape
    assert h % BLOCK == 0 and w % BLOCK == 0
    nb_h, nb_w = h // BLOCK, w // BLOCK
    blocks = img.reshape(nb_h, BLOCK, nb_w, BLOCK).transpose(0, 2, 1, 3)
    blocks = blocks.reshape(-1, BLOCK, BLOCK)

    coeffs = block_dct(blocks)
    activity = block_activity(blocks)
    mult = aq_multipliers(activity, aq_strength)
    qtable = base_quant_table(quality)
    steps = mult[:, None, None] * qtable[None, :, :]

    q = deadzone_quantize(coeffs, steps)
    deq = q * steps
    recon_blocks = block_idct(deq)

    recon = recon_blocks.reshape(nb_h, nb_w, BLOCK, BLOCK).transpose(0, 2, 1, 3)
    recon = recon.reshape(h, w)
    recon_u8 = to_uint8(recon)
    return {
        "recon": recon,
        "recon_u8": recon_u8,
        "steps": steps,
        "activity": activity,
        "mult": mult,
        "blocks": blocks,
        "qtable": qtable,
        "coeffs": coeffs,
        "qcoeffs": q,
    }
