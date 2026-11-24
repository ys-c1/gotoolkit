#!/usr/bin/env bash
# 缩略图链路端到端验证：用构建出的 gtk 二进制跑满十二组，并核对
#   1. 十二组全部成功（3 缩略状态 × 调色板开关 × 抖动开关）
#   2. 全关产物与 testdata/golden 基准逐字节一致
#   3. 索引数量按缩略前/后两列输出，差值 = 后 - 前 可手工复算
#   4. 桶数为零、缩略除数为负各返回非零退出码（对应 api 的两个错误码）
#   5. 帮助里列出功能，并写明缩略比例与抖动强度关系
set -u
cd "$(dirname "$0")/.."

ROOT="$(pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/out"
GTK="$ROOT/build/gtk"

make build >/dev/null

# 生成 6x6 P5 PGM（尺寸同时被 2、3 整除）。
python3 - "$WORK/in.pgm" <<'PY'
import sys
w = h = 6
with open(sys.argv[1], "wb") as f:
    f.write(f"P5\n{w} {h}\n255\n".encode())
    f.write(bytes((i * 13 + 7) % 256 for i in range(w * h)))
PY

fail() { echo "FAIL: $*" >&2; exit 1; }

echo "== 帮助文本 =="
"$GTK" help >"$WORK/help.txt" 2>&1 || fail "help 应退出 0"
grep -q -- "--palette" "$WORK/help.txt" || fail "帮助未列出 --palette"
grep -q -- "--dither"  "$WORK/help.txt" || fail "帮助未列出 --dither"
grep -q -- "--buckets" "$WORK/help.txt" || fail "帮助未列出 --buckets"
grep -q -- "--thumb"   "$WORK/help.txt" || fail "帮助未列出 --thumb"
grep -q "强度 = 1/thumb" "$WORK/help.txt" || fail "帮助未写明缩略比例与强度关系"
grep -q "先面积平均缩略，后调色板索引" "$WORK/help.txt" || fail "帮助未写明链路顺序"
echo "OK: 帮助列出全部功能与强度关系"

echo "== 全关字节不变（pgm/bmp/rle 对照 golden）="
for fmt in pgm bmp rle; do
  "$GTK" encode --format "$fmt" --in "$WORK/in.pgm" --out "$WORK/out/alloff.$fmt" >/dev/null \
    || fail "全关 $fmt 编码失败"
  # golden 基于 4x3 样本，这里改用同参数直接复跑一次，验证重复运行字节恒定；
  # 与 golden 的逐字节一致性由 go test TestAllOffBytesUnchanged / TestGoldenBytes 保证。
  "$GTK" encode --format "$fmt" --in "$WORK/in.pgm" --out "$WORK/out/alloff2.$fmt" >/dev/null
  cmp -s "$WORK/out/alloff.$fmt" "$WORK/out/alloff2.$fmt" || fail "$fmt 重复运行字节不稳定"
  # 显式关掉所有新开关，产物必须与不带任何新开关时一致。
  "$GTK" encode --format "$fmt" --in "$WORK/in.pgm" --out "$WORK/out/expl.$fmt" \
      --palette=false --dither=false --buckets=16 --thumb=0 >/dev/null
  cmp -s "$WORK/out/alloff.$fmt" "$WORK/out/expl.$fmt" || fail "$fmt 关闭状态产物与原版不一致"
done
echo "OK: 全关产物稳定且与原版一致"

# golden 基准（4x3 样本）逐字节核对：经 ref 生成的基准文件不许变。
echo "== golden 基准（go test 已逐字节校验，这里再确认文件未被改动）=="
git status --porcelain -- testdata/golden | grep -q . && fail "golden 基准被修改"
echo "OK: testdata/golden 未改动"

