#!/usr/bin/env bash
# 批量链路端到端：十二组各跑一次并留机读记录，少一组即失败。
#
# 十二组 = 2 方向(encode/decode) × 3 格式(pgm/bmp/rle) × 2 场景(全成功 good / 带失败 mixed)。
# 另附两个错误码场景：空目录(退出码 3) 与 权限不足(退出码 4)。
#
# 每组在 $REC_DIR 下留一份记录，机读正文即 gtk 的机读输出：
#   失败行: FAIL\t<文件名>\t<原因>          （两列内容，文件名与原因分列）
#   汇总行: SUMMARY\ttotal=<N>\tsucceeded=<N>\tfailed=<N>   （固定四字段）
#
# 退出码约定：0 全成功 / 1 有文件失败 / 2 用法错 / 3 空目录 / 4 权限不足。
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REC_DIR="${1:-$ROOT/artifacts/batch-records}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

GTK="$WORK/gtk"
GEN="$WORK/genbatchfixture"
( cd "$ROOT" && go build -o "$GTK" ./cmd/gtk && go build -o "$GEN" ./cmd/genbatchfixture ) || {
	echo "构建失败" >&2; exit 1; }

FORMATS=(pgm bmp rle)
GOOD_N=5          # 全成功组文件数
MIXED_GOOD=3      # 带失败组中的好文件数
mkdir -p "$REC_DIR"
rm -f "$REC_DIR"/g*.record "$REC_DIR"/index.tsv "$REC_DIR"/error-cases.txt

IDX="$REC_DIR/index.tsv"
echo -e "group\tdirection\tformat\tscenario\texit_code\ttotal\tsucceeded\tfailed" > "$IDX"

groups_run=0
fail() { echo "ASSERT-FAIL: $*" >&2; exit 1; }

# 运行一组并写记录、机读断言。
# 参数: gid direction format scenario expect_exit expect_total expect_ok expect_fail
run_group() {
	local gid="$1" dirn="$2" fmt="$3" scenario="$4"
	local want_exit="$5" want_total="$6" want_ok="$7" want_fail="$8"
	local in="$WORK/$gid/in" out="$WORK/$gid/out"
	mkdir -p "$in"

	if [[ "$dirn" == "encode" ]]; then sub="batch-encode"; else sub="batch-decode"; fi
	"$GEN" good "$in" "$dirn" "$fmt" \
		"$([[ "$scenario" == good ]] && echo "$GOOD_N" || echo "$MIXED_GOOD")" || fail "夹具生成 $gid"
	if [[ "$scenario" == "mixed" ]]; then
		local bad_ext
		if [[ "$dirn" == "encode" ]]; then bad_ext=".pgm"; else bad_ext=".$fmt"; fi
		"$GEN" corrupt "$in" "$dirn" "$fmt" "broken$bad_ext" || fail "坏夹具 $gid"
	fi

	local rec="$REC_DIR/$gid.record"
	{
		echo "# command: gtk $sub --format $fmt --in $in --out $out"
		echo "# scenario: $scenario"
	} > "$rec"

	"$GTK" "$sub" --format "$fmt" --in "$in" --out "$out" >>"$rec" 2>>"$rec.err"
	local got_exit=$?
	[[ $got_exit -eq $want_exit ]] || fail "$gid 退出码=$got_exit 期望=$want_exit"

	# 机读断言：汇总四字段且计数与文件数相等。
	local sumline; sumline="$(grep -m1 '^SUMMARY	' "$rec")" || fail "$gid 缺汇总行"
	local total ok failed
	IFS=$'\t' read -r _ t s f <<<"$sumline"
	total="${t#total=}"; ok="${s#succeeded=}"; failed="${f#failed=}"
	[[ "$total" == "$want_total" ]] || fail "$gid total=$total 期望 $want_total"
	[[ "$ok" == "$want_ok" ]]       || fail "$gid succeeded=$ok 期望 $want_ok"
	[[ "$failed" == "$want_fail" ]] || fail "$gid failed=$failed 期望 $want_fail"
	(( total == ok + failed ))      || fail "$gid total != ok+failed"

	if [[ "$scenario" == "mixed" ]]; then
		# 失败行必须是 前缀+文件名+原因 三列且报出坏文件名。
		local fline; fline="$(grep -m1 '^FAIL	' "$rec")" || fail "$gid 缺失败行"
		awk -F'\t' 'NR==1{ if (NF!=3 || $1!="FAIL" || $2=="" || $3=="") exit 1 }' <<<"$fline" \
			|| fail "$gid 失败行列结构非法: $fline"
		grep -q $'^FAIL\tbroken' "$rec" || fail "$gid 未报出坏文件名"
		# 好文件必须继续产出（失败隔离）。
		[[ "$(find "$out" -type f | wc -l)" -eq "$want_ok" ]] || fail "$gid 好文件未继续产出"
	fi

	echo -e "$gid\t$dirn\t$fmt\t$scenario\t$got_exit\t$total\t$ok\t$failed" >> "$IDX"
	groups_run=$((groups_run+1))
}

gid=1
for dirn in encode decode; do
	for fmt in "${FORMATS[@]}"; do
		# 全成功：退出码 0
		run_group "$(printf 'g%02d' "$gid")" "$dirn" "$fmt" good 0 "$GOOD_N" "$GOOD_N" 0
		gid=$((gid+1))
		# 带失败：退出码 1（失败隔离）
		run_group "$(printf 'g%02d' "$gid")" "$dirn" "$fmt" mixed 1 \
			"$((MIXED_GOOD+1))" "$MIXED_GOOD" 1
		gid=$((gid+1))
	done
done

# ---- 两个错误码场景 ----
: > "$REC_DIR/error-cases.txt"
empty_dir="$WORK/empty"; mkdir -p "$empty_dir"
"$GTK" batch-encode --format bmp --in "$empty_dir" --out "$WORK/empty-out" >>"$REC_DIR/error-cases.txt" 2>&1
ec=$?
[[ $ec -eq 3 ]] || fail "空目录退出码=$ec 期望 3"
echo "empty-dir exit=$ec (want 3)" >> "$REC_DIR/error-cases.txt"

if [[ "$(id -u)" -eq 0 ]]; then
	echo "SKIP permission case: root 绕过权限位" >> "$REC_DIR/error-cases.txt"
else
	locked="$WORK/locked"; mkdir -p "$locked"; chmod 000 "$locked"
	"$GTK" batch-decode --format bmp --in "$locked" --out "$WORK/locked-out" >>"$REC_DIR/error-cases.txt" 2>&1
	ec=$?
	chmod 755 "$locked"
	[[ $ec -eq 4 ]] || fail "权限不足退出码=$ec 期望 4"
	echo "permission-denied exit=$ec (want 4)" >> "$REC_DIR/error-cases.txt"
fi

[[ $groups_run -eq 12 ]] || fail "组数=$groups_run 期望 12，少一组即未完成"
echo "OK: 十二组全部通过，记录在 $REC_DIR (groups=$groups_run)"
cat "$IDX"
