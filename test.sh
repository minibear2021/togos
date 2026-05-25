#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Togos 功能测试脚本
# ============================================================

PORT=19999
TOKEN="test-token-2024"
TEST_DIR="/tmp/togos-test-$$"
SITE_URL="http://localhost:$PORT"
PASS=0
FAIL=0

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

# ---------- 工具函数 ----------

pass() { echo -e "  ${GREEN}PASS${NC} $1"; PASS=$((PASS + 1)); }
fail() { echo -e "  ${RED}FAIL${NC} $1 (期望: $2, 实际: $3)"; FAIL=$((FAIL + 1)); }

api() {
    # api method path [data]
    local method="$1" path="$2" data="${3:-}"
    if [ -n "$data" ]; then
        curl -s -X "$method" "$SITE_URL$path" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data"
    else
        curl -s -X "$method" "$SITE_URL$path" \
            -H "Authorization: Bearer $TOKEN"
    fi
}

api_code() {
    # api_code method path [data] -> HTTP status code
    local method="$1" path="$2" data="${3:-}"
    if [ -n "$data" ]; then
        curl -s -o /dev/null -w "%{http_code}" -X "$method" "$SITE_URL$path" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data"
    else
        curl -s -o /dev/null -w "%{http_code}" -X "$method" "$SITE_URL$path" \
            -H "Authorization: Bearer $TOKEN"
    fi
}

pub_code() {
    # pub_code method path [data] -> HTTP status code
    local method="$1" path="$2" data="${3:-}"
    if [ -n "$data" ]; then
        curl -s -o /dev/null -w "%{http_code}" -X "$method" "$SITE_URL$path" -d "$data"
    else
        curl -s -o /dev/null -w "%{http_code}" -X "$method" "$SITE_URL$path"
    fi
}

json_field() {
    python3 -c "import sys,json; print(json.load(sys.stdin).get('$1',''))" 2>/dev/null || echo ""
}

