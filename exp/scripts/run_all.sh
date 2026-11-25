#!/usr/bin/env bash
# 端到端复现：1D 理论核验 + 等效核定界/模糊测试 + 2D 图像实验 + 定量图
set -euo pipefail
cd "$(dirname "$0")"
OUT=../output/data
mkdir -p "$OUT" ../output/figures
python3 -I verify_1d.py | tee "$OUT/log_verify_1d.txt"
python3 -I bounds.py  | tee "$OUT/log_bounds.txt"
python3 -I run_2d.py  | tee "$OUT/log_run_2d.txt"
python3 -I make_plots.py
echo "ALL DONE -> ../output (figures/, data/)"
