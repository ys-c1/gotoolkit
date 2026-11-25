#!/usr/bin/env python3
"""从 artifacts/hybrid 的数据文件重画四张图（需在仓库根目录运行）。

    python3 tools/charts/charts.py

输入：artifacts/hybrid/{split,checker,scene}_{sweep.tsv,blocks.tsv,summary.json}
输出：artifacts/hybrid/charts/{sweep,volume,sigma,regions}.png
"""
import sys, json, csv, os
_extra = '/home/ys/.local/lib/python3.14/site-packages'
if os.path.isdir(_extra):
    sys.path.insert(0, _extra)
import matplotlib
matplotlib.use('Agg')
import matplotlib.font_manager as fm
import matplotlib.pyplot as plt
import numpy as np

FONT = '/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf'
if os.path.exists(FONT):
    fm.fontManager.addfont(FONT)
plt.rcParams.update({
    'font.family': 'Noto Sans CJK SC', 'font.size': 9,
    'axes.facecolor': '#fcfcfb', 'figure.facecolor': '#fcfcfb',
    'axes.edgecolor': '#c3c2b7', 'axes.labelcolor': '#52514e', 'text.color': '#0b0b0b',
    'xtick.color': '#898781', 'ytick.color': '#898781',
    'axes.grid': False, 'grid.color': '#e1e0d9', 'grid.linewidth': 0.8, 'grid.linestyle': '-',
    'axes.spines.top': False, 'axes.spines.right': False,
    'legend.frameon': False, 'savefig.dpi': 144,
})
BLUE = '#2a78d6'; ORANGE = '#eb6834'; INK = '#0b0b0b'; SUB = '#52514e'; MUTED = '#898781'; GRID = '#e1e0d9'
BASE = 'artifacts/hybrid'
NAMES = [('split', 'Split 左平右纹'), ('checker', 'Checker 棋盘'), ('scene', 'Scene 场景')]


def summary(key):
    return json.load(open(f'{BASE}/{key}_summary.json'))


# ---------- fig 1: sweep, two rows ----------
fig, axes = plt.subplots(2, 3, figsize=(11.5, 6.4))
for col, (key, title) in enumerate(NAMES):
    rows = list(csv.DictReader(open(f'{BASE}/{key}_sweep.tsv'), delimiter='\t'))
    k = np.array([int(r['flat_blocks']) for r in rows])
    tot = np.array([float(r['total']) for r in rows])
    mod = np.array([float(r['total_model']) for r in rows])
    s = summary(key); mk = s['model_k']
    ax = axes[0, col]
    ax.plot(k, mod, color=BLUE, lw=2.0, zorder=3, label='模型')
    ax.plot(k, tot, color=ORANGE, lw=1.4, zorder=4, label='实测')
    ax.scatter([mk], [tot[mk]], s=36, color=INK, zorder=5, edgecolors='#fcfcfb', linewidths=2)
    ax.axvline(mk, color=MUTED, lw=1, zorder=1)
    ax.set_yscale('log')
    ax.set_title(title, color=INK, fontsize=10, pad=6)
    ax.set_xlabel('判为平坦的块数 k', color=SUB, fontsize=8)
    ax.grid(axis='y', color=GRID, lw=0.8); ax.set_axisbelow(True)
    ax.tick_params(labelsize=8)
    ax.annotate(f'k*={mk}\nMSE {tot[mk]:.2f}', (mk, tot[mk]), textcoords='offset points',
                xytext=(10, 10), fontsize=8, color=SUB)
    ax.set_ylim(tot.min() * 0.55, tot.max() * 2.2)
    if col == 0:
        ax.set_ylabel('整幅每像素 MSE（对数轴）', color=SUB, fontsize=8)
        ax.legend(loc='upper left', fontsize=8, ncol=2)
    ax = axes[1, col]
    d_meas = np.diff(tot); d_model = np.diff(mod)
    ax.plot(k[1:], d_model, color=BLUE, lw=2.0, zorder=3, label='模型差分')
    ax.scatter(k[1:], d_meas, s=5, color=ORANGE, zorder=4, label='实测差分')
    ax.axhline(0, color='#c3c2b7', lw=1, zorder=1)
    ax.axvline(mk, color=MUTED, lw=1, zorder=1)
    ax.set_yscale('symlog', linthresh=1e-3)
    ax.set_xlabel('k', color=SUB, fontsize=8)
    ax.grid(axis='y', color=GRID, lw=0.8); ax.set_axisbelow(True)
    ax.tick_params(labelsize=8)
    if col == 0:
        ax.set_ylabel('ΔS(k)=S(k)−S(k−1)', color=SUB, fontsize=8)
        ax.legend(loc='center right', fontsize=8)
fig.suptitle('阈值扫描：模型总失真随 k 先降后升，差分单调递增、只过一次零 → 唯一极小点',
             color=INK, fontsize=11.5, y=0.995)
