"""带整数约束的桶分配求解器。

模型(可分凸资源分配)
    最小化  F(n_1..n_C) = Σ_i w_i / n_i
    约束    Σ_i n_i = K,  n_i ∈ [1, u_i] 整数, w_i > 0
其中 w_i = p_i v_i。

提供三种分配:
  greedy_allocate   边际收益贪心, 给出该整数问题的全局最优解(见 optimality 说明)
  hamilton_allocate 按权重比例 + 最大余数法(Hamilton/Vinton), 即"按出现次数分桶"
                    类做法的比例分配版, 用于复现 Alabama 悖论
  dp_allocate       O(CK^2) 动态规划, 小问题上的精确解, 仅用于交叉验证贪心
"""

from __future__ import annotations

import math

import numpy as np


def continuous_share(weights: np.ndarray, k: int) -> np.ndarray:
    """连续松弛解 n_i* = K·w_i / Σw。"""
    return k * weights / weights.sum()


def greedy_allocate(
    weights: np.ndarray,
    k: int,
    caps: np.ndarray | None = None,
) -> np.ndarray:
    """边际贪心求整数最优。

    每格先给 1 桶(下界), 之后每加一桶都投给边际收益
        Δ_i(m) = w_i·(1/m − 1/(m+1)) = w_i/(m(m+1))
    最大的格; Δ_i(m) 随 m 单调递减(离散凸), 所以贪心等价于全局最优。
    """
    c = weights.size
    if caps is None:
        caps = np.full(c, k, dtype=np.int64)
    if k < c:
        raise ValueError(f"K={k} 小于非空格数 C={c}, 无法保证每格至少 1 桶")
    if (caps < 1).any():
        raise ValueError("每格容量上界必须 >= 1")

    n = np.ones(c, dtype=np.int64)
    caps = caps.astype(np.int64)
    active = np.ones(c, dtype=bool)
    active[caps <= 1] = False
    remaining = k - c
    # 用堆维护边际收益; 惰性删除失效条目
    import heapq

    heap: list[tuple[float, int]] = []
    for i in range(c):
        if active[i]:
            m = int(n[i])
            heapq.heappush(heap, (-weights[i] / (m * (m + 1)), i))

    while remaining > 0:
        if not heap:
            break
        neg_gain, i = heapq.heappop(heap)
        m = int(n[i])
        # 惰性失效: 堆里记录的 m 必须等于当前 m 才有效
        if not active[i] or m >= caps[i] or not math.isclose(
            -neg_gain, weights[i] / (m * (m + 1)), rel_tol=1e-12, abs_tol=1e-18
        ):
            continue
        n[i] = m + 1
        remaining -= 1
        if n[i] < caps[i]:
            nm = int(n[i])
            heapq.heappush(heap, (-weights[i] / (nm * (nm + 1)), i))
    if remaining > 0:
        raise RuntimeError("容量上界总和不足, 无法分配 K 个桶")
    return n


def hamilton_allocate(
    weights: np.ndarray, k: int, caps: np.ndarray | None = None
) -> np.ndarray:
    """最大余数法(Hamilton/Vinton apportionment)。

    先按连续比例取整(每格保底 1), 余数名额依次发给小数部分最大的格。
    该方法不满足房屋单调性: K 增大时某格可能反而丢桶(Alabama 悖论)。
    """
    c = weights.size
    if k < c:
        raise ValueError(f"K={k} 小于非空格数 C={c}")
    if caps is None:
        caps = np.full(c, k, dtype=np.int64)
    share = continuous_share(weights, k)
    floor = np.maximum(1, np.floor(share).astype(np.int64))
    floor = np.minimum(floor, caps)
    n = floor.copy()
    remaining = k - int(n.sum())
    if remaining > 0:
        frac = share - np.floor(share)
        # 已触顶的格不再参与余数分配
        order = np.argsort(-frac, kind="stable")
        for i in order:
            if remaining == 0:
                break
            if n[i] < caps[i]:
                n[i] += 1
                remaining -= 1
    elif remaining < 0:
        # 触顶导致的超额, 从最小权重且 >1 的格回收
        order = np.argsort(weights, kind="stable")
        for i in order:
            while remaining < 0 and n[i] > 1:
                n[i] -= 1
                remaining += 1
    if n.sum() != k:
        raise RuntimeError("Hamilton 分配未能用尽名额")
    return n


def dp_allocate(weights: np.ndarray, k: int) -> np.ndarray:
    """O(CK^2) 精确 DP, 仅用于小规模交叉验证。

    状态 dp[t,j] = 前 t 格分 j 桶的最小 Σ w/n。
    """
    c = weights.size
    if k < c:
        raise ValueError(f"K={k} 小于 C={c}")
    inf = np.inf
    dp = np.full((c + 1, k + 1), inf)
    take = np.zeros((c + 1, k + 1), dtype=np.int64)
    dp[0, 0] = 0.0
    for t in range(1, c + 1):
        w = weights[t - 1]
        for j in range(t, k + 1):  # 前 t 格至少 t 桶
            best = inf
            best_m = 1
            # 第 t 格分 m 桶, 前面 t-1 格至少 t-1 桶
            for m in range(1, j - (t - 1) + 1):
                val = dp[t - 1, j - m] + w / m
                if val < best:
                    best = val
                    best_m = m
            dp[t, j] = best
            take[t, j] = best_m
    n = np.zeros(c, dtype=np.int64)
    j = k
    for t in range(c, 0, -1):
        m = int(take[t, j])
        n[t - 1] = m
        j -= m
    return n


def objective(weights: np.ndarray, n: np.ndarray) -> float:
    return float(np.sum(weights / n))
