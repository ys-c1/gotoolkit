"""抖动矩阵周期—主频—可见度实验的核心计算模块。

约定
----
* 空间频率两套单位:
    - f_pix  : cycles / pixel (数字图平面)
    - f_ang  : cycles / degree (视网膜角频率, cpd)
    换算: f_ang = f_pix * ppd, ppd = 每度像素数
* 观察条件: 像素节距 p (米), 观察距离 D (米)
    ppd = D * tan(1°) / p
* 眼睛光学: 3 mm 瞳孔、波长 550 nm 的衍射受限 MTF
* 视觉敏感度: CSF S(f) = Smax * (f/f0)^a * exp(-(f/f0)^b)
  取 Smax=150, f0=4.5 cpd, a=1.0, b=1.4, 阈值对比度 C_thr = 1/S
"""
from __future__ import annotations

import numpy as np

# ---------------------------------------------------------------- 观察条件
PIXEL_PITCH_M = 0.265e-3        # 0.265 mm, 约 96 dpi 桌面显示器
WAVELENGTH_M = 550e-9
PUPIL_M = 3e-3
TAN1 = np.tan(np.deg2rad(1.0))

# ---------------------------------------------------------------- CSF 参数
# 工程简化 CSF (Kelly 型 S = a f exp(-b f) 的归一化形式):
#   S(f) = Smax * x * exp(1 - x),  x = f / f0
# 峰值在 f=f0 处取 Smax; S>=1 的高频截止约 36 cpd, 与 Barten 数据同量级。
S_MAX = 150.0                   # 峰值对比敏感度 (f=f0)
F0_CPD = 4.5                    # 峰值频率 cpd
S_FLOOR = 1.0                   # 敏感度下限: S<1 意味着阈值超过 100% 对比度, 物理不可见

# 视觉敏感区按 CSF 直接定义: S(f) >= S_BAND_MIN (即阈值对比度 <= 1/S_BAND_MIN)
S_BAND_MIN = 20.0


# ================================================================ 1. 矩阵
def raster_matrix(T: int) -> np.ndarray:
    """行优先 (raster order) 阈值矩阵, T×T, 值域 [0, T^2)。

    阈值随行号线性增长, 对均匀灰阶抖动后产生周期恰好为 T 的
    水平条带 (50% 灰时为 50% 占空比方波), 频谱基频严格位于 1/T。
    """
    return np.arange(T * T, dtype=np.int64).reshape(T, T)


def dither_raster(T: int, gray: float = 0.5, N: int = 1024) -> np.ndarray:
    """用周期 T 的 raster 矩阵抖动均匀灰阶, 返回 0/1 的 N×N 图。"""
    mat = raster_matrix(T)
    thresholds = (mat + 0.5) / (T * T)
    ii, jj = np.meshgrid(np.arange(N), np.arange(N), indexing="ij")
    tile = thresholds[ii % T, jj % T]
    return (gray >= tile).astype(np.float64)


