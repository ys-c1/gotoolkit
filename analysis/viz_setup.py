"""统一引导：隔离模式(-I)下显式接入用户 site-packages，并注册中文字体。

所有绘图/主脚本 import 本模块即可，禁止在各处重复 sys.path 修补。
"""
import glob
import os
import sys

USER_SITE = "/home/ys/.local/lib/python3.14/site-packages"
if USER_SITE not in sys.path:
    sys.path.insert(0, USER_SITE)

import matplotlib  # noqa: E402

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib import font_manager  # noqa: E402

# 注册 Noto Sans CJK（字体文件存在但未进 matplotlib 默认缓存时也能工作）
for path in glob.glob("/home/ys/.local/share/fonts/NotoSansCJKsc-*.ttf"):
    font_manager.fontManager.addfont(path)

plt.rcParams.update({
    "font.family": "sans-serif",
    "font.sans-serif": ["Noto Sans CJK SC", "DejaVu Sans"],
    "axes.unicode_minus": False,
    "figure.dpi": 110,
    "savefig.dpi": 150,
    "axes.edgecolor": "#52514e",
    "axes.labelcolor": "#0b0b0b",
    "text.color": "#0b0b0b",
    "xtick.color": "#52514e",
    "ytick.color": "#52514e",
    "axes.grid": True,
    "grid.color": "#e1e0d9",
    "grid.linewidth": 0.7,
    "axes.spines.top": False,
    "axes.spines.right": False,
    "font.size": 11,
})

# dataviz 技能参考配色（已通过 validate_palette.js，三色 all-pairs 安全）
COLORS = {"flat": "#2a78d6", "edge": "#eb6834", "texture": "#1baf7a"}
CLASS_CN = {"flat": "平坦区", "edge": "边缘区", "texture": "纹理区"}
SURFACE = "#fcfcfb"
INK_MUTED = "#898781"
