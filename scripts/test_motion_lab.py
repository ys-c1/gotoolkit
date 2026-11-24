#!/usr/bin/env python3
"""Deterministic checks for the curvature-aware motion experiment."""

from __future__ import annotations

import math
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import numpy as np

import motion_lab as ml


class MotionLabTests(unittest.TestCase):
    def test_parabolic_vertex_exact_quadratic(self) -> None:
        # J(s) = 3 + 2*(s - 0.25)^2 evaluated around zero with h=0.5.
        h = 0.5
        true_delta = 0.25
        jm = 3.0 + 2.0 * (-h - true_delta) ** 2
        j0 = 3.0 + 2.0 * true_delta**2
        jp = 3.0 + 2.0 * (h - true_delta) ** 2
        delta, jmin = ml.parabolic_vertex_1d(jm, j0, jp, h)
        self.assertAlmostEqual(delta, true_delta, places=12)
        self.assertAlmostEqual(jmin, 3.0, places=10)

    def test_guaranteed_descent_formula(self) -> None:
        grad = 4.0
        kappa = 8.0
        given = 0.25
        step, max_drop = ml.guaranteed_descent_step(grad, kappa, given)
        self.assertAlmostEqual(max_drop, 1.0)
        self.assertIsNotNone(step)
        assert step is not None
        predicted = grad * step - 0.5 * kappa * step**2
        self.assertGreaterEqual(predicted, given - 1e-12)
        none_step, small_max = ml.guaranteed_descent_step(0.1, kappa, given)
        self.assertIsNone(none_step)
        self.assertLess(small_max, given)

    def test_rounding_bounds_hold_for_random_positions(self) -> None:
        audit = ml.random_rounding_audit(seed=123)
        self.assertLessEqual(audit["max_integer_l2"], audit["integer_bound_l2"] + 1e-12)
        self.assertLessEqual(audit["max_subgrid_l2"], audit["subgrid_bound_l2"] + 1e-12)

    def test_three_velocities_improve_over_integer(self) -> None:
        for case in ml.CASES:
            result, _extra, _grid, _surface = ml.run_case(case)
            self.assertLess(result.rms_subpixel, result.rms_integer)
            self.assertLess(result.truth_error_px, 0.06)
            self.assertLessEqual(
                result.subgrid_round_error_px,
                result.subgrid_round_bound_px + 1e-12,
            )
            self.assertLessEqual(
                result.rounding_mse_increase,
                result.rounding_mse_bound + 1e-9,
            )
            accepted = [t for t in result.trace if t.get("accepted")]
            self.assertTrue(accepted)
            self.assertTrue(all(t["actual_drop"] >= 1e-3 - 1e-10 for t in accepted))


if __name__ == "__main__":
    unittest.main(verbosity=2)
