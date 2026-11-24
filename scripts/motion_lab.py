#!/usr/bin/env python3
"""Curvature-aware sub-pixel motion estimation lab.

The script builds three controlled constant-velocity sequences, compares an
integer-pixel block matcher with a hierarchical residual-surface search, and
writes tables, diagnostic figures, and comparison images under
``artifacts/motion``.

The important formulas are deliberately implemented in this one file so the
experiment can be audited without a notebook.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Callable

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
from matplotlib import font_manager
from matplotlib.colors import LinearSegmentedColormap

ROOT = Path(__file__).resolve().parents[1]
OUT_DIR = ROOT / "artifacts" / "motion"

# Validated dataviz palette slots (light mode).
C_BLUE = "#2a78d6"
C_ORANGE = "#eb6834"
C_AQUA = "#1baf7a"
C_INK = "#0b0b0b"
C_SECONDARY = "#52514e"
C_GRID = "#e1e0d9"
C_SURFACE = "#fcfcfb"
VELOCITY_COLORS = [C_BLUE, C_ORANGE, C_AQUA]

# A blue sequential ramp and a blue-white-red diverging residual ramp.
SEQ_CMAP = LinearSegmentedColormap.from_list(
    "motion_blue", ["#f7fbff", "#cde2fb", "#6da7ec", "#2a78d6", "#0d366b"]
)
DIV_CMAP = LinearSegmentedColormap.from_list(
    "residual_div", ["#2a78d6", "#f0efec", "#d03b3b"]
)


@dataclass(frozen=True)
class VelocityCase:
    key: str
    label: str
    velocity: tuple[float, float]
    color: str


CASES = [
    VelocityCase("slow", "slow", (1.20, 0.60), C_BLUE),
    VelocityCase("medium", "medium", (2.40, -1.10), C_ORANGE),
    VelocityCase("fast", "fast", (3.70, 1.80), C_AQUA),
]


@dataclass
class CaseResult:
    key: str
    label: str
    vx_true: float
    vy_true: float
    speed_true: float
    ix: int
    iy: int
    dx: float
    dy: float
    speed_hat: float
    rms_integer: float
    rms_subpixel: float
    residual_reduction_pct: float
    kappa_x: float
    kappa_y: float
    final_step_px: float
    primary_step_px: float
    step_sequence: str
    guaranteed_step_px: float
    curvature_resolution_step_px: float
    output_grid_px: float
    integer_round_error_px: float
    integer_round_bound_px: float
    subgrid_round_error_px: float
    subgrid_round_bound_px: float
    rounding_mse_increase: float
    rounding_mse_bound: float
    truth_error_px: float
    layers: list[dict] = field(default_factory=list)
    trace: list[dict] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Synthetic sequences
# ---------------------------------------------------------------------------


def _fourier_texture(
    rng: np.random.Generator,
    size: float,
    n_terms: int,
    k_min: float,
    k_max: float,
    total_amp: float,
) -> Callable[[np.ndarray, np.ndarray], np.ndarray]:
    """Return a smooth isotropic random Fourier texture."""
    terms = []
    for _ in range(n_terms):
        k = rng.uniform(k_min, k_max)
        theta = rng.uniform(0.0, 2.0 * math.pi)
        fx = k * math.cos(theta) / size
        fy = k * math.sin(theta) / size
        amp = total_amp / math.sqrt(n_terms) * rng.uniform(0.65, 1.25)
        phase = rng.uniform(0.0, 2.0 * math.pi)
        terms.append((fx, fy, amp, phase))

    def sample(x: np.ndarray, y: np.ndarray) -> np.ndarray:
        z = np.zeros(np.broadcast_shapes(x.shape, y.shape), dtype=float)
        for fx, fy, amp, phase in terms:
            z += amp * np.sin(2.0 * math.pi * (fx * x + fy * y) + phase)
        return z

    return sample


def _rounded_rect_alpha(
    x: np.ndarray,
    y: np.ndarray,
    px: float,
    py: float,
    size: float,
    radius: float,
) -> np.ndarray:
    """Coverage of an anti-aliased rounded square."""
    h = size / 2.0
    qx = np.abs(x - (px + h)) - (h - radius)
    qy = np.abs(y - (py + h)) - (h - radius)
    signed = (
        np.hypot(np.maximum(qx, 0.0), np.maximum(qy, 0.0))
        + np.minimum(np.maximum(qx, qy), 0.0)
        - radius
    )
    return np.clip(0.5 - signed, 0.0, 1.0)


def make_sequence(
    velocity: tuple[float, float],
    *,
    width: int = 192,
    height: int = 128,
    n_frames: int = 5,
    seed: int = 731,
    noise_sigma: float = 1.1,
) -> tuple[list[np.ndarray], list[np.ndarray]]:
    """Create frames and current-object interior masks for one velocity."""
    rng = np.random.default_rng(seed)
    yy, xx = np.mgrid[0:height, 0:width].astype(np.float64)

    bg_tex = _fourier_texture(rng, max(width, height), 18, 0.7, 3.2, 20.0)
    bg = 116.0 + 0.05 * (xx - width / 2.0) + bg_tex(xx, yy)

    card_size = 72.0
    card_tex = _fourier_texture(rng, card_size, 34, 1.1, 8.5, 58.0)
    # A few broad blobs make the card identifiable at coarse scales.
    blobs = [
        (rng.uniform(12, 60), rng.uniform(12, 60), rng.uniform(7, 15), rng.uniform(-34, 34))
        for _ in range(7)
    ]

    x0, y0 = 57.0, 27.0
    frames: list[np.ndarray] = []
    masks: list[np.ndarray] = []

    for t in range(n_frames):
        px = x0 + velocity[0] * t
        py = y0 + velocity[1] * t
        lx = xx - px
        ly = yy - py
        card = 151.0 + card_tex(lx, ly)
        for bx, by, br, amp in blobs:
            card += amp * np.exp(-((lx - bx) ** 2 + (ly - by) ** 2) / (2.0 * br**2))
        card = np.clip(card, 28.0, 232.0)

        alpha = _rounded_rect_alpha(xx, yy, px, py, card_size, 11.0)
        img = bg * (1.0 - alpha) + card * alpha
        img += rng.normal(0.0, noise_sigma, img.shape)
        frames.append(np.clip(img, 0.0, 255.0))

        # Eroded interior: the objective is not dominated by raster edges.
        masks.append(
            (lx >= 6.0)
            & (lx <= card_size - 6.0)
            & (ly >= 6.0)
            & (ly <= card_size - 6.0)
        )

    return frames, masks


# ---------------------------------------------------------------------------
# Residual surface and interpolation
# ---------------------------------------------------------------------------


def shift_image(img: np.ndarray, dx: float, dy: float) -> tuple[np.ndarray, np.ndarray]:
    """Bilinear translation; output at pixel p samples input at p-(dx,dy)."""
    h, w = img.shape
    yy, xx = np.mgrid[0:h, 0:w].astype(np.float64)
    sx = xx - dx
    sy = yy - dy

    x0 = np.floor(sx).astype(np.int64)
    y0 = np.floor(sy).astype(np.int64)
    wx = sx - x0
    wy = sy - y0

    valid = (sx >= 0.0) & (sx <= w - 1.0) & (sy >= 0.0) & (sy <= h - 1.0)
    x0c = np.clip(x0, 0, w - 1)
    y0c = np.clip(y0, 0, h - 1)
    x1c = np.clip(x0 + 1, 0, w - 1)
    y1c = np.clip(y0 + 1, 0, h - 1)

    top = img[y0c, x0c] * (1.0 - wx) + img[y0c, x1c] * wx
    bottom = img[y1c, x0c] * (1.0 - wx) + img[y1c, x1c] * wx
    out = top * (1.0 - wy) + bottom * wy
    return out, valid


class ResidualSurface:
    """Mean squared motion-compensated residual over all frame transitions."""

    def __init__(self, frames: list[np.ndarray], masks: list[np.ndarray]):
        self.frames = frames
        self.masks = masks
        self.cache: dict[tuple[float, float], float] = {}
        self.evaluations = 0

    def mse(self, dx: float | int, dy: float | int) -> float:
        key = (round(float(dx), 10), round(float(dy), 10))
        if key in self.cache:
            return self.cache[key]
        self.evaluations += 1
        sse = 0.0
        count = 0
        dxf, dyf = float(dx), float(dy)
        for t in range(len(self.frames) - 1):
            pred, valid = shift_image(self.frames[t], dxf, dyf)
            mask = self.masks[t + 1] & valid
            diff = self.frames[t + 1] - pred
            sse += float(np.sum(diff[mask] ** 2))
            count += int(mask.sum())
        value = sse / max(count, 1)
        self.cache[key] = value
        return value

    def central_pair_residual(
        self, dx: float, dy: float
    ) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
        mid = len(self.frames) // 2 - 1
        pred, valid = shift_image(self.frames[mid], dx, dy)
        residual = self.frames[mid + 1] - pred
        mask = self.masks[mid + 1] & valid
        return residual, pred, mask


def integer_search(surface: ResidualSurface, radius: int = 7) -> tuple[int, int, float, np.ndarray]:
    grid = np.full((2 * radius + 1, 2 * radius + 1), np.inf)
    best = (0, 0)
    best_j = math.inf
    for iy, dy in enumerate(range(-radius, radius + 1)):
        for ix, dx in enumerate(range(-radius, radius + 1)):
            j = surface.mse(dx, dy)
            grid[iy, ix] = j
            if j < best_j:
                best_j = j
                best = (dx, dy)
    return best[0], best[1], best_j, grid


def curvature_at(
    surface: ResidualSurface,
    center: np.ndarray,
    h: float,
) -> tuple[float, np.ndarray, np.ndarray, np.ndarray]:
    """Return J0, gradient, regularized Hessian and raw Hessian."""
    cx, cy = float(center[0]), float(center[1])
    j0 = surface.mse(cx, cy)
    jxp = surface.mse(cx + h, cy)
    jxm = surface.mse(cx - h, cy)
    jyp = surface.mse(cx, cy + h)
    jym = surface.mse(cx, cy - h)
    jpp = surface.mse(cx + h, cy + h)
    jpm = surface.mse(cx + h, cy - h)
    jmp = surface.mse(cx - h, cy + h)
    jmm = surface.mse(cx - h, cy - h)

    gx = (jxp - jxm) / (2.0 * h)
    gy = (jyp - jym) / (2.0 * h)
    kx = (jxp - 2.0 * j0 + jxm) / (h * h)
    ky = (jyp - 2.0 * j0 + jym) / (h * h)
    kxy = (jpp - jpm - jmp + jmm) / (4.0 * h * h)
    raw_h = np.array([[kx, kxy], [kxy, ky]], dtype=float)

    # A residual surface is only locally convex.  Clip negative/weak curvature
    # in eigenspace so the inverse problem remains well posed.
    eigvals, eigvecs = np.linalg.eigh(raw_h)
    positive_floor = max(0.5, 0.02 * max(float(np.max(eigvals)), 1.0))
    reg_eig = np.maximum(eigvals, positive_floor)
    h_reg = (eigvecs * reg_eig) @ eigvecs.T
    grad = np.array([gx, gy], dtype=float)
    return j0, grad, h_reg, raw_h


def parabolic_vertex_1d(
    j_minus: float, j_zero: float, j_plus: float, h: float
) -> tuple[float, float]:
    """Offset and fitted minimum of a central three-point parabola."""
    denom = j_plus - 2.0 * j_zero + j_minus
    if denom <= 1e-12:
        return 0.0, j_zero
    delta = h * (j_minus - j_plus) / (2.0 * denom)
    # Do not extrapolate beyond the probe interval.
    delta = float(np.clip(delta, -h, h))
    fitted_min = j_zero - (j_plus - j_minus) ** 2 / (8.0 * denom)
    return delta, float(min(j_zero, fitted_min))


def curvature_resolution_step(kappa: float, given_drop: float) -> float:
    """Displacement whose quadratic residual penalty equals ``given_drop``.

    For J(u)-J* = 0.5*kappa*u^2, this is sqrt(2*given_drop/kappa).  Any grid
    spacing at least this large changes the quadratic residual by at least the
    requested amount over one curvature unit.
    """
    return math.sqrt(2.0 * max(given_drop, 0.0) / max(kappa, 1e-12))


def guaranteed_descent_step(
    grad_norm: float, kappa_dir: float, given_drop: float
) -> tuple[float | None, float]:
    """Smallest steepest-descent distance with model drop >= given_drop.

    Returns ``(None, max_model_drop)`` when the quadratic model cannot deliver
    ``given_drop``; that condition is used as a convergence criterion.
    """
    kappa_dir = max(kappa_dir, 1e-12)
    max_drop = grad_norm * grad_norm / (2.0 * kappa_dir)
    if max_drop + 1e-15 < given_drop:
        return None, max_drop
    discriminant = max(0.0, grad_norm * grad_norm - 2.0 * kappa_dir * given_drop)
    small_root = (grad_norm - math.sqrt(discriminant)) / kappa_dir
    return max(0.0, small_root), max_drop


def subpixel_search(
    surface: ResidualSurface,
    integer_dx: int,
    integer_dy: int,
    integer_j: float,
    *,
    min_drop: float = 1e-3,
    output_grid: int = 32,
    max_layers: int = 6,
    integer_radius: int = 7,
) -> tuple[np.ndarray, float, float, list[dict], list[dict], np.ndarray]:
    """Hierarchical curvature-guided minimization.

    ``min_drop`` is the guaranteed minimum accepted MSE decrease in gray^2.
    Near the optimum the quadratic model predicts less than this decrease; that
    is the primary convergence criterion.
    """
    q = 1.0 / output_grid
    center = np.array([float(integer_dx), float(integer_dy)], dtype=float)
    layers: list[dict] = []
    trace: list[dict] = []

    # Initial curvature is obtained from exact integer-shift evaluations.
    j0, grad, hess, raw_hess = curvature_at(surface, center, 1.0)
    inv_h = np.linalg.inv(hess)
    newton = -inv_h @ grad
    model_drop = 0.5 * float(grad @ inv_h @ grad)
    k_eff = float(max(np.linalg.eigvalsh(hess).min(), 1e-6))
    if model_drop > 1e-12:
        k_range = float((grad @ inv_h @ grad) / max(newton @ newton, 1e-12))
        range_from_residual = math.sqrt(2.0 * model_drop / max(k_range, 1e-6))
    else:
        range_from_residual = 2.0 * q
    radius = float(np.clip(1.25 * range_from_residual, 4.0 * q, 1.75))

    layers.append(
        {
            "layer": 0,
            "origin": "integer_exhaustive",
            "cx": float(integer_dx),
            "cy": float(integer_dy),
            "range_px": float(integer_radius),
            "next_range_px": radius,
            "probe_h_px": 1.0,
            "residual_mse": integer_j,
            "fitted_floor_mse": None,
            "residual_excess": model_drop,
            "kappa_x": raw_hess[0, 0],
            "kappa_y": raw_hess[1, 1],
            "step_px": 1.0,
            "guaranteed_step_px": curvature_resolution_step(k_eff, min_drop),
        }
    )

    final_guaranteed_step = 0.0
    final_step = 0.0

    for layer in range(1, max_layers + 1):
        probe_h = max(0.5 ** min(layer, 5), 2.0 * q)
        layer_anchor = center.copy()
        layer_radius = radius
        moved_in_layer = 0.0

        # A bounded number of curvature/Newton steps inside this layer's range.
        for iteration in range(8):
            j_before, grad, hess, raw_hess = curvature_at(surface, center, probe_h)
            inv_h = np.linalg.inv(hess)
            newton = -inv_h @ grad
            model_drop = 0.5 * float(grad @ inv_h @ grad)
            p_norm = float(np.linalg.norm(newton))

            # Scalar steepest-descent specialization; logged as an independent
            # curvature-derived minimum step for audit against the Newton ray.
            grad_norm = float(np.linalg.norm(grad))
            steepest_req = 0.0
            if grad_norm > 1e-12:
                descent_unit = -grad / grad_norm
                k_dir = float(descent_unit @ hess @ descent_unit)
                req, _ = guaranteed_descent_step(grad_norm, k_dir, min_drop)
                steepest_req = req if req is not None else 0.0

            # Convergence: even the quadratic optimum cannot deliver min_drop.
            if model_drop < min_drop or p_norm < 0.5 * q:
                trace.append(
                    {
                        "layer": layer,
                        "iteration": iteration,
                        "cx": center[0],
                        "cy": center[1],
                        "j_before": j_before,
                        "kappa_x": raw_hess[0, 0],
                        "kappa_y": raw_hess[1, 1],
                        "step_px": 0.0,
                        "guaranteed_step_px": final_guaranteed_step,
                        "steepest_req_step_px": steepest_req,
                        "predicted_drop": model_drop,
                        "actual_drop": 0.0,
                        "accepted": False,
                        "reason": "model_drop_below_given"
                        if model_drop < min_drop
                        else "step_below_quantization",
                    }
                )
                break

            # Along the Newton ray, predicted drop is D*alpha*(2-alpha), where
            # D is the full Newton model drop.  alpha_req is the smaller root
            # for which that drop equals the caller's given minimum.
            ratio = min(1.0, min_drop / max(model_drop, 1e-18))
            alpha_req = 1.0 - math.sqrt(max(0.0, 1.0 - ratio))
            guaranteed_step = alpha_req * p_norm
            final_guaranteed_step = guaranteed_step

            alpha = 1.0
            if p_norm > layer_radius:
                alpha = layer_radius / p_norm

            accepted = False
            last_actual_drop = 0.0
            for backtrack in range(9):
                candidate = center + alpha * newton
                # Stay inside the range inferred for this layer.
                offset = candidate - layer_anchor
                if np.linalg.norm(offset) > layer_radius:
                    offset *= layer_radius / np.linalg.norm(offset)
                    candidate = layer_anchor + offset
                j_after = surface.mse(float(candidate[0]), float(candidate[1]))
                actual_drop = j_before - j_after
                last_actual_drop = actual_drop
                predicted = model_drop * alpha * (2.0 - alpha)
                armijo_ok = actual_drop >= 0.08 * max(predicted, 0.0)
                if actual_drop >= min_drop and armijo_ok and actual_drop > 0.0:
                    step = float(np.linalg.norm(candidate - center))
                    center = candidate
                    moved_in_layer += step
                    final_step = step
                    trace.append(
                        {
                            "layer": layer,
                            "iteration": iteration,
                            "cx": center[0],
                            "cy": center[1],
                            "j_before": j_before,
                            "j_after": j_after,
                            "kappa_x": raw_hess[0, 0],
                            "kappa_y": raw_hess[1, 1],
                            "step_px": step,
                            "guaranteed_step_px": guaranteed_step,
                            "steepest_req_step_px": steepest_req,
                            "predicted_drop": predicted,
                            "actual_drop": actual_drop,
                            "accepted": True,
                            "reason": f"accepted_bt{backtrack}",
                        }
                    )
                    accepted = True
                    break
                alpha *= 0.5

            if not accepted:
                trace.append(
                    {
                        "layer": layer,
                        "iteration": iteration,
                        "cx": center[0],
                        "cy": center[1],
                        "j_before": j_before,
                        "kappa_x": raw_hess[0, 0],
                        "kappa_y": raw_hess[1, 1],
                        "step_px": 0.0,
                        "guaranteed_step_px": guaranteed_step,
                        "steepest_req_step_px": steepest_req,
                        "predicted_drop": model_drop,
                        "actual_drop": last_actual_drop,
                        "accepted": False,
                        "reason": "no_actual_drop_meets_given",
                    }
                )
                break

        # Three-point parabola gives an independent curvature inversion and a
        # fitted residual floor for the next layer's range.
        h_fit = max(probe_h, q)
        j0 = surface.mse(center[0], center[1])
        jxp = surface.mse(center[0] + h_fit, center[1])
        jxm = surface.mse(center[0] - h_fit, center[1])
        jyp = surface.mse(center[0], center[1] + h_fit)
        jym = surface.mse(center[0], center[1] - h_fit)
        dx_fit, jx_min = parabolic_vertex_1d(jxm, j0, jxp, h_fit)
        dy_fit, jy_min = parabolic_vertex_1d(jym, j0, jyp, h_fit)
        fit_candidate = center + np.array([dx_fit, dy_fit])
        j_fit_candidate = surface.mse(float(fit_candidate[0]), float(fit_candidate[1]))
        # A noisy parabola can point slightly uphill.  Never accept such a
        # correction; logged Newton acceptances remain the guaranteed drops.
        if j_fit_candidate <= j0 + 1e-12:
            center = fit_candidate
            j_fit = min(jx_min, jy_min, j_fit_candidate)
        else:
            j_fit = min(jx_min, jy_min, j0)

        _, grad_end, hess_end, raw_end = curvature_at(surface, center, probe_h)
        inv_end = np.linalg.inv(hess_end)
        end_drop = max(0.0, 0.5 * float(grad_end @ inv_end @ grad_end))
        excess = max(surface.mse(center[0], center[1]) - j_fit, end_drop, 0.0)
        k_eff = float(max(np.linalg.eigvalsh(hess_end).min(), 1e-6))
        residual_range = math.sqrt(2.0 * excess / k_eff)

        # Quantization floor keeps one final verification layer; geometric cap
        # makes the hierarchy coarse-to-fine even under a flat/noisy estimate.
        next_upper = max(q, min(0.75, 0.75 * radius))
        next_radius = float(np.clip(1.15 * residual_range, q, next_upper))
        layers.append(
            {
                "layer": layer,
                "origin": f"residual_inversion:{excess:.6g}",
                "cx": center[0],
                "cy": center[1],
                "range_px": radius,
                "next_range_px": next_radius,
                "probe_h_px": probe_h,
                "residual_mse": surface.mse(center[0], center[1]),
                "fitted_floor_mse": j_fit,
                "residual_excess": excess,
                "kappa_x": raw_end[0, 0],
                "kappa_y": raw_end[1, 1],
                "step_px": moved_in_layer,
                "guaranteed_step_px": final_guaranteed_step,
            }
        )
        radius = next_radius

        if radius <= 2.0 * q * 1.01 and layer >= 3:
            break

    raw_center = center.copy()
    snapped = np.round(center / q) * q
    final_j = surface.mse(float(snapped[0]), float(snapped[1]))
    return snapped, final_j, final_step, layers, trace, raw_center


# ---------------------------------------------------------------------------
# Experiment, bounds and artifacts
# ---------------------------------------------------------------------------


def rounding_bounds(estimate: np.ndarray, q: float) -> dict[str, float]:
    integer_rounded = np.round(estimate)
    int_err_vec = np.abs(estimate - integer_rounded)
    grid_rounded = np.round(estimate / q) * q
    grid_err_vec = np.abs(estimate - grid_rounded)
    return {
        "integer_round_error_x": float(int_err_vec[0]),
        "integer_round_error_y": float(int_err_vec[1]),
        "integer_round_error_l2": float(np.linalg.norm(int_err_vec)),
        "integer_bound_axis": 0.5,
        "integer_bound_l2": math.sqrt(0.5),
        "subgrid_round_error_x": float(grid_err_vec[0]),
        "subgrid_round_error_y": float(grid_err_vec[1]),
        "subgrid_round_error_l2": float(np.linalg.norm(grid_err_vec)),
        "subgrid_bound_axis": q / 2.0,
        "subgrid_bound_l2": q / math.sqrt(2.0),
    }


def random_rounding_audit(seed: int = 991) -> dict[str, float | int]:
    rng = np.random.default_rng(seed)
    q = 1.0 / 32.0
    n = 200_000
    xy = rng.uniform(-12.0, 12.0, size=(n, 2))
    err_int = np.linalg.norm(xy - np.round(xy), axis=1)
    err_grid = np.linalg.norm(xy - np.round(xy / q) * q, axis=1)
    assert np.max(np.abs(xy - np.round(xy))) <= 0.5 + 1e-12
    assert np.max(err_int) <= math.sqrt(0.5) + 1e-12
    assert np.max(err_grid) <= q / math.sqrt(2.0) + 1e-12
    return {
        "samples": n,
        "max_integer_l2": float(err_int.max()),
        "max_subgrid_l2": float(err_grid.max()),
        "integer_bound_l2": math.sqrt(0.5),
        "subgrid_bound_l2": q / math.sqrt(2.0),
    }


def run_case(case: VelocityCase) -> tuple[CaseResult, dict, np.ndarray, ResidualSurface]:
    seeds = {"slow": 1701, "medium": 1702, "fast": 1703}
    frames, masks = make_sequence(case.velocity, seed=seeds[case.key])
    surface = ResidualSurface(frames, masks)
    idx, idy, j_int, grid = integer_search(surface)
    q_grid = 32
    q = 1.0 / q_grid
    est, j_sub, final_step, layers, trace, raw_est = subpixel_search(
        surface, idx, idy, j_int, output_grid=q_grid
    )
    rms_int = math.sqrt(j_int)
    rms_sub = math.sqrt(j_sub)

    _, _, hess, raw = curvature_at(surface, est, max(2 * q, 0.0625))
    kappa_eff = float(max(np.linalg.eigvalsh(hess).min(), 1e-6))
    kappa_max = float(max(np.linalg.eigvalsh(hess).max(), 1e-6))
    raw_mse = surface.mse(float(raw_est[0]), float(raw_est[1]))
    rounding_mse_increase = j_sub - raw_mse
    # Quadratic diagnostic: 0.5*lambda_max*||e||^2, with ||e|| <= q/sqrt(2).
    rounding_mse_bound = 0.5 * kappa_max * (q / math.sqrt(2.0)) ** 2
    accepted_steps = [float(t["step_px"]) for t in trace if t.get("accepted")]
    primary_step = accepted_steps[0] if accepted_steps else 0.0
    step_sequence = ";".join(f"{s:.5f}" for s in accepted_steps)
    bounds = rounding_bounds(raw_est, q)
    truth = np.array(case.velocity, dtype=float)
    result = CaseResult(
        key=case.key,
        label=case.label,
        vx_true=case.velocity[0],
        vy_true=case.velocity[1],
        speed_true=float(np.linalg.norm(truth)),
        ix=idx,
        iy=idy,
        dx=float(est[0]),
        dy=float(est[1]),
        speed_hat=float(np.linalg.norm(est)),
        rms_integer=rms_int,
        rms_subpixel=rms_sub,
        residual_reduction_pct=100.0 * (1.0 - rms_sub / rms_int),
        kappa_x=float(raw[0, 0]),
        kappa_y=float(raw[1, 1]),
        final_step_px=final_step,
        primary_step_px=primary_step,
        step_sequence=step_sequence,
        guaranteed_step_px=curvature_resolution_step(kappa_eff, 1e-3),
        curvature_resolution_step_px=curvature_resolution_step(kappa_eff, 1e-3),
        output_grid_px=q,
        integer_round_error_px=bounds["integer_round_error_l2"],
        integer_round_bound_px=bounds["integer_bound_l2"],
        subgrid_round_error_px=bounds["subgrid_round_error_l2"],
        subgrid_round_bound_px=bounds["subgrid_bound_l2"],
        rounding_mse_increase=rounding_mse_increase,
        rounding_mse_bound=rounding_mse_bound,
        truth_error_px=float(np.linalg.norm(est - truth)),
        layers=layers,
        trace=trace,
    )
    extra = {
        "bounds": bounds,
        "grid": grid,
        "frames": frames,
        "masks": masks,
    }
    return result, extra, grid, surface


# ---------------------------------------------------------------------------
# Plotting
# ---------------------------------------------------------------------------


def _save_fig(fig: plt.Figure, path: Path) -> None:
    fig.savefig(path, dpi=170, bbox_inches="tight", facecolor=C_SURFACE)
    plt.close(fig)


def plot_version_comparison(
    results: list[CaseResult],
    extras: list[dict],
    surfaces: list[ResidualSurface],
    *,
    subpixel: bool,
    path: Path,
) -> None:
    """Three-row comparison board for either integer or sub-pixel compensation."""
    fig, axes = plt.subplots(3, 4, figsize=(15.2, 10.2), constrained_layout=True)
    fig.patch.set_facecolor(C_SURFACE)
    version = "Sub-pixel curvature-guided" if subpixel else "Integer-pixel"

    for row, (res, extra, surface) in enumerate(zip(results, extras, surfaces)):
        frames = extra["frames"]
        mid = len(frames) // 2 - 1
        if subpixel:
            dx, dy = res.dx, res.dy
        else:
            dx, dy = float(res.ix), float(res.iy)
        residual, pred, mask = surface.central_pair_residual(dx, dy)
        pair_rms = float(np.sqrt(np.mean(residual[mask] ** 2)))
        show_res = np.ma.array(residual, mask=~mask)
        absmax = float(np.nanpercentile(np.abs(show_res.filled(np.nan)), 98))
        absmax = max(absmax, 4.0)

        panels = [
            (frames[mid], "Reference frame", "gray", None),
            (frames[mid + 1], "Current frame", "gray", None),
            (pred, f"Warped prediction  d=({dx:+.3f},{dy:+.3f}) px", "gray", None),
            (
                show_res,
                f"Motion residual (shown pair) RMS={pair_rms:.2f}",
                DIV_CMAP,
                (-absmax, absmax),
            ),
        ]
        for col, (img, title, cmap, lim) in enumerate(panels):
            ax = axes[row, col]
            if cmap == "gray":
                ax.imshow(img, cmap="gray", vmin=0, vmax=255)
            else:
                im = ax.imshow(img, cmap=cmap, vmin=lim[0], vmax=lim[1])
                fig.colorbar(im, ax=ax, fraction=0.046, pad=0.02)
            ax.set_title(title, fontsize=10, color=C_INK)
            ax.set_xticks([])
            ax.set_yticks([])
            for spine in ax.spines.values():
                spine.set_edgecolor(C_GRID)
        axes[row, 0].set_ylabel(
            f"{res.label}\ntrue ({res.vx_true:+.2f},{res.vy_true:+.2f})",
            color=res and CASES[row].color,
            fontsize=10,
            fontweight="bold",
        )

    fig.suptitle(
        f"{version} motion-compensated comparison — three velocities",
        fontsize=15,
        fontweight="bold",
        color=C_INK,
    )
    _save_fig(fig, path)


def plot_raw_multiframe(
    results: list[CaseResult],
    extras: list[dict],
    surfaces: list[ResidualSurface],
    path: Path,
) -> None:
    """Show why uncompensated multi-frame differences look smeared."""
    fig, axes = plt.subplots(3, 4, figsize=(15.2, 10.2), constrained_layout=True)
    fig.patch.set_facecolor(C_SURFACE)

    for row, (res, extra, surface) in enumerate(zip(results, extras, surfaces)):
        frames = extra["frames"]
        temporal_mean = np.mean(np.stack(frames), axis=0)
        raw_abs = np.mean(
            [np.abs(frames[t + 1] - frames[t]) for t in range(len(frames) - 1)],
            axis=0,
        )
        int_res, _, int_mask = surface.central_pair_residual(float(res.ix), float(res.iy))
        sub_res, _, sub_mask = surface.central_pair_residual(res.dx, res.dy)
        int_show = np.ma.array(int_res, mask=~int_mask)
        sub_show = np.ma.array(sub_res, mask=~sub_mask)
        int_pair_rms = float(np.sqrt(np.mean(int_res[int_mask] ** 2)))
        sub_pair_rms = float(np.sqrt(np.mean(sub_res[sub_mask] ** 2)))
        bound = max(
            float(np.nanpercentile(np.abs(int_show.filled(np.nan)), 98)),
            float(np.nanpercentile(np.abs(sub_show.filled(np.nan)), 98)),
            4.0,
        )

        panels = [
            (temporal_mean, "Temporal average: motion ghost", "gray", None),
            (raw_abs, "Raw multi-frame |difference|", SEQ_CMAP, (0, max(18.0, float(raw_abs.max())))),
            (int_show, f"Integer pair RMS={int_pair_rms:.2f}", DIV_CMAP, (-bound, bound)),
            (sub_show, f"Sub-pixel pair RMS={sub_pair_rms:.2f}", DIV_CMAP, (-bound, bound)),
        ]
        for col, (img, title, cmap, lim) in enumerate(panels):
            ax = axes[row, col]
            if cmap == "gray":
                im = ax.imshow(img, cmap="gray", vmin=0, vmax=255)
            elif lim[0] >= 0:
                im = ax.imshow(img, cmap=cmap, vmin=lim[0], vmax=lim[1])
            else:
                im = ax.imshow(img, cmap=cmap, vmin=lim[0], vmax=lim[1])
            fig.colorbar(im, ax=ax, fraction=0.046, pad=0.02)
            ax.set_title(title, fontsize=10)
            ax.set_xticks([])
            ax.set_yticks([])
        axes[row, 0].set_ylabel(res.label, color=CASES[row].color, fontsize=11, fontweight="bold")

    fig.suptitle(
        "From smeared multi-frame difference to curvature-aligned residual",
        fontsize=15, fontweight="bold",
    )
    _save_fig(fig, path)


def plot_three_row_diagnostics(results: list[CaseResult], path: Path) -> None:
    """Three velocity rows: displacement, residual bars, step descent trace."""
    fig, axes = plt.subplots(3, 3, figsize=(14.5, 10.0), constrained_layout=True)
    fig.patch.set_facecolor(C_SURFACE)

    for r, res in enumerate(results):
        color = CASES[r].color
        # Column 1: true vs estimated displacement vectors.
        ax = axes[r, 0]
        ax.axhline(0, color=C_GRID, lw=1)
        ax.axvline(0, color=C_GRID, lw=1)
        ax.quiver(
            [0], [0], [res.vx_true], [res.vy_true],
            angles="xy", scale_units="xy", scale=1,
            color=C_SECONDARY, width=0.012, label="true",
        )
        ax.quiver(
            [0], [0], [res.dx], [res.dy],
            angles="xy", scale_units="xy", scale=1,
            color=color, width=0.010, label="estimated",
        )
        ax.scatter([res.vx_true], [res.vy_true], s=42, color=C_SECONDARY, marker="x")
        ax.scatter([res.dx], [res.dy], s=42, color=color, edgecolor="white", linewidth=0.7)
        ax.set_xlim(-5.2, 5.2)
        ax.set_ylim(-3.2, 3.2)
        ax.set_aspect("equal")
        ax.grid(color=C_GRID, lw=0.7)
        ax.set_title(
            f"{res.label}: displacement  est=({res.dx:+.3f},{res.dy:+.3f})",
            fontsize=10,
        )
        ax.set_xlabel("dx (px)")
        ax.set_ylabel("dy (px)")
        if r == 0:
            ax.legend(frameon=False, fontsize=8, loc="lower right")

        # Column 2: residual comparison.
        ax = axes[r, 1]
        bars = ax.bar(
            ["integer", "sub-pixel"],
            [res.rms_integer, res.rms_subpixel],
            color=[C_SECONDARY, color],
            width=0.58,
        )
        for bar, value in zip(bars, [res.rms_integer, res.rms_subpixel]):
            ax.text(
                bar.get_x() + bar.get_width() / 2,
                value + 0.08,
                f"{value:.2f}",
                ha="center",
                va="bottom",
                fontsize=9,
                color=C_INK,
            )
        ax.set_ylim(0, max(res.rms_integer * 1.18, 1))
        ax.grid(axis="y", color=C_GRID, lw=0.7)
        ax.set_title(
            f"motion-region RMS residual  −{res.residual_reduction_pct:.1f}%",
            fontsize=10,
        )
        ax.set_ylabel("RMS gray level")

        # Column 3: accepted step lengths and actual drops.
        ax = axes[r, 2]
        accepted = [t for t in res.trace if t.get("accepted")]
        if accepted:
            xs = np.arange(1, len(accepted) + 1)
            steps = [t["step_px"] for t in accepted]
            drops = [t["actual_drop"] for t in accepted]
            ax.plot(xs, steps, "-o", color=color, lw=2, ms=5, label="step length")
            ax.set_ylabel("step length (px)", color=color)
            ax.tick_params(axis="y", labelcolor=color)
            ax2 = ax.twinx()
            ax2.plot(xs, drops, "--s", color=C_ORANGE, lw=1.6, ms=4, label="MSE drop")
            ax2.set_ylabel("actual MSE drop", color=C_ORANGE)
            ax2.tick_params(axis="y", labelcolor=C_ORANGE)
            ax2.grid(False)
            ax.set_xticks(range(1, len(accepted) + 1))
            ax.set_xlim(0.8, len(accepted) + 0.2)
        ax.grid(axis="y", color=C_GRID, lw=0.7)
        ax.set_title("curvature-derived accepted steps", fontsize=10)
        ax.set_xlabel("accepted iteration")

    fig.suptitle(
        "Three velocity rows: displacement, residual and step history",
        fontsize=15,
        fontweight="bold",
    )
    _save_fig(fig, path)


def plot_quantity_rows(results: list[CaseResult], path: Path) -> None:
    """Alternative layout: three metric rows and three velocity columns."""
    fig, axes = plt.subplots(3, 3, figsize=(14.2, 10.0), constrained_layout=True)
    fig.patch.set_facecolor(C_SURFACE)

    for cidx, res in enumerate(results):
        color = CASES[cidx].color
        # Row 0: displacement components.
        ax = axes[0, cidx]
        labels = ["true x", "est x", "true y", "est y"]
        vals = [res.vx_true, res.dx, res.vy_true, res.dy]
        colors = [C_SECONDARY, color, C_SECONDARY, color]
        bars = ax.bar(labels, vals, color=colors)
        ax.axhline(0, color=C_INK, lw=0.8)
        ax.grid(axis="y", color=C_GRID)
        ax.set_title(f"{res.label} velocity", fontsize=11, fontweight="bold", color=C_INK)
        for b, v in zip(bars, vals):
            ax.text(b.get_x() + b.get_width() / 2, v + (0.12 if v >= 0 else -0.28),
                    f"{v:.2f}", ha="center", fontsize=8)
        ax.set_ylim(-2.2, 4.6)

        # Row 1: residual.
        ax = axes[1, cidx]
        bars = ax.bar(["int", "sub"], [res.rms_integer, res.rms_subpixel],
                      color=[C_SECONDARY, color], width=0.55)
        for b, v in zip(bars, [res.rms_integer, res.rms_subpixel]):
            ax.text(b.get_x() + b.get_width() / 2, v + 0.05, f"{v:.2f}",
                    ha="center", fontsize=9)
        ax.grid(axis="y", color=C_GRID)
        ax.set_ylim(0, res.rms_integer * 1.2)
        ax.set_title(f"RMS residual, −{res.residual_reduction_pct:.1f}%", fontsize=10)

        # Row 2: layer range and step.
        ax = axes[2, cidx]
        layer_nums = [d["layer"] for d in res.layers]
        ranges = [d["range_px"] for d in res.layers]
        steps = [max(d.get("step_px", 0), 1e-4) for d in res.layers]
        x = np.arange(len(layer_nums))
        ax.bar(x - 0.18, ranges, width=0.36, color=C_BLUE, label="range")
        ax.bar(x + 0.18, steps, width=0.36, color=C_AQUA, label="moved/step")
        ax.set_yscale("log")
        ax.set_xticks(x, [str(v) for v in layer_nums])
        ax.grid(axis="y", color=C_GRID, which="both")
        ax.set_title("layer range from residual inversion", fontsize=10)
        ax.set_xlabel("layer")
        ax.set_ylabel("px (log)")
        if cidx == 0:
            ax.legend(frameon=False, fontsize=8)

    fig.suptitle(
        "Displacement, residual and step/range — metric rows by velocity column",
        fontsize=15, fontweight="bold",
    )
    _save_fig(fig, path)


def plot_residual_surfaces(
    results: list[CaseResult],
    surfaces: list[ResidualSurface],
    path: Path,
) -> None:
    fig, axes = plt.subplots(1, 3, figsize=(14.5, 4.4), constrained_layout=True)
    radius = 1.2
    n = 121
    xs = np.linspace(-radius, radius, n)
    ys = np.linspace(-radius, radius, n)
    for ax, res, surface, color in zip(axes, results, surfaces, VELOCITY_COLORS):
        z = np.array([[surface.mse(res.dx + x, res.dy + y) for x in xs] for y in ys])
        im = ax.imshow(
            z,
            origin="lower",
            extent=[-radius, radius, -radius, radius],
            cmap=SEQ_CMAP,
            aspect="equal",
        )
        cs = ax.contour(xs, ys, z, levels=7, colors=C_INK, linewidths=0.55, alpha=0.55)
        ax.clabel(cs, inline=True, fontsize=7, fmt="%.1f")
        ax.scatter([0], [0], marker="+", s=90, color=C_ORANGE, linewidths=1.8)
        ax.set_title(f"{res.label}: residual surface around optimum", fontsize=10)
        ax.set_xlabel("Δdx (px)")
        ax.set_ylabel("Δdy (px)")
        fig.colorbar(im, ax=ax, fraction=0.046, pad=0.03, label="MSE")
    fig.suptitle("Local residual surfaces and curvature basins", fontsize=14, fontweight="bold")
    _save_fig(fig, path)


# ---------------------------------------------------------------------------
# Reports
# ---------------------------------------------------------------------------


def write_tables(
    results: list[CaseResult],
    audit: dict,
    out_dir: Path,
) -> None:
    accepted_drops = [
        float(t["actual_drop"])
        for r in results
        for t in r.trace
        if t.get("accepted")
    ]
    min_actual_drop = min(accepted_drops)

    metrics_path = out_dir / "metrics.csv"
    metric_fields = [k for k in asdict(results[0]).keys() if k not in {"layers", "trace"}]
    with metrics_path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=metric_fields, extrasaction="ignore")
        writer.writeheader()
        for r in results:
            writer.writerow(asdict(r))

    layer_path = out_dir / "layer_trace.csv"
    with layer_path.open("w", newline="", encoding="utf-8") as f:
        fields = ["case", "layer", "origin", "cx", "cy", "range_px", "next_range_px",
                  "probe_h_px", "residual_mse", "fitted_floor_mse", "residual_excess",
                  "kappa_x", "kappa_y", "step_px", "guaranteed_step_px"]
        writer = csv.DictWriter(f, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        for r in results:
            for layer in r.layers:
                row = {"case": r.key, **layer}
                writer.writerow(row)

    step_path = out_dir / "step_trace.csv"
    with step_path.open("w", newline="", encoding="utf-8") as f:
        fields = ["case"] + list(results[0].trace[0].keys())
        writer = csv.DictWriter(f, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        for r in results:
            for t in r.trace:
                writer.writerow({"case": r.key, **t})

    bounds_path = out_dir / "rounding_bounds.json"
    bounds_path.write_text(json.dumps(audit, indent=2, ensure_ascii=False), encoding="utf-8")

    md: list[str] = []
    md.append("# 曲率引导的亚像素运动估计实验\n")
    md.append("所有数值由 `scripts/motion_lab.py` 确定性生成。残差为运动区域内多帧相邻对的 MSE/RMS。\n")
    md.append("## 三行总表：每组速度的位移、残差与步长\n")
    md.append(
        "| 速度组 | 真值位移 (px) | 整像素位移 | 亚像素位移 | 整像素 RMS | 亚像素 RMS | 残差降幅 | κx, κy | 主搜索步长 (px) | 已接受步长序列 (px) |\n"
    )
    md.append("|---|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
    for r in results:
        md.append(
            f"| {r.label} | ({r.vx_true:+.2f}, {r.vy_true:+.2f}), |v|={r.speed_true:.3f} "
            f"| ({r.ix:+d}, {r.iy:+d}) "
            f"| ({r.dx:+.4f}, {r.dy:+.4f}) "
            f"| {r.rms_integer:.4f} | {r.rms_subpixel:.4f} "
            f"| {r.residual_reduction_pct:.2f}% "
            f"| {r.kappa_x:.2f}, {r.kappa_y:.2f} "
            f"| {r.primary_step_px:.5f} | {r.step_sequence} |\n"
        )

    md.append("\n## 取整误差上界与实测\n")
    md.append(
        "任意二维位置取最近整数时，单坐标误差 ≤ 1/2 px，欧氏误差上界 "
        "`sqrt(0.5)=0.70710678 px`。若先量化到步长 q=1/32 px 的亚像素网格，"
        "则单坐标 ≤ q/2，欧氏上界 `q/sqrt(2)=0.02209709 px`。\n"
    )
    md.append(
        "| 速度组 | 到整数点实测 L2 | 整数上界 | 到 1/32 网格实测 L2 | 网格上界 | 对真值估计误差 |\n"
    )
    md.append("|---|---:|---:|---:|---:|---:|\n")
    for r in results:
        md.append(
            f"| {r.label} | {r.integer_round_error_px:.6f} "
            f"| {r.integer_round_bound_px:.6f} "
            f"| {r.subgrid_round_error_px:.6f} "
            f"| {r.subgrid_round_bound_px:.6f} "
            f"| {r.truth_error_px:.6f} |\n"
        )
    md.append(
        f"\n随机取整审计：{audit['samples']:,} 个点，整数取整最大 L2="
        f"{audit['max_integer_l2']:.8f} ≤ {audit['integer_bound_l2']:.8f}；"
        f"1/32 网格最大 L2={audit['max_subgrid_l2']:.8f} ≤ "
        f"{audit['subgrid_bound_l2']:.8f}。\n"
    )
    md.append(
        "位置取整还会使残差离开连续极小点。按局部二次曲面 "
        "`ΔJ≤0.5 κ_max ||e||²` 和 `||e||≤q/√2`，诊断上界为 "
        "`ΔJ≤κ_max q²/4`（gray²）。三组实测量化增量均在该界内：\n\n"
        "| 速度组 | 实测 MSE 增量 | 二次曲率诊断上界 |\n|---|---:|---:|\n"
    )
    for r in results:
        md.append(
            f"| {r.label} | {r.rounding_mse_increase:.6f} "
            f"| {r.rounding_mse_bound:.6f} |\n"
        )

    md.append("\n## 曲率、步长与分层范围公式\n")
    md.append(
        "在中心点 `c` 用探针步长 `h` 计算二阶差分：\n\n"
        "```text\n"
        "κx = [J(cx+h,cy) - 2J(c) + J(cx-h,cy)] / h²\n"
        "κy = [J(cx,cy+h) - 2J(c) + J(cx,cy-h)] / h²\n"
        "```\n\n"
        "并以交叉二阶差分组成 Hessian；负特征值被正则化为正曲率。"
        "Newton 方向为 `p=-H⁻¹g`，二次模型预测下降量 "
        "`D=0.5 gᵀH⁻¹g`。沿该方向以系数 α 前进时，预测下降为 "
        "`Dα(2-α)`。给定必须达到的下降量 `Δ=1e-3` gray²，较小根\n\n"
        "```text\n"
        "α_req = 1 - sqrt(max(0, 1 - Δ/D))\n"
        "s_req = α_req · ||p||\n"
        "```\n\n"
        "给出模型上恰好下降 Δ 的步长；`D<Δ` 时曲率模型判定没有任何步长能达到给定量，"
        "这正是收敛条件。实际候选还必须满足真实 MSE 下降 ≥ Δ 且通过宽松 Armijo 检查，"
        "否则减半回溯；无法满足则结束本层。\n\n"
        "每层结束后用三点抛物线/二次模型反解残差超出量 `E=J-J_floor`，"
        "由二次曲面 `E≈0.5κR²` 得到下一层范围：\n\n"
        "```text\n"
        "R_next = clip(1.15 sqrt(2E/κ_eff), q, max(q, 0.75 R))\n"
        "```\n\n"
        "其中 q=1/32 px，小于 q 后不再缩小范围，只做量化验证。收敛判据为：模型最大下降量 < Δ、Newton 长度 < q/2，"
        "或回溯后没有真实下降 ≥ Δ 的步长。本次运行中所有被接受步的最小真实 MSE "
        f"下降量为 `{min_actual_drop:.6f}` gray²，满足 `≥ 1e-3`。\n"
    )

    md.append("\n## 产物\n")
    for name in [
        "comparison_integer.png",
        "comparison_subpixel.png",
        "multiframe_difference.png",
        "three_rows_velocity.png",
        "three_rows_quantities.png",
        "residual_surfaces.png",
        "metrics.csv",
        "layer_trace.csv",
        "step_trace.csv",
    ]:
        md.append(f"- `{name}`\n")
    (out_dir / "motion_report.md").write_text("".join(md), encoding="utf-8")


def save_raw_frame_examples(extras: list[dict], out_dir: Path) -> None:
    raw_dir = out_dir / "frames"
    raw_dir.mkdir(parents=True, exist_ok=True)
    for case, extra in zip(CASES, extras):
        for i, frame in enumerate(extra["frames"]):
            # Matplotlib-independent PGM writer for auditable raw frames.
            u8 = np.clip(frame, 0, 255).astype(np.uint8)
            header = f"P5\n{u8.shape[1]} {u8.shape[0]}\n255\n".encode("ascii")
            (raw_dir / f"{case.key}_t{i}.pgm").write_bytes(header + u8.tobytes())


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, default=OUT_DIR)
    args = parser.parse_args()
    out_dir: Path = args.out
    out_dir.mkdir(parents=True, exist_ok=True)

    audit = random_rounding_audit()
    results: list[CaseResult] = []
    extras: list[dict] = []
    surfaces: list[ResidualSurface] = []

    for case in CASES:
        result, extra, _, surface = run_case(case)
        results.append(result)
        extras.append(extra)
        surfaces.append(surface)
        print(
            f"{case.label:6s} true=({result.vx_true:+.2f},{result.vy_true:+.2f}) "
            f"int=({result.ix:+d},{result.iy:+d}) RMS={result.rms_integer:.3f} "
            f"sub=({result.dx:+.4f},{result.dy:+.4f}) RMS={result.rms_subpixel:.3f} "
            f"drop={result.residual_reduction_pct:.2f}% truth_err={result.truth_error_px:.4f}"
        )
        assert result.rms_subpixel < result.rms_integer, case.key
        assert result.subgrid_round_error_px <= result.subgrid_round_bound_px + 1e-12
        assert result.integer_round_error_px <= result.integer_round_bound_px + 1e-12
        assert result.rounding_mse_increase <= result.rounding_mse_bound + 1e-9

    # Every logged accepted step satisfies the user's given descent floor.
    for result in results:
        accepted = [t for t in result.trace if t.get("accepted")]
        assert accepted, result.key
        assert all(t["actual_drop"] >= 1e-3 - 1e-10 for t in accepted)

    write_tables(results, audit, out_dir)
    save_raw_frame_examples(extras, out_dir)
    plot_version_comparison(results, extras, surfaces, subpixel=False,
                            path=out_dir / "comparison_integer.png")
    plot_version_comparison(results, extras, surfaces, subpixel=True,
                            path=out_dir / "comparison_subpixel.png")
    plot_raw_multiframe(results, extras, surfaces,
                        path=out_dir / "multiframe_difference.png")
    plot_three_row_diagnostics(results, out_dir / "three_rows_velocity.png")
    plot_quantity_rows(results, out_dir / "three_rows_quantities.png")
    plot_residual_surfaces(results, surfaces, out_dir / "residual_surfaces.png")

    summary = {
        "min_drop_mse_gray2": 1e-3,
        "output_grid": 1 / 32,
        "all_subpixel_residuals_below_integer": all(
            r.rms_subpixel < r.rms_integer for r in results
        ),
        "audit": audit,
        "cases": [asdict(r) | {"layers": r.layers, "trace": r.trace} for r in results],
    }
    (out_dir / "summary.json").write_text(
        json.dumps(summary, indent=2, ensure_ascii=False, default=float),
        encoding="utf-8",
    )
    print(f"wrote artifacts to {out_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
