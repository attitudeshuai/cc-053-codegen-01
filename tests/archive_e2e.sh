#!/usr/bin/env bash
# 方言调查点档案 · 端到端验收脚本
#
# 用法：
#   docker compose up -d --build
#   BASE_URL=http://localhost:9053 tests/archive_e2e.sh
#
# 依赖：bash 4+、curl、python3
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:9053}"
API="$BASE_URL/api/v1"
PASS=0; FAIL=0

# 唯一后缀，允许重复跑脚本而不被唯一约束挡住
SFX="$(date +%s)"

ok()   { PASS=$((PASS+1)); printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  \033[31m✗ %s\033[0m\n' "$1"; }

# req METHOD PATH [JSON_BODY] [EXPECT_STATUS=200] -> 输出响应体到 /tmp 并回显
http() {
  local method="$1" path="$2" body="${3:-}" expect="${4:-200}"
  local tmp; tmp="$(mktemp)"
  local code
  if [[ -n "$body" ]]; then
    code=$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" \
      -H 'Content-Type: application/json' -d "$body" "$API$path")
  else
    code=$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" "$API$path")
  fi
  RESP="$tmp"
  if [[ "$code" == "$expect" ]]; then
    ok "$method $path → $code"
    return 0
  fi
  bad "$method $path → got $code want $expect; body: $(cat "$tmp")"
  return 1
}

json() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))'"$1"')' "$RESP"; }

echo "== 0. 健康检查 =="
curl -sf "$BASE_URL/healthz" >/dev/null && ok "GET /healthz 200" || { bad "服务未就绪: $BASE_URL"; exit 1; }

echo "== 1. 建档：县 =="
http POST /counties "{\"name\":\"余庆县\",\"code\":\"YUQ-$SFX\",\"province\":\"贵州\"}" 201
COUNTY_ID=$(json "['data']['id']")

echo "== 2. 方言分区多级树 =="
http POST /regions "{\"name\":\"西南官话\",\"level\":1}" 201
R1=$(json "['data']['id']")
http POST /regions "{\"name\":\"川黔片\",\"level\":2,\"parent_id\":$R1}" 201
R2=$(json "['data']['id']")
http POST /regions "{\"name\":\"黔中小片\",\"level\":3,\"parent_id\":$R2}" 201
LEAF_A=$(json "['data']['id']")
http POST /regions "{\"name\":\"黔南小片\",\"level\":3,\"parent_id\":$R2}" 201
LEAF_B=$(json "['data']['id']")
# 层级断档（level 3 直接挂 level 1）必须拒绝
http POST /regions "{\"name\":\"断档区\",\"level\":3,\"parent_id\":$R1}" 400
# 顶级不是 level 1 必须拒绝
http POST /regions "{\"name\":\"伪大区\",\"level\":2}" 400

echo "== 3. 调查点只能挂叶子分区 =="
http POST /survey-points \
  "{\"name\":\"白泥镇-$SFX\",\"code\":\"BN-$SFX\",\"county_id\":$COUNTY_ID,\"region_id\":$R1}" 400
# 挂叶子分区，建档成功
http POST /survey-points \
  "{\"name\":\"白泥镇-$SFX\",\"code\":\"BN-$SFX\",\"county_id\":$COUNTY_ID,\"region_id\":$LEAF_A,\"aliases\":[\"白泥\"]}" 201
P_A=$(json "['data']['id']")

echo "== 4. 一个点只能挂一个下级区：同县重名/别名当场指出 =="
http POST /survey-points \
  "{\"name\":\"白泥镇-$SFX\",\"code\":\"BN-DUP-$SFX\",\"county_id\":$COUNTY_ID}" 409
http POST /survey-points \
  "{\"name\":\"另一个镇-$SFX\",\"code\":\"OTH-$SFX\",\"county_id\":$COUNTY_ID,\"aliases\":[\"白泥\"]}" 409
# 跨县同名不算重（另一个县也可能有白泥镇）
http POST /counties "{\"name\":\"湄潭县\",\"code\":\"MT-$SFX\"}" 201
COUNTY2=$(json "['data']['id']")
http POST /survey-points \
  "{\"name\":\"白泥镇-$SFX\",\"code\":\"BN2-$SFX\",\"county_id\":$COUNTY2}" 201

echo "== 5. 调整归属必须写生效日；挂重当场拒绝 =="
# 当前归属就是 LEAF_A：同区再挂（任何生效日）→ 409
http PUT /survey-points/$P_A/assignment \
  "{\"region_id\":$LEAF_A,\"valid_from\":\"2026-01-01\",\"changed_by\":\"li\"}" 409
# 正常调整：2026-01-01 起改挂黔南小片
http PUT /survey-points/$P_A/assignment \
  "{\"region_id\":$LEAF_B,\"valid_from\":\"2026-01-01\",\"changed_by\":\"li\",\"reason\":\"重新调查后改划\"}" 201
