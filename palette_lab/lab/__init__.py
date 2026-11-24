"""palette_lab: 色域分格加权调色板桶分配实验。

模块:
  image_gen  合成受控彩色测试图(密集窄团 + 稀疏广域)
  gamut      RGB 色域规则分格, 统计每格像素占比 p_i、支撑体积 v_i、权重 w_i
  kmeans     无第三方依赖的 k-means++/Lloyd 子量化器
  allocate   整数约束分配求解器(贪心精确解 / Hamilton 基线 / DP 交叉验证)
  experiment 五档桶数实测与预测曲线校验
"""