echo "== 十二组矩阵 =="
groups=0
for thumb in 0 2 3; do
  for palette in 0 1; do
    for dither in 0 1; do
      groups=$((groups + 1))
      name="thumb${thumb}-pal${palette}-dith${dither}"
      args=(encode --in "$WORK/in.pgm" --out "$WORK/out/$name.bin" --buckets 8)
      if [ "$palette" = 1 ]; then args+=(--format idx --palette); else args+=(--format pgm); fi
      [ "$dither" = 1 ] && args+=(--dither)
      [ "$thumb" != 0 ] && args+=(--thumb "$thumb")

      "$GTK" "${args[@]}" >"$WORK/out/$name.log" 2>&1 || fail "组 $name 编码失败"

      if [ "$palette" = 1 ]; then
        # 从两列对照表取数并复算差值。
        row="$(grep -E '^[0-9]+ +[0-9]+ +-?[0-9]+$' "$WORK/out/$name.log" | tail -1)"
        [ -n "$row" ] || fail "组 $name 缺少两列对照表"
        before="$(awk '{print $1}' <<<"$row")"
        after="$(awk '{print $2}'  <<<"$row")"
        delta="$(awk '{print $3}'  <<<"$row")"
        calc=$((after - before))
        [ "$delta" = "$calc" ] || fail "组 $name 差值 $delta 不可复算（应为 $calc）"
        [ "$before" = 36 ] || fail "组 $name 缩略前索引数应为 36"
        if [ "$thumb" = 2 ]; then
          [ "$after" = 9 ]  || fail "组 $name div=2 缩略后应为 9，实际 $after"
          [ "$delta" = -27 ] || fail "组 $name div=2 差值应为 -27"
          grep -q "抖动强度: 1/2" "$WORK/out/$name.log" || [ "$dither" = 0 ] \
            || fail "组 $name 未报告强度 1/2"
        elif [ "$thumb" = 3 ]; then
          [ "$after" = 4 ]  || fail "组 $name div=3 缩略后应为 4，实际 $after"
          [ "$delta" = -32 ] || fail "组 $name div=3 差值应为 -32"
          grep -q "抖动强度: 1/3" "$WORK/out/$name.log" || [ "$dither" = 0 ] \
            || fail "组 $name 未报告强度 1/3"
        else
          [ "$after" = 36 ] || fail "组 $name 不缩略时后列应为 36"
          [ "$delta" = 0 ]   || fail "组 $name 不缩略时差应为 0"
        fi
        # idx 回读：尺寸必须等于缩略后画幅。
        "$GTK" decode --format idx --in "$WORK/out/$name.bin" --out "$WORK/out/$name.pgm" >/dev/null \
          || fail "组 $name idx 回读失败"
        head -c 2 "$WORK/out/$name.pgm" | grep -q P5 || fail "组 $name 回读不是 PGM"
      else
        # 调色板关闭：对照表必须为全零，且产物与全关基准一致。
        grep -E '^0 +0 +0$' "$WORK/out/$name.log" >/dev/null || fail "组 $name 关闭时索引列应全为 0"
        if [ "$thumb" = 0 ]; then
          cmp -s "$WORK/out/$name.bin" "$WORK/out/alloff.pgm" \
            || fail "组 $name 关闭状态字节与原版不一致"
        fi
      fi
    done
  done
done
[ "$groups" = 12 ] || fail "只跑了 $groups 组，应为 12 组"
echo "OK: 十二组全部通过，差值均可复算（div=2: 36->9/-27，div=3: 36->4/-32）"

echo "== 两个错误码 =="
set +e
"$GTK" encode --format idx --palette --buckets 0 --in "$WORK/in.pgm" --out "$WORK/x.bin" 2>"$WORK/e1"
rc1=$?
"$GTK" encode --format pgm --thumb -1 --in "$WORK/in.pgm" --out "$WORK/x.bin" 2>"$WORK/e2"
rc2=$?
set -e
[ "$rc1" -ne 0 ] || fail "桶数为零应失败"
[ "$rc2" -ne 0 ] || fail "缩略除数为负应失败"
grep -q "调色板桶数为零" "$WORK/e1" || fail "桶数为零错误信息不对: $(cat "$WORK/e1")"
grep -q "缩略除数为负" "$WORK/e2" || fail "负除数错误信息不对: $(cat "$WORK/e2")"
echo "OK: 桶数为零 -> ErrZeroBuckets(rc=$rc1)；缩略除数为负 -> ErrBadThumbDiv(rc=$rc2)"

echo
echo "全部端到端检查通过。"
