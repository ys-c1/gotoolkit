# palette_lab — 色域分格加权的调色板桶分配

验证"桶数翻倍画面反而更糟"的根因是桶未按色域分布分配，并用 `w_i = p_i·v_i` 加权的
整数最优分配解决。完整论述见 `report/report.md`。

## 运行

```bash
python3 -I lab/experiment.py     # 主实验: 五档 K=64..1024 实测/预测/两组对照
python3 -I lab/alabama_scan.py   # 细粒度 K 扫描捕捉 Alabama 悖论 + 最小反例
python3 -I lab/plots.py          # 渲染 figures/*.png
```

依赖：numpy（数值）、matplotlib（仅出图）。全程 `-I` 隔离运行。

## 目录

| 路径 | 内容 |
|---|---|
| `lab/gamut.py` | 4×4×4 规则分格, 每格 p_i、支撑体积 v_i（64³ 子体素粒度）、权重 w_i=p_i·v_i |
| `lab/allocate.py` | 边际贪心（整数最优）、Hamilton 最大余数法、O(CK²) 精确 DP |
| `lab/kmeans.py` | 确定性 k-means++/Lloyd 子量化器，带结果缓存 |
| `lab/image_gen.py` | 受控测试图：等像素量、不同支撑体积的三主区 + 长尾撒点 |
| `lab/experiment.py` | 微基准标定 (a_i, r_i)、五档实测、预测曲线、p-only 与 Hamilton 对照 |
| `lab/alabama_scan.py` | K=64..160 逐桶扫描 + 合成最小反例 |
| `lab/plots.py` | 五张图 |
| `data/metrics.json` | 全部数值结果 |
| `data/cells.csv` | 64 格 p/v/w 与各档每格桶数 |
| `data/alabama.json` | 悖论事件与最小反例 |
| `data/*.ppm` | 原图与五档量化结果 |
| `figures/*.png` | prediction / tradeoff / baselines / allocation / alabama |
