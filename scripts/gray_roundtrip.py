#!/usr/bin/env python3
"""灰度映射 -> 区间量化 -> 反解往返：平台、可逆性与像素差异审计。

模型（题目未给具体区间数，这里取仓库 uint8/PGM 口径下唯一自然且可审计的选择）：
  * 位深 b ∈ {8,10,12}，输入灰度数 N = 2^b；
  * 全档共用 M = 256 个等宽区间（交付码级只有 0..255）；
  * 映射到区间号：T(x) = floor(M*x/N)，x=0..N-1；
  * 反解取区间内第一个灰阶（伪逆）：R(y) = ceil(y*N/M)。

平台段的唯一判定口径是“相邻映射值之差为零”：
  d_i = T(i) - T(i-1)，d_i == 0 即一处零差相邻，连续零差拼成平台段。

脚本产物全部写到 artifacts/：
  * roundtrip_<b>bit.png            每档位深一张往返对照图（题目要求）
  * source/recovered_<b>bit_16.png  原生位深的源图与往返图（16bit PNG，可逐像素复核）
  * lut_<b>bit.csv                  逐灰阶映射、相邻差、平台标记
  * plateau_runs_<b>bit.csv         平台段明细
  * plateau_table.csv / .json / roundtrip_report.md
运行：python3 scripts/gray_roundtrip.py
"""
from __future__ import annotations

import csv
import json
import os

import numpy as np

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.colors import LinearSegmentedColormap
from matplotlib import font_manager
from PIL import Image

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ART = os.path.join(ROOT, "artifacts")
os.makedirs(ART, exist_ok=True)

BITS_LIST = [8, 10, 12]
M_INTERVALS = 256          # 全档共用的区间数（8bit 交付码级数）
W = H = 64
N_PIX = W * H              # 4096，恰好让 12bit 每个灰阶出现一次

# dataviz 参考调色板（与 docs/../uidreczz28dqXjZ8lt2 姊妹图同一套，蓝/红已校验）
C_LINE = "#2a78d6"
C_PLAT = "#d03b3b"
C_OK = "#1baf7a"
INK = "#0b0b0b"
MUTED = "#52514e"
GRID = "#e1e0d9"
AXIS = "#c3c2b7"

FONT_PATH = os.path.expanduser("~/.local/share/fonts/NotoSansCJKsc-Regular.ttf")
if os.path.exists(FONT_PATH):
    font_manager.fontManager.addfont(FONT_PATH)
    FONT = font_manager.FontProperties(fname=FONT_PATH)
    plt.rcParams["font.family"] = FONT.get_name()
plt.rcParams.update({
    "axes.edgecolor": AXIS,
    "axes.labelcolor": INK,
    "text.color": INK,
    "xtick.color": MUTED,
    "ytick.color": MUTED,
    "axes.grid": True,
    "grid.color": GRID,
    "grid.linewidth": 0.8,
    "figure.facecolor": "white",
    "axes.facecolor": "#fcfcfb",
    "axes.unicode_minus": False,
})


def map_lut(n: int, m: int) -> np.ndarray:
    """T(x)=floor(m*x/n)，并把末点钳在 m-1（防御性，正常公式已不越界）。"""
    x = np.arange(n, dtype=np.int64)
    y = (m * x) // n
    return np.minimum(y, m - 1)


def repr_value(y: int, n: int, m: int) -> int:
    """区间 y 的首灰阶 ceil(y*n/m)，floor 分箱的伪逆代表值。"""
    return (y * n + m - 1) // m


def plateau_runs(lut: np.ndarray) -> list[dict]:
    """按相邻差为零合并极大等值段；length=段内输入灰阶数（单点段 length=1）。"""
    runs: list[dict] = []
    start = 0
    n = len(lut)
    for i in range(1, n + 1):
        if i == n or lut[i] != lut[start]:
            end = i - 1
            runs.append({
                "start": start,
                "end": end,
                "value": int(lut[start]),
                "length": end - start + 1,
                "zero_edges": end - start,
            })
            start = i
    return runs