fig.tight_layout(rect=(0, 0, 1, 0.97))
fig.savefig(f'{BASE}/charts/sweep.png', bbox_inches='tight'); plt.close(fig)

# ---------- fig 2: volume, log scale ----------
fig, axes = plt.subplots(1, 3, figsize=(11, 3.6))
labels = ['PGM', 'BMP', 'RLE', '混合']
for ax, (key, title) in zip(axes, NAMES):
    s = summary(key); v = s['volume']
    vals = [v['pgm single'], v['bmp single'], v['rle single'], v['hybrid']]
    cols = ['#b7d3f6', '#9ec5f4', '#86b6ef', ORANGE]
    ax.bar(range(4), vals, color=cols, width=0.58, zorder=2)
    ax.set_xticks(range(4)); ax.set_xticklabels(labels, fontsize=8.5)
    ax.set_yscale('log')
    ax.set_title(f'{title}\n混合/最小单一 = {v["hybrid"]/v["min_single"]:.3f}', color=INK, fontsize=10, pad=6)
    ax.grid(axis='y', color=GRID, lw=0.8); ax.set_axisbelow(True)
    for i, val in enumerate(vals):
        ax.annotate(f'{val/1024:.1f}', (i, val), textcoords='offset points', xytext=(0, 4),
                    ha='center', fontsize=8, color=SUB)
    ax.tick_params(labelsize=8); ax.set_ylim(8, max(vals) * 4.2)
    ax.set_ylabel('KiB（对数轴）', color=SUB, fontsize=8)
fig.suptitle('体积：混合码流低于任一单一格式', color=INK, fontsize=11.5, y=1.02)
fig.tight_layout(); fig.savefig(f'{BASE}/charts/volume.png', bbox_inches='tight'); plt.close(fig)

# ---------- fig 3: sigma distribution + threshold ----------
fig, axes = plt.subplots(1, 3, figsize=(11, 3.3), sharey=True)
tau = summary('scene')['tau']
for ax, (key, title) in zip(axes, NAMES):
    bl = list(csv.DictReader(open(f'{BASE}/{key}_blocks.tsv'), delimiter='\t'))
    sig = np.array([float(r['sigma']) for r in bl])
    s = summary(key)
    bins = np.logspace(np.log10(0.1), np.log10(60), 34)
    ax.hist(np.clip(sig, 0.1, None), bins=bins, color=BLUE, alpha=0.85, zorder=2)
    ax.axvline(tau, color=ORANGE, lw=2, zorder=3)
    ax.set_xscale('log')
    ax.set_title(f'{title}\n平坦 {s["flat"]["blocks"]} · 纹理 {s["tex"]["blocks"]}',
                 color=INK, fontsize=10, pad=6)
    ax.set_xlabel('块标准差 σ（对数轴）', color=SUB, fontsize=8)
    ax.grid(axis='y', color=GRID, lw=0.8); ax.set_axisbelow(True)
    ax.tick_params(labelsize=8)
    ax.annotate(f'τ={tau:.2f}', (tau, ax.get_ylim()[1] * 0.52), textcoords='offset points',
                xytext=(4, 0), fontsize=8.5, color=ORANGE)
axes[0].set_ylabel('块数', color=SUB, fontsize=8)
fig.suptitle('块 σ 分布与阈值 τ：τ 落在平坦类（左）与纹理类（右）之间的空档',
             color=INK, fontsize=11.5, y=1.04)
fig.tight_layout(); fig.savefig(f'{BASE}/charts/sigma.png', bbox_inches='tight'); plt.close(fig)

# ---------- fig 4: regions ----------
fig, axes = plt.subplots(1, 3, figsize=(11, 2.9), sharex=True)
for ax, (key, title) in zip(axes, NAMES):
    s = summary(key)
    vals = [s['tex']['mse'], s['flat']['mse']]
    ax.barh([1, 0], vals, color=[ORANGE, BLUE], height=0.42, zorder=2)
    ax.set_yticks([1, 0]); ax.set_yticklabels(['纹理区', '平坦区'], fontsize=9)
    ax.set_title(title, color=INK, fontsize=10, pad=6)
    ax.grid(axis='x', color=GRID, lw=0.8); ax.set_axisbelow(True)
    for y, val in zip([1, 0], vals):
        ax.annotate(f'{val:.2f}', (val, y), textcoords='offset points', xytext=(4, 0),
                    va='center', fontsize=8.5, color=SUB)
    ax.set_xlim(0, 28); ax.tick_params(labelsize=8)
axes[0].set_xlabel('逐像素 MSE', color=SUB, fontsize=8)
fig.suptitle('两类区域实测失真（同一横轴）', color=INK, fontsize=11.5, y=1.04)
fig.tight_layout(); fig.savefig(f'{BASE}/charts/regions.png', bbox_inches='tight'); plt.close(fig)
print('ok')
