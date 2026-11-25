"""
Main experiment: 3 images x 3 kernel supports at r=2 (4:2:0-style).

Outputs per cell:
  fringe influence-band width in pixels (measured vs r+2s, tolerance 1 px)
  threshold-visible fringe columns
  luma loss: RMSE, high-frequency energy retention, worst-edge gradient loss
Also fits the empirical trade-off coefficient C in
  ΔRMSE_Y <= C · (W_ref - W)   per 1 px of fringe narrowing.
"""
import json
import sys
import numpy as np

sys.path.insert(0, "/home/ys/feedback/uidreczz28dpNBzBitz/analysis")
from chroma_2d import (IMAGES, rgb2ycbcr, ycbcr2rgb, chain_2d,
                       subsample_rgb)
from measure import influence_mask_1d, measure_visible
from chroma_model import step_signal, chain_1d

R = 2
SUPPORTS = (2.5, 3.0, 3.5)          # tent half-support s, all > r (measurable)
EPS = 0.02


def edge_columns_vertical(plane):
    """Strongest vertical step (col, center-row), kept away from borders."""
    H, W = plane.shape
    diff = np.abs(np.diff(plane, axis=1))
    m = int(0.12 * W)
    inner = diff[:, m:W - m]
    col = int(np.argmax(inner.mean(axis=0))) + m + 1
    rows = np.flatnonzero(diff[:, col - 1] > 0.5 * diff[:, col - 1].max())
    row = int(np.median(rows)) if len(rows) else H // 2
    return col, row


def fringe_2d(rec_chroma, true_chroma, r, s, edge_col, half=14):
    """Local horizontal influence/visible band across one edge.

    Influence mask is geometric (weight-based). The visible measure uses the
    chroma-error ESF inside a +/-half column window around the edge.
    """
    N = rec_chroma.shape[1]
    mask, pL, pR = influence_mask_1d(float(edge_col), r, s, N, kind="tent")
    span = (pR + s) - (pL - s)
    infl_cols = int(mask.sum())
    # local visible ESF: normalized chroma-error profile near the edge
    a = max(edge_col - half, 0)
    b = min(edge_col + half, N)
    prof = np.sqrt(((rec_chroma - true_chroma) ** 2).mean(axis=0))[a:b]
    prof = prof / max(prof.max(), 1e-9)
    idx = np.flatnonzero(prof > EPS)
    if len(idx):
        vis_cols = idx[-1] - idx[0] + 1
        lo, hi = idx[0], idx[-1]
        left = (a + lo - prof[lo] / (prof[lo] - prof[lo - 1])
                if lo > 0 and prof[lo] > prof[lo - 1] else float(a + lo))
        right = (a + hi + prof[hi] / (prof[hi] - prof[hi + 1])
                 if hi < len(prof) - 1 and prof[hi] > prof[hi + 1]
                 else a + hi + 1.0)
        vis_span = right - left
    else:
        vis_cols, vis_span = 0, 0.0
    return span, infl_cols, vis_span, vis_cols


def local_lipschitz(Y, edge_col, half=14):
    """RMS Lipschitz constant in the edge window: RMS |dY/dx|."""
    a = max(edge_col - half, 0)
    b = min(edge_col + half, Y.shape[1] - 1)
    grad = np.diff(Y[:, a:b], axis=1)
    return float(np.sqrt((grad ** 2).mean()))


def luma_metrics(Y, r, s):
    """Run the same chain on luma and quantify detail loss."""
    Yhat = chain_2d(Y, r, s, kind="tent")
    rmse = float(np.sqrt(np.mean((Yhat - Y) ** 2)))
    # high-frequency energy: |F| above 0.6 of Nyquist
    F = np.fft.fft2(Y - Y.mean())
    H, W = Y.shape
    fy = np.fft.fftfreq(H)[:, None]
    fx = np.fft.fftfreq(W)[None, :]
    fr = np.sqrt(fx ** 2 + fy ** 2)
    hf = fr > 0.30
    e_orig = float(np.sum(np.abs(np.fft.fft2(Y - Y.mean()))[hf] ** 2))
    e_rec = float(np.sum(np.abs(np.fft.fft2(Yhat - Yhat.mean()))[hf] ** 2))
    retain = e_rec / max(e_orig, 1e-9)
    # gradient energy loss
    gx_o = np.diff(Y, axis=1)
    gx_r = np.diff(Yhat, axis=1)
    grad_loss = 1.0 - (gx_r ** 2).sum() / max((gx_o ** 2).sum(), 1e-9)
    return rmse, retain, float(grad_loss), Yhat


