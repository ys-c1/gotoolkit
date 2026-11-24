"""细粒度 K 扫描, 捕捉 Hamilton 最大余数法的 Alabama 悖论。

五档桶数是翻倍跳变, 小权重格又有保底 1 桶, 悖论在粗档位上看不出来;
这里在 K = C..K_MAX 上逐桶扫描, 记录"总桶数增加、某格反而丢桶"的事件。
贪心解满足房屋单调性(边际收益随 m 递减, 旧桶不会被收回), 同场对照。
"""

from __future__ import annotations

import json
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from lab import allocate, gamut as gamut_mod, image_gen

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
K_MAX = 160


def main() -> None:
    rgb = image_gen.make_image()
    g = gamut_mod.Gamut(rgb, grid_bits=2)
    w = g.weights()
    caps = np.minimum(g.counts(), K_MAX)

    events = []
    prev_h = None
    prev_g = None
    greedy_loss = 0
    for K in range(g.c, K_MAX + 1):
        h = allocate.hamilton_allocate(w, K, caps)
        gr = allocate.greedy_allocate(w, K, caps)
        if prev_h is not None:
            lost = np.where(h < prev_h)[0]
            for i in lost:
                events.append(
                    {
                        "K_from": K - 1,
                        "K_to": K,
                        "cell_idx": g.cells[int(i)].idx,
                        "p": g.cells[int(i)].p,
                        "v": g.cells[int(i)].v,
                        "w": g.cells[int(i)].w,
                        "buckets_before": int(prev_h[i]),
                        "buckets_after": int(h[i]),
                    }
                )
            greedy_loss += int(np.sum(gr < prev_g))
        prev_h = h
        prev_g = gr

    out = {
        "scan_range": [g.c, K_MAX],
        "n_events": len(events),
        "greedy_loss_total": int(greedy_loss),
        "events": events,
        "minimal_counterexample": _minimal_counterexample(),
    }
    path = os.path.join(ROOT, "data", "alabama.json")
    with open(path, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)

    print(f"扫描 K={g.c}..{K_MAX}: Hamilton 丢桶事件 {len(events)} 次, "
          f"贪心丢桶 {greedy_loss} 次")
    for e in events[:8]:
        print(
            f"  K {e['K_from']}->{e['K_to']}: 格 {e['cell_idx']} "
            f"({e['buckets_before']}->{e['buckets_after']} 桶), "
            f"p={e['p']:.5f} v={e['v']:.0f} w={e['w']:.4f}"
        )
    mc = out["minimal_counterexample"]
    print("\n最小反例(合成权重, 同一份求解器代码):")
    for row in mc["allocations"]:
        print(f"  K={row['K']:3d} Hamilton={row['hamilton']} 贪心={row['greedy']}")
    print(f"  丢桶发生在 K={mc['event']['K_from']}->{mc['event']['K_to']}, "
          f"格 {mc['event']['cell']} {mc['event']['before']}->{mc['event']['after']} 桶")
    print(f"[out] {path}")


def _minimal_counterexample() -> dict:
    """在合成权重上找触发悖论的最小例子, 证明这是 Hamilton 法的性质而非数据偶然。"""
    rng = np.random.default_rng(0)
    for _ in range(500):
        c = int(rng.integers(3, 7))
        w = np.round(np.exp(rng.normal(0, 1.5, c)), 4)
        caps = np.full(c, 200)
        prev = None
        for K in range(c, 120):
            h = allocate.hamilton_allocate(w, K, caps)
            gr = allocate.greedy_allocate(w, K, caps)
            if prev is not None and (h < prev[0]).any():
                lost = int(np.where(h < prev[0])[0][0])
                rows = []
                for KK in range(K - 2, K + 2):
                    rows.append(
                        {
                            "K": KK,
                            "hamilton": allocate.hamilton_allocate(w, KK, caps).tolist(),
                            "greedy": allocate.greedy_allocate(w, KK, caps).tolist(),
                        }
                    )
                return {
                    "weights": w.tolist(),
                    "event": {
                        "K_from": K - 1,
                        "K_to": K,
                        "cell": lost,
                        "before": int(prev[0][lost]),
                        "after": int(h[lost]),
                    },
                    "allocations": rows,
                }
            prev = (h, gr)
    raise RuntimeError("未找到反例")


if __name__ == "__main__":
    main()
