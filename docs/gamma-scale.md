# 伽马与缩放交互：暗边偏差及反解补偿

## 1. 符号约定

取题面“直接在编码值上缩放会偏亮”所对应的伽马方向：

\[
E=L^\gamma,\qquad L=E^{1/\gamma},\qquad \gamma=2.2
\]

其中 \(E\) 是编码码值的归一化值，\(L\) 是该约定下的伽马空间值。设暗边模型为“暗电平 \(C_d\) 与黑场 \(0\) 混合，暗边覆盖系数为 \(\rho\)”；在整数盒缩小中，校准过的单列暗边满足 \(\rho=1/N\)，\(N\) 是缩放分母。

原编码值路径：

\[
C_E=\rho C_d
\]

先反伽马、缩放、再加回伽马的参照路径：

\[
C_D=255\left[\rho\left(\frac{C_d}{255}\right)^{1/\gamma}\right]^\gamma
   =\rho^\gamma C_d
\]

因此“编码值路径减参照路径”的有符号偏差为：

\[
\boxed{\Delta(\rho,\gamma,C_d)=C_E-C_D
=C_d\rho\left(1-\rho^{\gamma-1}\right)}
\]

当 \(0<\rho<1,\ \gamma>1\) 时，\(\Delta>0\)，即原路径偏亮。

## 2. 反解补偿量

令补偿系数为 \(K\)，并要求 \(K C_E=C_D\)：

\[
\boxed{K(\rho,\gamma)=\frac{C_D}{C_E}
=\rho^{\gamma-1}}
\]

补偿后的连续值为：

\[
C_K=K C_E=\rho^{\gamma-1}\rho C_d=\rho^\gamma C_d=C_D
\]

所以连续解析式没有方法误差；实际残差只来自 8-bit 取整和用取整后的原路径码值估计局部覆盖率。

对伽马的单调性：

\[
\frac{\partial K}{\partial\gamma}
=\rho^{\gamma-1}\ln\rho<0
\qquad(0<\rho<1)
\]

因此 **乘性系数 \(K\) 随伽马增大而单调减小**；等价地，**压暗幅度 \(1-K\) 或加性修正量 \((K-1)C_E=-\Delta\) 的绝对值随伽马增大而单调增大**。对固定伽马，缩放越强（\(\rho\) 越小），\(K\) 越小、压暗越强。

> 若管线把伽马定义成标准显示解码指数 \(L=E^\Gamma\)，参照式会变成 \(C_D=\rho^{1/\Gamma}C_d\)，有符号偏差为 \(C_d\rho(1-\rho^{1/\Gamma})<0\)，即直接平均编码值通常偏暗而不是偏亮；对应补偿为 \(K_{\rm std}=\rho^{1/\Gamma-1}>1\)。代码中保留了 `linear` 参照模式和 `LinearEdgeBias` / `LinearCompensation`，以免两种伽马方向混淆。

## 3. 三档缩放比与两档画幅复算

固定 \(C_d=32\)、\(\gamma=2.2\)、8-bit 容差 \(\tau=1\) 个码值（归一化 \(1/255\approx0.003922\)）。三档为 \(\rho=1/2,1/4,1/8\)。

下面三行可由 `go run ./cmd/gtk gamma-lab` 复算；每行给出解析偏差、\(C_d=32\) 的取整残差，以及两档画幅上遍历全部暗电平后的最大残差：

```text
rho=1/2: Δ(32)=+9.036 code, R(32)=0; 640x480->320x240 Rmax=1, 1920x1080->960x540 Rmax=1
rho=1/4: Δ(32)=+6.484 code, R(32)=0; 640x480->160x120 Rmax=1, 1920x1080->480x270 Rmax=1
rho=1/8: Δ(32)=+3.670 code, R(32)=0; 640x480->80x60   Rmax=1, 1920x1080->240x135 Rmax=1
```

全局比较一次：三档、两画幅的最大补偿后残差为 \(R_{\max}=1\) code，因此

\[
R_{\max}=1\le \tau=1\ \text{code}
\quad\left(\frac1{255}\le\frac1{255}\right)
\]

满足给定的 8-bit 容差。两档画幅得到相同的最大残差，是因为解析补偿只依赖 \((\rho,\gamma)\)，不依赖绝对画幅；画幅只要求能被缩放分母整除。

## 4. 可切回的路径

新增 API：

```go
api.Scale(img, api.Options{
    Ratio:      0.5,                 // 1/2 | 1/4 | 1/8
    Gamma:      2.2,
    ScaleSpace: "encoded",          // 原路径；可改为 compensated / gamma-dual / linear
})
```

CLI：

```sh
# 原路径：直接平均编码值
go run ./cmd/gtk scale --in edge.pgm --out enc.pgm --ratio 0.5 --space encoded

# 解析补偿路径
go run ./cmd/gtk scale --in edge.pgm --out comp.pgm --ratio 0.5 --space compensated

# 伽马对偶参照路径
go run ./cmd/gtk scale --in edge.pgm --out dual.pgm --ratio 0.5 --space gamma-dual

# 输出三档和两档画幅复算表
go run ./cmd/gtk gamma-lab --gamma 2.2 --tolerance 1
```

测试：

```sh
go test ./internal/scale ./api
```

补偿模式对“0 背景 + 单一暗电平”的盒内边缘用取整后的原路径结果估计 \(\theta=C_E/C_d\)，再应用 \(K(\theta,\gamma)=\theta^{\gamma-1}\)。均匀区域有 \(\theta=1,\ K=1\)，不会被全局压暗。