def main():
    table = []
    detailed = {}
    for name, fn in IMAGES.items():
        rgb = fn()
        ycc = rgb2ycbcr(rgb)
        Y = ycc[..., 0]
        edge_col, edge_row = edge_columns_vertical(ycc[..., 1])
        Lloc = local_lipschitz(Y, edge_col)
        cell = []
        for s in SUPPORTS:
            rec_rgb, rec_ycc = subsample_rgb(rgb, R, s, kind="tent")
            span, infl, vspan, vcols = fringe_2d(
                rec_ycc[..., 1], ycc[..., 1], R, s, edge_col)
            pred = R + 2 * s
            rmse, retain, grad_loss, _ = luma_metrics(Y, R, s)
            # analytic RMS trade-off bound:
            # pointwise |err| <= L_rms (s - r/2)  ->  RMS err <= same;
            # expressed in width W = r + 2s:  bound = L_rms (W - 2r)/4
            bound = Lloc * (pred - 2 * R) / 4.0
            row = dict(image=name, s=s, support_px=2 * s, r=R,
                       pred_width=pred, infl_span=span, infl_cols=infl,
                       width_match_1px=abs(span - infl) <= 1.0,
                       vis_span=vspan, vis_cols=vcols,
                       luma_rmse=rmse, analytic_bound=bound,
                       bound_holds=rmse <= bound,
                       L_rms=Lloc,
                       hf_retain=retain, grad_loss=grad_loss)
            table.append(row)
            cell.append(row)
        detailed[name] = cell

    # empirical trade-off coefficient: fit RMSE = alpha + beta*W.
    # RMSE grows with W (wider kernel), so beta > 0 is the RMSE change per
    # 1 px of width; narrowing the fringe by 1 px costs beta RMSE units.
    Cs = []
    for name in IMAGES:
        rows = [r for r in table if r["image"] == name]
        W = np.array([r["infl_span"] for r in rows])
        E = np.array([r["luma_rmse"] for r in rows])
        beta = np.polyfit(W, E, 1)[0]
        Cs.append(beta)
    C = max(Cs)
    summary = dict(r=R, supports=list(SUPPORTS), eps=EPS,
                   width_formula="W_max = r + 2s",
                   tradeoff_C_rmse_per_px=C,
                   per_image_C=dict(zip(IMAGES, Cs)),
                   rows=table)
    with open("/home/ys/feedback/uidreczz28dpNBzBitz/analysis/results.json",
              "w") as f:
        json.dump(summary, f, indent=2,
                  default=lambda o: o.item() if hasattr(o, "item") else str(o))

    hdr = (f"{'image':9} {'s':>4} {'2s':>4} | {'r+2s':>4} {'span':>5} "
           f"{'cols':>4} {'ok':>3} | {'vis':>4} | {'RMSEy':>6} "
           f"{'HFret':>6} {'gradL':>6}")
    print(hdr)
    for r in table:
        print(f"{r['image']:9} {r['s']:4.1f} {r['support_px']:4.1f} | "
              f"{r['pred_width']:4.1f} {r['infl_span']:5.1f} "
              f"{r['infl_cols']:4d} {'Y' if r['width_match_1px'] else 'N':>3} | "
              f"{r['vis_cols']:4d} | {r['luma_rmse']:6.3f} "
              f"{r['hf_retain']:6.3f} {r['grad_loss']:6.3f}")
    print(f"\ntrade-off coefficient C (worst image) = {C:.4f} "
          f"luma-RMSE units gained per 1 px of fringe width removed")
    print("per-image C:", {k: round(v, 4) for k, v in
          summary["per_image_C"].items()})


if __name__ == "__main__":
    main()