# 再以同一天挂一次（挂重）→ 409
http PUT /survey-points/$P_A/assignment \
  "{\"region_id\":$LEAF_A,\"valid_from\":\"2026-01-01\",\"changed_by\":\"wang\"}" 409
# 往回改（早于现行生效日）→ 409
http PUT /survey-points/$P_A/assignment \
  "{\"region_id\":$LEAF_A,\"valid_from\":\"2025-12-31\",\"changed_by\":\"wang\"}" 409
# 未来再调整一次，正常
http PUT /survey-points/$P_A/assignment \
  "{\"region_id\":$LEAF_A,\"valid_from\":\"2026-06-01\",\"changed_by\":\"wang\"}" 201
# 挂到非叶子 → 400
http PUT /survey-points/$P_A/assignment \
  "{\"region_id\":$R2,\"valid_from\":\"2027-01-01\"}" 400

echo "== 6. 按当时归属回看历史，已发出说法不随后续调整变化 =="
http GET "/survey-points/$P_A/attribution?at=2025-06-01" "" 200
GOT=$(json "['data']['region_id']")
[[ "$GOT" == "$LEAF_A" ]] && ok "2025-06-01 回看（调整前）= 黔中小片($LEAF_A)" || bad "回看 2025-06-01 region=$GOT want $LEAF_A"
http GET "/survey-points/$P_A/attribution?at=2026-03-01" "" 200
GOT=$(json "['data']['region_id']")
[[ "$GOT" == "$LEAF_B" ]] && ok "2026-03-01 回看 = 黔南小片($LEAF_B)" || bad "回看 2026-03-01 region=$GOT want $LEAF_B"
http GET "/survey-points/$P_A/attribution?at=2026-08-01" "" 200
GOT=$(json "['data']['region_id']")
[[ "$GOT" == "$LEAF_A" ]] && ok "2026-08-01 回看 = 黔中小片($LEAF_A)" || bad "回看 2026-08-01 region=$GOT want $LEAF_A"

echo "== 7. 异名同地：认出重复并合成一个，条数对得上 =="
# B 以异名建档（同县），再挂一个发音人
http POST /survey-points \
  "{\"name\":\"白泥关口音-$SFX\",\"code\":\"BNK-$SFX\",\"county_id\":$COUNTY_ID,\"region_id\":$LEAF_B}" 201
P_B=$(json "['data']['id']")
http POST /speakers \
  "{\"code_name\":\"sp-$SFX-1\",\"birth_year\":1960,\"gender\":\"male\",\"dialect_point_code\":\"BNK-$SFX\",\"survey_point_id\":$P_B}" 201

http GET "/survey-points/$P_A/counts" "" 200
A_SPK_BEFORE=$(json "['data']['speakers']")
http GET "/survey-points/$P_B/counts" "" 200
B_SPK_BEFORE=$(json "['data']['speakers']")

http POST /survey-points/merge "{\"kept_point_id\":$P_A,\"merged_point_id\":$P_B}" 200
BAL=$(json "['data']['balanced']")
[[ "$BAL" == "True" ]] && ok "合并对账 balanced=true" || bad "合并对账不平衡: $(cat "$RESP")"

# 合并后：旧档案 id 自动归并；发音人条数两边相加
http GET "/survey-points/$P_B/counts" "" 200
SPK_AFTER=$(json "['data']['speakers']")
EXPECT=$((A_SPK_BEFORE + B_SPK_BEFORE))
[[ "$SPK_AFTER" == "$EXPECT" ]] && ok "条数对账 speakers $A_SPK_BEFORE+$B_SPK_BEFORE=$SPK_AFTER" \
  || bad "speakers 条数 after=$SPK_AFTER want=$EXPECT"

# 旧点名作为别名留在保留点上；再用旧名建档必须被认重拦截
http POST /survey-points \
  "{\"name\":\"白泥关口音-$SFX\",\"code\":\"BNK2-$SFX\",\"county_id\":$COUNTY_ID}" 409
# 已合并的点不能再调整归属
http PUT /survey-points/$P_B/assignment \
  "{\"region_id\":$LEAF_A,\"valid_from\":\"2026-09-01\"}" 409
# 不同县不许自动合并
http GET "/survey-points?county_id=$COUNTY2&q=白泥" "" 200
http POST /survey-points/merge "{\"kept_point_id\":$P_A,\"merged_point_id\":$(json "['data']['items'][0]['id']")}" 409

echo
if [[ "$FAIL" -eq 0 ]]; then
  printf '\033[32m全部 %d 项断言通过\033[0m\n' "$PASS"
else
  printf '\033[31m%d/%d 项断言失败\033[0m\n' "$FAIL" "$((PASS+FAIL))"
  exit 1
fi