cleanup() {
    [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    rm -rf "$TEST_DIR" /tmp/togos-upload-*.txt /tmp/togos-upload-*.bin 2>/dev/null || true
}
trap cleanup EXIT

# ---------- 编译和启动 ----------
echo -e "${YELLOW}========== Togos 功能测试 ==========${NC}"
echo ""

cd "$(dirname "$0")"

echo -e "${YELLOW}[1/8] 编译${NC}"
CGO_ENABLED=0 go build -o /tmp/togos-test-bin . 2>&1
[ -f /tmp/togos-test-bin ] && pass "编译成功" || fail "编译成功" "binary exists" "not found"

echo ""
echo -e "${YELLOW}[2/8] 启动服务${NC}"
/tmp/togos-test-bin \
    -data-dir "$TEST_DIR" \
    -listen ":$PORT" \
    -admin-token "$TOKEN" \
    -site-url "$SITE_URL" \
    -max-file-size 1 &
SERVER_PID=$!
sleep 1
kill -0 "$SERVER_PID" 2>/dev/null && pass "服务进程启动" || fail "服务进程启动" "running" "not running"

# 准备测试文件
echo "Hello Togos - This is a test file content." > /tmp/togos-upload-test.txt
dd if=/dev/urandom of=/tmp/togos-upload-big.bin bs=1024 count=2048 2>/dev/null

# ========================================================
# 3. 认证测试
# ========================================================
echo ""
echo -e "${YELLOW}[3/8] API 认证测试${NC}"

CODE=$(curl -s -o /dev/null -w "%{http_code}" "$SITE_URL/api/files")
[ "$CODE" = "401" ] && pass "无 Token → 401" || fail "无 Token → 401" "401" "$CODE"

CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer wrong" "$SITE_URL/api/files")
[ "$CODE" = "403" ] && pass "错误 Token → 403" || fail "错误 Token → 403" "403" "$CODE"

CODE=$(api_code GET /api/files)
[ "$CODE" = "200" ] && pass "正确 Token → 200" || fail "正确 Token → 200" "200" "$CODE"

# ========================================================
# 4. 文件管理 API
# ========================================================
echo ""
echo -e "${YELLOW}[4/8] 文件管理 API${NC}"

# 上传文件
RESP=$(curl -s -X POST "$SITE_URL/api/files" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@/tmp/togos-upload-test.txt")
FILE1_ID=$(echo "$RESP" | json_field id)
echo "$RESP" | grep -q '"id"' && pass "上传文件 → 201" || fail "上传文件 → 201" "has id" "$RESP"
echo "$RESP" | grep -q '"mime_type"' && pass "上传返回 MIME 类型" || fail "上传返回 MIME 类型" "has mime_type" "$RESP"
[ -f "$TEST_DIR/files/${FILE1_ID}.txt" ] && pass "文件持久化到磁盘" || fail "文件持久化到磁盘" "file exists" "not found"

# 上传超大文件 (max 1MB, upload 2MB)
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$SITE_URL/api/files" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@/tmp/togos-upload-big.bin")
[ "$CODE" = "400" ] && pass "超大文件被拦截 → 400" || fail "超大文件被拦截 → 400" "400" "$CODE"

# 本地路径导入
RESP=$(api POST /api/files/local '{"path":"/tmp/togos-upload-test.txt"}')
FILE2_ID=$(echo "$RESP" | json_field id)
echo "$RESP" | grep -q '"id"' && pass "本地路径导入 → 201" || fail "本地路径导入 → 201" "has id" "$RESP"

# 导入不存在路径
CODE=$(api_code POST /api/files/local '{"path":"/tmp/does-not-exist-xyz"}')
[ "$CODE" = "404" ] && pass "导入不存在路径 → 404" || fail "导入不存在路径 → 404" "404" "$CODE"

# 文件列表
RESP=$(api GET /api/files)
COUNT=$(echo "$RESP" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
[ "$COUNT" = "2" ] && pass "文件列表有 2 个" || fail "文件列表有 2 个" "2" "$COUNT"

# 文件详情
RESP=$(api GET "/api/files/$FILE1_ID")
echo "$RESP" | grep -q '"name"' && pass "文件详情含 name" || fail "文件详情含 name" "has name" "$RESP"
echo "$RESP" | grep -q '"size"' && pass "文件详情含 size" || fail "文件详情含 size" "has size" "$RESP"

# 不存在的文件
CODE=$(api_code GET /api/files/99999)
[ "$CODE" = "404" ] && pass "不存在文件 → 404" || fail "不存在文件 → 404" "404" "$CODE"

# ========================================================
# 5. 分享管理 API
# ========================================================
echo ""
echo -e "${YELLOW}[5/8] 分享管理 API${NC}"

# 无限制分享
RESP=$(api POST /api/shares "{\"file_id\":$FILE1_ID}")
SHARE1_CODE=$(echo "$RESP" | json_field code)
SHARE1_ID=$(echo "$RESP" | json_field id)
echo "$RESP" | grep -q '"has_password":false' && pass "无限制分享 → has_password=false" || fail "无限制分享 → has_password=false" "false" "$RESP"

# 带密码分享
RESP=$(api POST /api/shares "{\"file_id\":$FILE1_ID,\"password\":\"mypassword\"}")
SHARE2_CODE=$(echo "$RESP" | json_field code)
echo "$RESP" | grep -q '"has_password":true' && pass "密码分享 → has_password=true" || fail "密码分享 → has_password=true" "true" "$RESP"

# 下载次数限制
RESP=$(api POST /api/shares "{\"file_id\":$FILE1_ID,\"max_downloads\":2}")
SHARE3_CODE=$(echo "$RESP" | json_field code)
echo "$RESP" | grep -q '"max_downloads":2' && pass "下载限制分享 → max_downloads=2" || fail "下载限制分享 → max_downloads=2" "2" "$RESP"

# 有效期分享 (5 秒)
RESP=$(api POST /api/shares "{\"file_id\":$FILE1_ID,\"expires_in\":5}")
SHARE4_CODE=$(echo "$RESP" | json_field code)
echo "$RESP" | grep -q '"expires_at"' && pass "有效期分享 → 含 expires_at" || fail "有效期分享 → 含 expires_at" "present" "$RESP"

# 全保护分享
RESP=$(api POST /api/shares "{\"file_id\":$FILE1_ID,\"password\":\"strong\",\"max_downloads\":1,\"expires_in\":3600}")
SHARE5_CODE=$(echo "$RESP" | json_field code)
SHARE5_ID=$(echo "$RESP" | json_field id)
echo "$RESP" | grep -q "$SITE_URL/s/$SHARE5_CODE" && pass "share_url 正确" || fail "share_url 正确" "$SITE_URL/s/$SHARE5_CODE" "$RESP"

# 分享不存在文件
CODE=$(api_code POST /api/shares '{"file_id":99999}')
[ "$CODE" = "404" ] && pass "分享不存在文件 → 404" || fail "分享不存在文件 → 404" "404" "$CODE"

# 列出分享
RESP=$(api GET /api/shares)
COUNT=$(echo "$RESP" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
[ "$COUNT" = "5" ] && pass "分享列表有 5 个" || fail "分享列表有 5 个" "5" "$COUNT"

# 分享详情
RESP=$(api GET "/api/shares/$SHARE5_ID")
echo "$RESP" | grep -q "$SHARE5_CODE" && pass "分享详情正确" || fail "分享详情正确" "$SHARE5_CODE" "$RESP"

# 不存在分享
CODE=$(api_code GET /api/shares/99999)
[ "$CODE" = "404" ] && pass "不存在分享 → 404" || fail "不存在分享 → 404" "404" "$CODE"

# ========================================================
# 6. 公开访问
# ========================================================
echo ""
echo -e "${YELLOW}[6/8] 公开访问${NC}"

# 无密码分享 — 直接下载
PAGE=$(curl -s "$SITE_URL/s/$SHARE1_CODE")
echo "$PAGE" | grep -q '下载文件' && pass "无密码页含下载按钮" || fail "无密码页含下载按钮" "present" "not found"

CONTENT=$(curl -s "$SITE_URL/s/$SHARE1_CODE/download")
echo "$CONTENT" | grep -q "Hello Togos" && pass "无密码直接下载成功" || fail "无密码直接下载成功" "Hello Togos" "$CONTENT"

# 有密码分享 — 需要密码
PAGE=$(curl -s "$SITE_URL/s/$SHARE2_CODE")
echo "$PAGE" | grep -q 'password' && pass "密码分享页含输入框" || fail "密码分享页含输入框" "present" "not found"

CODE=$(pub_code GET "/s/$SHARE2_CODE/download")
[ "$CODE" = "403" ] && pass "未验证密码下载 → 403" || fail "未验证密码下载 → 403" "403" "$CODE"

# 错误密码
CODE=$(pub_code POST "/s/$SHARE2_CODE" "password=wrong")
[ "$CODE" = "302" ] && pass "错误密码 → 302" || fail "错误密码 → 302" "302" "$CODE"

# 正确密码后下载
COOKIE_JAR=$(mktemp)
curl -s -X POST "$SITE_URL/s/$SHARE2_CODE" -d "password=mypassword" -c "$COOKIE_JAR" > /dev/null
CONTENT=$(curl -s -b "$COOKIE_JAR" "$SITE_URL/s/$SHARE2_CODE/download")
echo "$CONTENT" | grep -q "Hello Togos" && pass "密码验证后下载成功" || fail "密码验证后下载成功" "Hello Togos" "$CONTENT"
rm -f "$COOKIE_JAR"

# 下载次数限制 — 2 次后拒绝
curl -s "$SITE_URL/s/$SHARE3_CODE/download" > /dev/null
curl -s "$SITE_URL/s/$SHARE3_CODE/download" > /dev/null
CODE=$(pub_code GET "/s/$SHARE3_CODE/download")
[ "$CODE" = "410" ] && pass "下载次数用尽 → 410" || fail "下载次数用尽 → 410" "410" "$CODE"

# 全保护分享 (密码 + 1次下载)
COOKIE_JAR=$(mktemp)
curl -s -X POST "$SITE_URL/s/$SHARE5_CODE" -d "password=strong" -c "$COOKIE_JAR" > /dev/null
curl -s -b "$COOKIE_JAR" "$SITE_URL/s/$SHARE5_CODE/download" > /dev/null
CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" "$SITE_URL/s/$SHARE5_CODE/download")
[ "$CODE" = "410" ] && pass "全保护分享次数用尽 → 410" || fail "全保护分享次数用尽 → 410" "410" "$CODE"
rm -f "$COOKIE_JAR"

# 不存在的分享
CODE=$(pub_code GET /s/zzzzzzzz)
[ "$CODE" = "404" ] && pass "不存在分享 → 404" || fail "不存在分享 → 404" "404" "$CODE"

# 有效期分享 — 等 6 秒后过期
echo "  等待 6 秒验证过期..."
sleep 6
CODE=$(pub_code GET "/s/$SHARE4_CODE/download")
[ "$CODE" = "410" ] && pass "过期分享下载 → 410" || fail "过期分享下载 → 410" "410" "$CODE"

# ========================================================
# 7. 删除
# ========================================================
echo ""
echo -e "${YELLOW}[7/8] 删除操作${NC}"

RESP=$(api DELETE "/api/shares/$SHARE5_ID")
echo "$RESP" | grep -q "已删除" && pass "删除分享 → 200" || fail "删除分享 → 200" "已删除" "$RESP"

CODE=$(pub_code GET "/s/$SHARE5_CODE")
[ "$CODE" = "404" ] && pass "删除后分享不可访问" || fail "删除后分享不可访问" "404" "$CODE"

RESP=$(api DELETE "/api/files/$FILE2_ID")
echo "$RESP" | grep -q "已删除" && pass "删除文件 → 200" || fail "删除文件 → 200" "已删除" "$RESP"

COUNT=$(api GET /api/files | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
[ "$COUNT" = "1" ] && pass "文件列表剩余 1 个" || fail "文件列表剩余 1 个" "1" "$COUNT"

# ========================================================
# 8. 速率限制 & 其他
# ========================================================
echo ""
echo -e "${YELLOW}[8/8] 速率限制与边界${NC}"

# 速率限制 (30 req/min per IP for /s/*)
echo "  发送 35 次请求测试速率限制..."
HIT=0
for i in $(seq 1 35); do
    CODE=$(curl -s -o /dev/null -w "%{http_code}" "$SITE_URL/s/$SHARE1_CODE")
    if [ "$CODE" = "429" ]; then HIT=1; break; fi
done
[ "$HIT" = "1" ] && pass "速率限制触发 → 429" || fail "速率限制触发 → 429" "429 hit" "never hit"

# API 路由不受速率限制 (通过认证绕过)
CODE=$(api_code GET /api/files)
[ "$CODE" = "200" ] && pass "API 不受速率限制影响" || fail "API 不受速率限制影响" "200" "$CODE"

# 不存在路由
CODE=$(api_code GET /api/nonexistent)
[ "$CODE" = "404" ] && pass "不存在路由 → 404" || fail "不存在路由 → 404" "404" "$CODE"

# 根路径
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$SITE_URL/")
[ "$CODE" = "404" ] && pass "根路径 → 404" || fail "根路径 → 404" "404" "$CODE"

# ========================================================
# 结果
# ========================================================
echo ""
echo -e "${YELLOW}========================================${NC}"
echo -e "${GREEN}通过: $PASS${NC}"
echo -e "${RED}失败: $FAIL${NC}"
echo ""

if [ "$FAIL" -eq 0 ]; then
    echo -e "${GREEN}全部测试通过！${NC}"
    exit 0
else
    echo -e "${RED}存在 $FAIL 个失败用例${NC}"
    exit 1
fi