# ================================================================ 2. 频谱
def fft_spectrum(img: np.ndarray):
    """返回 (径向频率 cy/px, 径向平均幅度谱, 完整幅度谱, 频率网格)。"""
    n = img.shape[0]
    spec = np.fft.fftshift(np.fft.fft2(img - img.mean()))
    mag = np.abs(spec)
    mag[n // 2, n // 2] = 0.0                      # 去 DC
    f = np.fft.fftshift(np.fft.fftfreq(n))
    fx, fy = np.meshgrid(f, f, indexing="ij")
    fr = np.sqrt(fx**2 + fy**2)
    df = 1.0 / n
    edges = np.arange(0.0, 0.5 + 1.5 * df, df)
    sums, _ = np.histogram(fr, bins=edges, weights=mag)
    cnt, _ = np.histogram(fr, bins=edges)
    radial = sums / np.maximum(cnt, 1)
    centers = 0.5 * (edges[:-1] + edges[1:])
    return centers, radial, mag, fr


def dominant_frequency(img: np.ndarray) -> float:
    """径向平均幅度谱主峰对应的频率 (cy/px), 原生 FFT 分辨率。"""
    centers, radial, _, _ = fft_spectrum(img)
    return float(centers[int(np.argmax(radial))])


def dominant_frequency_fine(img: np.ndarray, zp: int = 16) -> float:
    """零填充 1D FFT 的高精度主频 (cy/px), 频率分辨率提升 zp 倍。

    本实验的条纹图案只沿行方向变化, 全部谱能量位于纵轴, 因此对列平均
    剖面做 1D FFT 与完整 2D FFT 完全等价, 且无 2D 零填充的内存开销。
    """
    n = img.shape[0]
    profile = img.mean(axis=1) - img.mean()
    nz = n * zp
    spec = np.fft.fftshift(np.fft.fft(profile, n=nz))
    mag = np.abs(spec)
    mag[nz // 2] = 0.0
    f = np.fft.fftshift(np.fft.fftfreq(nz))
    return float(abs(f[int(np.argmax(mag))]))   # 实信号频谱正负对称, 取绝对值


# ================================================================ 3. 观察几何
def ppd(D_m: float, pitch_m: float = PIXEL_PITCH_M) -> float:
    """每度像素数: 距离 D 处, 1° 视角覆盖的像素数。"""
    return D_m * TAN1 / pitch_m


# ================================================================ 4. 眼睛光学 MTF
def _diffraction_mtf(f_ang: np.ndarray, pupil: float = PUPIL_M,
                     lam: float = WAVELENGTH_M) -> np.ndarray:
    """衍射受限圆形孔径 MTF (无像差)。

    ν_c = p/(λ d) 为衍射截止角频率 (cycles/radian), 输入 f 以 cpd 计。
    """
    nu_c = pupil / lam                              # cycles / radian
    nu_c_cpd = nu_c * np.deg2rad(1.0)              # cycles / degree
    v = np.clip(f_ang / nu_c_cpd, 0.0, 1.0)
    return (2.0 / np.pi) * (np.arccos(v) - v * np.sqrt(1.0 - v**2))


def eye_mtf(f_pix: np.ndarray, D_m: float) -> np.ndarray:
    """在给定观察距离下, 对图平面频率 (cy/px) 的眼睛 MTF。"""
    return _diffraction_mtf(f_pix * ppd(D_m))


# ================================================================ 5. 视网膜成像与对比度
def retinal_image(img: np.ndarray, D_m: float) -> np.ndarray:
    """模拟眼睛光学后的视网膜图 (与输入同尺寸, 已去均值)。"""
    n = img.shape[0]
    spec = np.fft.fftshift(np.fft.fft2(img - img.mean()))
    f = np.fft.fftshift(np.fft.fftfreq(n))
    fx, fy = np.meshgrid(f, f, indexing="ij")
    fr = np.sqrt(fx**2 + fy**2)
    mtf = eye_mtf(fr, D_m)
    out = np.fft.ifft2(np.fft.ifftshift(spec * mtf))
    return np.real(out)


def rms_contrast(img: np.ndarray) -> float:
    """整图 RMS 对比度: sigma(I), 输入归一化到 [0,1]。"""
    return float(np.std(img))


def michelson_contrast(img: np.ndarray) -> float:
    """整图 Michelson 对比度 (Imax-Imin)/(Imax+Imin)。"""
    hi, lo = float(img.max()), float(img.min())
    if hi + lo == 0:
        return 0.0
    return (hi - lo) / (hi + lo)


# ================================================================ 6. CSF 与阈值
def csf_sensitivity(f_ang: np.ndarray) -> np.ndarray:
    """对比敏感度函数 S(f), f 单位 cpd, 归一化 Kelly 型。"""
    f = np.asarray(f_ang, dtype=np.float64)
    safe = np.maximum(f, 1e-9)
    x = safe / F0_CPD
    return S_MAX * x * np.exp(1.0 - x)


def csf_threshold(f_ang: np.ndarray) -> np.ndarray:
    """CSF 阈值对比度 C_thr(f) = 1/S(f), 钳在物理上限 1.0 (100% 对比度)。"""
    s = np.maximum(csf_sensitivity(f_ang), S_FLOOR)
    return 1.0 / s


def _brentq_bisect(g, lo: float, hi: float, tol: float = 1e-10) -> float:
    """对单调根做二分求根 (g(lo)<0<g(hi) 或反向), 避免引入 scipy 依赖。"""
    glo, ghi = g(lo), g(hi)
    assert glo * ghi < 0, "端点未夹住根"
    for _ in range(200):
        mid = 0.5 * (lo + hi)
        gm = g(mid)
        if abs(gm) < tol or 0.5 * (hi - lo) < tol:
            return mid
        if gm * glo < 0:
            hi, ghi = mid, gm
        else:
            lo, glo = mid, gm
    return 0.5 * (lo + hi)


def sensitive_band(s_min: float = S_BAND_MIN) -> tuple[float, float]:
    """数值求解 S(f) = s_min 的两个根, 返回敏感带 (f_lo, f_hi), 单位 cpd。"""
    def g(f: float) -> float:
        return float(csf_sensitivity(np.array([f]))[0]) - s_min

    f_lo = _brentq_bisect(g, 1e-4, F0_CPD)
    f_hi = _brentq_bisect(g, F0_CPD, 200.0)
    return f_lo, f_hi


def threshold_period(D_m: float, f_band_cpd: float) -> float:
    """敏感带边界 f_band 对应的阈值周期 (像素): T* = ppd / f_band。"""
    return ppd(D_m) / f_band_cpd


# ================================================================ 7. 整行实验数据
def measure_row(T: int, D_m: float, N: int = 1024, gray: float = 0.5) -> dict:
    """一个 (周期 T, 距离 D) 组合的完整测量行。"""
    img = dither_raster(T, gray=gray, N=N)
    f_pix = dominant_frequency_fine(img)
    f_ang = f_pix * ppd(D_m)

    raw_rms = rms_contrast(img)
    raw_mich = michelson_contrast(img)
    ret = retinal_image(img, D_m)
    vis_rms = rms_contrast(ret)                      # 可见度指标: 视网膜整图 RMS 对比度

    c_thr = float(csf_threshold(np.array([f_ang]))[0])
    margin = vis_rms - c_thr                        # 逐档差值 (一个数)
    ratio = vis_rms / c_thr
    f_lo, f_hi = sensitive_band()
    in_band = bool(f_lo <= f_ang <= f_hi)

    return {
        "T_px": T,
        "D_m": D_m,
        "ppd": ppd(D_m),
        "f_pix_cyc_per_px": f_pix,
        "f_ang_cpd": f_ang,
        "raw_rms": raw_rms,
        "raw_michelson": raw_mich,
        "visibility_rms": vis_rms,
        "csf_threshold": c_thr,
        "margin_V_minus_thr": margin,
        "visibility_over_threshold": ratio,
        "in_sensitive_band": in_band,
    }
