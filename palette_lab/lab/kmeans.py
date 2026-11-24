"""确定性 k-means++ / Lloyd 子量化器 (纯 numpy)。

每个格内用 n_i 个桶对该格像素做量化, 返回调色板与该格 SSE。
k-means++ 初始中心用局部随机种子(种子由格编号 + 全局种子决定),
同一份数据多次运行结果完全一致, 保证实验可复现。
"""

from __future__ import annotations

import numpy as np


def _kmeanspp_init(x: np.ndarray, k: int, rng: np.random.Generator) -> np.ndarray:
    n = x.shape[0]
    centers = np.empty((k, x.shape[1]), dtype=np.float64)
    centers[0] = x[rng.integers(n)]
    closest = np.sum((x - centers[0]) ** 2, axis=1)
    for t in range(1, k):
        prob = closest
        s = prob.sum()
        if s <= 0:
            # 所有点重合, 剩余中心随机去重选取
            uniq = np.unique(x, axis=0)
            pick = rng.choice(uniq.shape[0], size=k - t, replace=uniq.shape[0] >= k - t)
            centers[t:] = uniq[pick][: k - t]
            break
        idx = rng.choice(n, p=prob / s)
        centers[t] = x[idx]
        d2 = np.sum((x - centers[t]) ** 2, axis=1)
        closest = np.minimum(closest, d2)
    return centers


def kmeans(
    x: np.ndarray,
    k: int,
    seed: int,
    max_iter: int = 100,
    tol: float = 1e-4,
    cache: dict | None = None,
    cache_key: tuple | None = None,
) -> tuple[np.ndarray, float]:
    """返回 (中心 (k,3), SSE)。k 大于去重色数时按实际色数返回。

    可传入 cache 字典与 cache_key 缓存同一 (数据, k) 的拟合结果。
    """
    if cache is not None and cache_key is not None and cache_key in cache:
        centers, sse = cache[cache_key]
        return centers.copy(), sse
    uniq = np.unique(x, axis=0)
    if uniq.shape[0] <= k:
        centers = uniq
        d2 = _assign_sse(x, centers)[1]
        result = (centers, float(d2.sum()))
        if cache is not None and cache_key is not None:
            cache[cache_key] = result
        return centers, float(d2.sum())

    rng = np.random.default_rng(seed)
    centers = _kmeanspp_init(x, k, rng)
    prev_sse = np.inf
    for _ in range(max_iter):
        labels, sse_terms = _assign_sse(x, centers)
        sse = float(sse_terms.sum())
        for j in range(k):
            mask = labels == j
            if mask.any():
                centers[j] = x[mask].mean(axis=0)
        if prev_sse - sse <= tol * max(1.0, prev_sse):
            break
        prev_sse = sse
    labels, sse_terms = _assign_sse(x, centers)
    # 去掉空簇并补跑一次收敛检查
    alive = np.unique(labels)
    if alive.size < centers.shape[0]:
        centers = centers[alive]
        labels, sse_terms = _assign_sse(x, centers)
    result = (centers, float(sse_terms.sum()))
    if cache is not None and cache_key is not None:
        cache[cache_key] = result
    return centers, float(sse_terms.sum())


def _assign_sse(x: np.ndarray, centers: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    # (n,k) 距离平方, 逐块计算控制内存
    n = x.shape[0]
    labels = np.empty(n, dtype=np.int64)
    best = np.empty(n, dtype=np.float64)
    block = 4096
    for s in range(0, n, block):
        e = min(n, s + block)
        d2 = np.sum((x[s:e, None, :] - centers[None, :, :]) ** 2, axis=2)
        labels[s:e] = np.argmin(d2, axis=1)
        best[s:e] = d2[np.arange(e - s), labels[s:e]]
    return labels, best


def quantize_with_palette(x: np.ndarray, centers: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    """给定调色板, 返回 (量化后的像素, 每点平方误差)。"""
    labels, sse_terms = _assign_sse(x, centers)
    return centers[labels], sse_terms