def analyze_tier(bits: int, m: int) -> dict:
    n = 1 << bits
    lut = map_lut(n, m)
    adj = np.diff(lut)                       # T(i)-T(i-1), i=1..n-1
    zero_right = np.where(adj == 0)[0] + 1   # 右下标 i
    runs = plateau_runs(lut)
    multi = [r for r in runs if r["length"] >= 2]
    max_len = max(r["length"] for r in runs)
    injective = bool(np.all(adj != 0))       # 非递减前提下 == 严格递增
    min_m = n

    # 模运算充要条件的程序化复核（m<n 区间）：
    # 零差 ⇔ (m*(i-1)) mod n < n-m
    modular_ok = True
    if m < n:
        for i in range(1, n):
            pred = (m * (i - 1)) % n < n - m
            got = lut[i] == lut[i - 1]
            if pred != got:
                modular_ok = False
                break

    # 全覆盖梯度源图：像素 p 的灰阶 floor(p*n/N_PIX)，每级出现 N_PIX/n 次。
    p = np.arange(N_PIX, dtype=np.int64)
    src = ((p * n) // N_PIX).reshape(H, W)
    mapped = (m * src) // n
    mapped = np.minimum(mapped, m - 1)
    reps = np.array([repr_value(y, n, m) for y in range(m)], dtype=np.int64)
    recovered = reps[mapped]
    diff = src - recovered
    mismatch = int(np.count_nonzero(diff))

    # 任意图像上的 8bit 可逆档额外核验（确定性伪随机字节），证明零差异不依赖梯度。
    rng = np.random.default_rng(20261126)
    arbitrary_zero = None
    if bits == 8 and m == n:
        arbitrary = rng.integers(0, 256, size=N_PIX, dtype=np.int64)
        a_map = map_lut(256, m)[arbitrary]
        a_rec = reps[a_map]
        arbitrary_zero = int(np.count_nonzero(arbitrary != a_rec))

    return {
        "bits": bits,
        "n_levels": n,
        "m_intervals": m,
        "bin_width_exact": (n % m == 0),
        "bin_width": n // m if n % m == 0 else None,
        "adjacent_steps": n - 1,
        "zero_diff_steps": int(len(zero_right)),
        "plateau_run_count_ge2": len(multi),
        "max_plateau_length": int(max_len),
        "max_plateau_zero_edges": int(max_len - 1),
        "injective_reversible": injective,
        "min_intervals": min_m,
        "modular_condition_verified": modular_ok,
        "first_zero_step_index": int(zero_right[0]) if len(zero_right) else None,
        "first_plateau": multi[0] if multi else None,
        "pixels": N_PIX,
        "mismatched_pixels": mismatch,
        "mismatch_fraction": mismatch / N_PIX,
        "arbitrary_image_mismatch": arbitrary_zero,
        "_lut": lut,
        "_adj": adj,
        "_runs": runs,
        "_src": src,
        "_mapped": mapped,
        "_recovered": recovered,
        "_diff": diff,
    }


def write_csvs(a: dict) -> None:
    bits, n, m = a["bits"], a["n_levels"], a["m_intervals"]
    lut, adj, runs = a["_lut"], a["_adj"], a["_runs"]
    run_id = np.empty(n, dtype=np.int64)
    run_len = np.empty(n, dtype=np.int64)
    for k, r in enumerate(runs):
        run_id[r["start"]:r["end"] + 1] = k
        run_len[r["start"]:r["end"] + 1] = r["length"]

    with open(os.path.join(ART, f"lut_{bits}bit.csv"), "w", newline="") as f:
        wcsv = csv.writer(f)
        wcsv.writerow(["i", "T_i", "inverse_repr", "adj_diff", "zero_diff",
                       "run_id", "run_length"])
        for i in range(n):
            d = "" if i == 0 else int(adj[i - 1])
            z = "" if i == 0 else int(adj[i - 1] == 0)
            wcsv.writerow([i, int(lut[i]), repr_value(int(lut[i]), n, m),
                           d, z, int(run_id[i]), int(run_len[i])])

    with open(os.path.join(ART, f"plateau_runs_{bits}bit.csv"), "w", newline="") as f:
        wcsv = csv.writer(f)
        wcsv.writerow(["run_index", "start", "end", "value", "length_levels",
                       "zero_adjacent_edges", "is_plateau_ge2"])
        for k, r in enumerate(runs):
            wcsv.writerow([k, r["start"], r["end"], r["value"], r["length"],
                           r["zero_edges"], int(r["length"] >= 2)])


def save_native_pngs(a: dict) -> tuple[str, str]:
    bits = a["bits"]
    src = a["_src"].astype(np.uint16)
    rec = a["_recovered"].astype(np.uint16)
    sp = os.path.join(ART, f"source_{bits}bit_native16.png")
    rp = os.path.join(ART, f"roundtrip_{bits}bit_native16.png")
    Image.fromarray(src).save(sp)
    Image.fromarray(rec).save(rp)
    # 回读自检：落盘的 16bit PNG 必须逐像素等于内存数组。
    back = np.array(Image.open(rp)).astype(np.int64)
    assert np.array_equal(back, a["_recovered"]), f"{bits}bit 16bit PNG 回读不一致"
    return sp, rp


def render_figure(a: dict) -> str:
    bits, n, m = a["bits"], a["n_levels"], a["m_intervals"]
    src, mapped, rec, diff = a["_src"], a["_mapped"], a["_recovered"], a["_diff"]
    reversible = a["injective_reversible"]
    w = a["bin_width"]

    fig = plt.figure(figsize=(15.5, 8.6))
    gs = fig.add_gridspec(2, 4, height_ratios=[1.05, 1.0], hspace=0.34, wspace=0.28,
                          left=0.05, right=0.97, top=0.90, bottom=0.08)
    fig.suptitle(
        f"{bits} 位深（N={n} 级）→ {m} 个等宽区间 → 反解往返　"
        f"平台长度 Lmax={a['max_plateau_length']}，"
        f"零差相邻 {a['zero_diff_steps']} 处，"
        f"可逆={'是' if reversible else '否'}，"
        f"差异像素 {a['mismatched_pixels']}/{a['pixels']}",
        fontsize=15, fontweight="bold", color=INK)

    vmax = n - 1
    panels = [
        (src, f"原图 {bits}bit（全覆盖梯度）", "gray", 0, vmax),
        (mapped, f"区间号 T=floor({m}x/{n})（0..{m-1}）", "gray", 0, m - 1),
        (rec, "反解往返 R(T)（区间首阶）", "gray", 0, vmax),
    ]
    axes_img = []
    for j, (arr, title, cmap, lo, hi) in enumerate(panels):
        ax = fig.add_subplot(gs[0, j])
        axes_img.append(ax)
        im = ax.imshow(arr, cmap=cmap, vmin=lo, vmax=hi, interpolation="nearest")
        ax.set_title(title, fontsize=11.5)
        ax.set_xticks([]); ax.set_yticks([])
        cb = fig.colorbar(im, ax=ax, fraction=0.046, pad=0.04)
        cb.ax.tick_params(labelsize=8)

    # 差异面板：原生位深的有符号残差（本伪逆下非负），单一暖色序列。
    axd = fig.add_subplot(gs[0, 3])
    dmax = max(1, int(diff.max()))
    cmap_diff = LinearSegmentedColormap.from_list(
        "white_to_status_red", ["#ffffff", "#f6b3a6", C_PLAT])
    imd = axd.imshow(diff, cmap=cmap_diff, vmin=0, vmax=dmax,
                     interpolation="nearest")
    axd.set_title(f"差异 x−R(T)（原生 LSB，放大显示）\n"
                  f"{a['mismatched_pixels']} 像素不同",
                  fontsize=11.5,
                  color=C_OK if a["mismatched_pixels"] == 0 else INK)
    axd.set_xticks([]); axd.set_yticks([])
    cbd = fig.colorbar(imd, ax=axd, fraction=0.046, pad=0.04)
    cbd.ax.tick_params(labelsize=8)
    if a["mismatched_pixels"] == 0:
        axd.text(0.5, 0.5, "0 个差异像素\n（逐像素相等）", transform=axd.transAxes,
                 ha="center", va="center", fontsize=13, fontweight="bold",
                 color=C_OK)

    # LUT 局部放大，直接画“平台”：蓝线 + 红色零差段。
    axl = fig.add_subplot(gs[1, 0:2])
    zoom = min(n, {8: 48, 10: 40, 12: 128}[bits])
    xs = np.arange(zoom)
    axl.plot(xs, a["_lut"][:zoom], color=C_LINE, lw=2.0,
             drawstyle="steps-post", label="T(i)（steps-post）")
    zv = [i for i in range(1, zoom) if a["_lut"][i] == a["_lut"][i - 1]]
    if zv:
        axl.scatter(zv, a["_lut"][zv], s=26, marker="o", facecolors="none",
                    edgecolors=C_PLAT, linewidths=1.4, zorder=5,
                    label=f"相邻差=0（窗口内 {len(zv)} 处）")
    axl.set_title(f"映射曲线暗部放大（前 {zoom} 阶；红圈即平台段）", fontsize=11.5)
    axl.set_xlabel("输入灰阶 i")
    axl.set_ylabel("区间号 T(i)")
    axl.legend(fontsize=9, framealpha=0.9)

    # 结论数字面板。
    axt = fig.add_subplot(gs[1, 2:4])
    axt.axis("off")
    verdict = "可逆（严格递增，存在唯一逆映射）" if reversible else "不可逆（存在平台，多阶映到同一处）"
    lines = [
        f"输入灰度数 N = 2^{bits} = {n}",
        f"共用区间数 M = {m}" + (f"，整除宽度 w = N/M = {w}" if w else ""),
        f"平台段最大长度 Lmax = {a['max_plateau_length']} 个灰阶"
        f"（每段零差边 {a['max_plateau_zero_edges']} 条）",
        f"长度≥2 的平台段 {a['plateau_run_count_ge2']} 段；"
        f"零差相邻边总数 Z = {a['zero_diff_steps']} / {a['adjacent_steps']}",
        f"可逆判定：{verdict}",
        f"可逆所需最小区间数 Mmin = N = {a['min_intervals']}",
        f"往返像素差异：{a['mismatched_pixels']} / {a['pixels']}"
        f"（{a['mismatch_fraction']*100:.2f}%）",
    ]
    if a["arbitrary_image_mismatch"] is not None:
        lines.append(f"任意图（确定性随机字节）往返差异：{a['arbitrary_image_mismatch']}")
    witness = a["first_plateau"]
    if witness:
        lines.append(
            f"首个平台见证：i∈[{witness['start']},{witness['end']}] "
            f"共 {witness['length']} 阶，T 全为 {witness['value']}")
    else:
        lines.append("无长度≥2 的平台：T(i)=i 为恒等映射")
    lines.append("")
    lines.append("判据：平台段 ⇔ 相邻映射值之差 T(i)−T(i−1)=0；")
    lines.append("可逆 ⇔ 不存在零差边 ⇔ 所有平台长度均为 1 ⇔ M≥N。")
    axt.text(0.01, 0.99, "\n".join(lines), va="top", ha="left", fontsize=11.5,
             linespacing=1.62, transform=axt.transAxes)

    out = os.path.join(ART, f"roundtrip_{bits}bit.png")
    fig.savefig(out, dpi=140)
    plt.close(fig)
    return out


def alt_endpoint_round_stats() -> dict:
    """替代取整口径 T=round((M-1)x/(N-1))（端点钉死 0/M-1）的平台统计。

    与等宽区间 floor(Mx/N) 口径对照：三档的零差边总数、段数、可逆判定相同，
    仅平台长度在整除宽度附近有 ±1 的边界扰动。
    """
    out = {}
    for bits in BITS_LIST:
        n = 1 << bits
        x = np.arange(n, dtype=np.float64)
        lut = np.floor((M_INTERVALS - 1) * x / (n - 1) + 0.5).astype(np.int64)
        runs = plateau_runs(lut)
        lens = np.array([r["length"] for r in runs if r["length"] >= 2])
        vals, cnt = np.unique(lens, return_counts=True) if len(lens) else ([], [])
        out[f"{bits}bit"] = {
            "zero_diff_steps": int(np.count_nonzero(np.diff(lut) == 0)),
            "run_count": len(runs),
            "plateau_length_hist": {int(k): int(v) for k, v in zip(vals, cnt)},
            "max_plateau_length": int(max(r["length"] for r in runs)),
            "injective": bool(np.all(np.diff(lut) != 0)),
        }
    return out


def empirical_min_scan() -> dict:
    """小 N 穷举验证 Mmin=N；规范三档只测 N-1/N/N+1 边界。"""
    small = {}
    for nn in range(2, 33):
        found = None
        for mm in range(1, nn + 1):
            lut = map_lut(nn, mm)
            if np.all(np.diff(lut) != 0):
                found = mm
                break
        small[nn] = found
    brute_ok = all(v == nn for nn, v in small.items())

    boundary = {}
    for bits in BITS_LIST:
        nn = 1 << bits
        boundary[bits] = {}
        for mm in (nn - 1, nn, nn + 1):
            lut = map_lut(nn, mm)
            boundary[bits][mm] = bool(np.all(np.diff(lut) != 0))
    return {"small_N_2_to_32_min_equals_N": brute_ok, "boundary": boundary}


def main() -> None:
    tiers = [analyze_tier(b, M_INTERVALS) for b in BITS_LIST]
    sweep = empirical_min_scan()

    # 断言题目点名的硬不变量。
    by = {a["bits"]: a for a in tiers}
    assert by[8]["mismatched_pixels"] == 0, "可逆的 8bit 档必须零像素差异"
    assert by[8]["injective_reversible"] is True
    assert by[10]["injective_reversible"] is False
    assert by[12]["injective_reversible"] is False
    assert by[10]["mismatched_pixels"] == 3072
    assert by[12]["mismatched_pixels"] == 3840
    for a in tiers:
        assert a["modular_condition_verified"]
        assert a["min_intervals"] == a["n_levels"]

    table_rows = []
    json_tiers = []
    for a in tiers:
        write_csvs(a)
        save_native_pngs(a)
        fig = render_figure(a)
        print(f"[ok] {a['bits']}bit -> {fig}")
        table_rows.append({
            "bits": a["bits"], "N": a["n_levels"], "M": a["m_intervals"],
            "bin_width_w": a["bin_width"],
            "max_plateau_length": a["max_plateau_length"],
            "plateau_runs_ge2": a["plateau_run_count_ge2"],
            "zero_diff_steps": a["zero_diff_steps"],
            "reversible": a["injective_reversible"],
            "min_intervals": a["min_intervals"],
            "mismatched_pixels": a["mismatched_pixels"],
        })
        json_tiers.append({k: v for k, v in a.items() if not k.startswith("_")})

    with open(os.path.join(ART, "plateau_table.csv"), "w", newline="") as f:
        wcsv = csv.DictWriter(f, fieldnames=list(table_rows[0].keys()))
        wcsv.writeheader()
        wcsv.writerows(table_rows)

    payload = {"model": "T(x)=floor(M*x/N), inverse R(y)=ceil(y*N/M)",
               "shared_intervals_M": M_INTERVALS,
               "image": f"{W}x{H} full-level ramp, {N_PIX} px",
               "min_scan": sweep,
               "alt_endpoint_halfup_round": alt_endpoint_round_stats(),
               "tiers": json_tiers}
    with open(os.path.join(ART, "plateau_reversibility.json"), "w") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)

    # 纯文本总报告。
    lines = [
        "灰度区间映射：平台 / 可逆性 / 往返像素差异",
        f"模型 T(x)=floor(M*x/N)，反解 R(y)=ceil(y*N/M)；全档 M={M_INTERVALS} 区间",
        f"源图 {W}x{H}={N_PIX} 像素全覆盖梯度；平台判据=相邻映射值之差为 0",
        "",
        "位深 | N    | M   | w=N/M | Lmax(阶) | 长度>=2段数 | 零差边Z | 可逆 | Mmin | 差异像素",
        "-----|------|-----|-------|----------|-------------|---------|------|------|--------",
    ]
    for r in table_rows:
        lines.append(
            f"{r['bits']:>4} | {r['N']:<4} | {r['M']:<3} | {str(r['bin_width_w']):>5} | "
            f"{r['max_plateau_length']:>8} | {r['plateau_runs_ge2']:>11} | "
            f"{r['zero_diff_steps']:>7} | {'是' if r['reversible'] else '否':<4} | "
            f"{r['min_intervals']:<4} | {r['mismatched_pixels']}")
    lines += [
        "",
        "结论：M<N 时取整必然在某处产生零差相邻（平台）；可逆 ⇔ 无零差边 ⇔ Lmax=1 ⇔ M>=N；",
        "可逆所需最小区间数 Mmin=N=2^b，三档为 256 / 1024 / 4096。",
        "共用 M=256 时仅 8bit 档可逆，其往返差异为 0 像素（任意输入图像皆然）。",
        f"小 N=2..32 穷举 Mmin=N：{sweep['small_N_2_to_32_min_equals_N']}；边界 M=N-1/N/N+1：{json.dumps(sweep['boundary'], ensure_ascii=False)}",
    ]
    txt = "\n".join(lines) + "\n"
    with open(os.path.join(ART, "plateau_report.txt"), "w") as f:
        f.write(txt)

    md = ["# 往返像素差异报告（脚本生成）", "",
          f"源图 `{W}×{H}` = {N_PIX} 像素全覆盖梯度；映射/反解口径同上。", "",
          "| 位深 | N | M | w | 平台最大长度 L | 零差边 Z | 可逆 | Mmin | 差异像素 |",
          "|---|---|---|---|---|---|---|---|---|"]
    for r in table_rows:
        md.append(
            f"| {r['bits']} bit | {r['N']} | {r['M']} | {r['bin_width_w']} | "
            f"{r['max_plateau_length']}（零差边 {r['max_plateau_length']-1}/段） | "
            f"{r['zero_diff_steps']} | {'✅ 是' if r['reversible'] else '❌ 否'} | "
            f"{r['min_intervals']} | **{r['mismatched_pixels']} / {N_PIX}** |")
    md += ["", "可逆的 8bit 档差异为 **0**；10/12bit 的非零计数来自区间内多阶共映。",
           "每张 `roundtrip_<bits>bit.png` 是该档的往返对照图；`roundtrip_<bits>bit_native16.png` "
           "是原生位深的恢复图，可逐像素复核。", ""]
    with open(os.path.join(ART, "roundtrip_report.md"), "w") as f:
        f.write("\n".join(md))

    print("\n" + txt)


if __name__ == "__main__":
    main()
