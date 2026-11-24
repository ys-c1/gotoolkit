"""RGB 色域规则分格与统计。

把 [0,255]^3 用边长 b 的立方体切成规则网格, 对每个非空格 i 统计:
  n_i  像素数, 占比 p_i = n_i / N
  v_i  支撑体积 = 该格内被像素占据的子体素数 × 子体素单位体积
       (粒度细到每通道 64 级时, v_i 逼近颜色点云的实际占据体积,
        而不是规则格的名义体积——名义体积对所有格相同, 无法区分疏密)
  w_i  权重 = p_i * v_i   (题设加权依据)
"""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np

VOX_BITS = 6  # 子体素: 每通道 2^6=64 级
VOX_LEVELS = 1 << VOX_BITS
VOX_SCALE = 256.0 / VOX_LEVELS


@dataclass
class Cell:
    idx: int          # 规则格编号
    n: int            # 像素数
    v: float          # 支撑体积(以 8bit 立方体为单位, 边长 256)
    p: float          # 像素占比
    w: float          # 权重 p*v
    pixels: np.ndarray  # (n,3) float64 像素副本


class Gamut:
    def __init__(self, rgb: np.ndarray, grid_bits: int = 3):
        """rgb: (H,W,3) 或 (N,3) uint8。grid_bits: 每通道 2^grid_bits 个格。"""
        x = rgb.reshape(-1, 3).astype(np.float64)
        self.n_total = x.shape[0]
        self.grid_levels = 1 << grid_bits
        cell_side = 256.0 / self.grid_levels
        cell_idx = np.clip((x // cell_side).astype(np.int64), 0, self.grid_levels - 1)
        cell_id = (
            cell_idx[:, 0] * self.grid_levels * self.grid_levels
            + cell_idx[:, 1] * self.grid_levels
            + cell_idx[:, 2]
        )

        # 子体素键, 用于支撑体积
        vox = np.clip((x.astype(np.int64) >> (8 - VOX_BITS)), 0, VOX_LEVELS - 1)
        vox_key = vox[:, 0] * VOX_LEVELS * VOX_LEVELS + vox[:, 1] * VOX_LEVELS + vox[:, 2]

        order = np.argsort(cell_id, kind="stable")
        sorted_id = cell_id[order]
        uniq, starts, counts = np.unique(
            sorted_id, return_index=True, return_counts=True
        )

        unit_vol = VOX_SCALE ** 3
        self.cells: list[Cell] = []
        for k, cid in enumerate(uniq):
            s = starts[k]
            e = s + counts[k]
            rows = order[s:e]
            occupied_vox = np.unique(vox_key[rows]).size
            v = occupied_vox * unit_vol
            n = int(counts[k])
            p = n / self.n_total
            self.cells.append(
                Cell(
                    idx=int(cid),
                    n=n,
                    v=float(v),
                    p=p,
                    w=p * v,
                    pixels=x[rows],
                )
            )
        self.cell_side = cell_side
        self.cell_vol = cell_side ** 3

    @property
    def c(self) -> int:
        return len(self.cells)

    def weights(self) -> np.ndarray:
        return np.array([c.w for c in self.cells], dtype=np.float64)

    def counts(self) -> np.ndarray:
        return np.array([c.n for c in self.cells], dtype=np.int64)

    def volumes(self) -> np.ndarray:
        return np.array([c.v for c in self.cells], dtype=np.float64)

    def summary(self) -> str:
        ws = self.weights()
        vs = self.volumes()
        ns = self.counts()
        top_n = np.argsort(ns)[::-1][:5]
        top_w = np.argsort(ws)[::-1][:5]
        lines = [
            f"总格数 {self.grid_levels**3}, 非空格 C={self.c}, 像素 N={self.n_total}",
            f"像素最多 5 格 idx={[int(self.cells[i].idx) for i in top_n]} "
            f"p={[f'{self.cells[i].p:.4f}' for i in top_n]} "
            f"v={[f'{self.cells[i].v:.0f}' for i in top_n]}",
            f"权重最高 5 格 idx={[int(self.cells[i].idx) for i in top_w]} "
            f"p={[f'{self.cells[i].p:.4f}' for i in top_w]} "
            f"v={[f'{self.cells[i].v:.0f}' for i in top_w]}",
            f"v 的变异系数={np.std(vs)/np.mean(vs):.3f}, "
            f"w 与 p 的秩相关={_spearman(ws, ns):.3f}",
        ]
        return "\n".join(lines)


def _spearman(a: np.ndarray, b: np.ndarray) -> float:
    ra = np.argsort(np.argsort(a)).astype(np.float64)
    rb = np.argsort(np.argsort(b)).astype(np.float64)
    ra -= ra.mean()
    rb -= rb.mean()
    return float(np.dot(ra, rb) / np.sqrt(np.dot(ra, ra) * np.dot(rb, rb)))
