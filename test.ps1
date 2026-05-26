#!/usr/bin/env pwsh
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ============================================================
# Togos 功能测试脚本 (Windows PowerShell)
# ============================================================

$PORT = 19999
$TOKEN = "test-token-2024"
$TEST_DIR = Join-Path $env:TEMP "togos-test-$PID"
$SITE_URL = "http://localhost:$PORT"
$TMP_DIR = Join-Path $env:TEMP "togos-tmp-$PID"
$PASS = 0
$FAIL = 0

# ---------- 工具函数 ----------

function pass([string]$msg) {
    Write-Host "  $([char]0x1b)[0;32mPASS$([char]0x1b)[0m $msg"
    $script:PASS++
}

function fail([string]$msg, [string]$expected, [string]$actual) {
    Write-Host "  $([char]0x1b)[0;31mFAIL$([char]0x1b)[0m $msg (期望: $expected, 实际: $actual)"
    $script:FAIL++
}

# Write JSON to temp file and call curl --data-binary to avoid escaping issues
function api_json([string]$method, [string]$path, $body = $null) {
    $uri = "$SITE_URL$path"
    if ($body) {
        $tmpFile = Join-Path $TMP_DIR "api-$PID-$([Guid]::NewGuid()).json"
        ($body | ConvertTo-Json -Compress) | Out-File -FilePath $tmpFile -Encoding ASCII -NoNewline
        $result = curl.exe -s -X $method $uri -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" --data-binary "@$tmpFile" 2>$null
        Remove-Item $tmpFile -Force -ErrorAction SilentlyContinue
    } else {
        $result = curl.exe -s -X $method $uri -H "Authorization: Bearer $TOKEN" 2>$null
    }
    return $result
}

function api_code([string]$method, [string]$path, $body = $null) {
    $uri = "$SITE_URL$path"
    if ($body) {
        $tmpFile = Join-Path $TMP_DIR "api-$PID-$([Guid]::NewGuid()).json"
        ($body | ConvertTo-Json -Compress) | Out-File -FilePath $tmpFile -Encoding ASCII -NoNewline
        $result = curl.exe -s -o nul -w "%{http_code}" -X $method $uri -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" --data-binary "@$tmpFile" 2>$null
        Remove-Item $tmpFile -Force -ErrorAction SilentlyContinue
    } else {
        $result = curl.exe -s -o nul -w "%{http_code}" -X $method $uri -H "Authorization: Bearer $TOKEN" 2>$null
    }
    return $result
}

function pub_code([string]$method, [string]$path, [string]$data = "") {
    $uri = "$SITE_URL$path"
    if ($data) {
        $result = curl.exe -s -o nul -w "%{http_code}" -X $method $uri -d $data 2>$null
    } else {
        $result = curl.exe -s -o nul -w "%{http_code}" -X $method $uri 2>$null
    }
    return $result
}

function json_field([string]$json, [string]$field) {
    try {
        $obj = $json | ConvertFrom-Json
        return $obj.$field
    } catch {
        return ""
    }
}

# ---------- 清理 ----------

$SERVER_PID = $null
function cleanup {
    if ($SERVER_PID) {
        Stop-Process -Id $SERVER_PID -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -Recurse -Force $TEST_DIR -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force $TMP_DIR -ErrorAction SilentlyContinue
    Remove-Item -Force (Join-Path $env:TEMP "togos-upload-test.txt") -ErrorAction SilentlyContinue
    Remove-Item -Force (Join-Path $env:TEMP "togos-upload-big.bin") -ErrorAction SilentlyContinue
    Remove-Item -Force (Join-Path $env:TEMP "togos-cookies-*.txt") -ErrorAction SilentlyContinue
}

# Create tmp dir
New-Item -ItemType Directory -Force $TMP_DIR | Out-Null

# ========================================================
# 1. 编译
# ========================================================
Write-Host "$([char]0x1b)[0;33m========== Togos 功能测试 (Windows) ==========$([char]0x1b)[0m"
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[1/8] 编译$([char]0x1b)[0m"

$env:CGO_ENABLED = "0"
$BIN = Join-Path $TMP_DIR "togos-test-bin.exe"
go build -o $BIN . 2>&1 | Out-Null
if (Test-Path $BIN) { pass "编译成功" } else { fail "编译成功" "binary exists" "not found"; exit 1 }

# ========================================================
# 2. 启动服务
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[2/8] 启动服务$([char]0x1b)[0m"

$procArgs = @(
    "-data-dir", $TEST_DIR,
    "-listen", ":$PORT",
    "-admin-token", $TOKEN,
    "-site-url", $SITE_URL,
    "-max-file-size", "1"
)
$proc = Start-Process -FilePath $BIN -ArgumentList $procArgs -PassThru -WindowStyle Hidden
$SERVER_PID = $proc.Id
Start-Sleep -Seconds 2

try { $null = Get-Process -Id $SERVER_PID -ErrorAction Stop; pass "服务进程启动" } catch {
    fail "服务进程启动" "running" "not running"
    cleanup
    exit 1
}

# 准备测试文件
"Hello Togos - This is a test file content from Windows." | Out-File -FilePath (Join-Path $env:TEMP "togos-upload-test.txt") -Encoding UTF8
$bigFile = Join-Path $env:TEMP "togos-upload-big.bin"
$bytes = New-Object byte[] (2 * 1024 * 1024)
(New-Object Random).NextBytes($bytes)
[IO.File]::WriteAllBytes($bigFile, $bytes)

# ========================================================
# 3. 认证测试
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[3/8] API 认证测试$([char]0x1b)[0m"

$CODE = pub_code GET "/api/files"
if ($CODE -eq "401") { pass "无 Token → 401" } else { fail "无 Token → 401" "401" "$CODE" }

$CODE = curl.exe -s -o nul -w "%{http_code}" -H "Authorization: Bearer wrong" "$SITE_URL/api/files" 2>$null
if ($CODE -eq "403") { pass "错误 Token → 403" } else { fail "错误 Token → 403" "403" "$CODE" }

$CODE = api_code GET "/api/files"
if ($CODE -eq "200") { pass "正确 Token → 200" } else { fail "正确 Token → 200" "200" "$CODE" }

# ========================================================
# 4. 文件管理 API
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[4/8] 文件管理 API$([char]0x1b)[0m"

# 上传文件
$uploadFile = Join-Path $env:TEMP "togos-upload-test.txt"
$RESP = curl.exe -s -X POST "$SITE_URL/api/files" -H "Authorization: Bearer $TOKEN" -F "file=@$uploadFile" 2>$null
$FILE1_ID = json_field $RESP "id"
if ($RESP -match '"id"') { pass "上传文件 → 201" } else { fail "上传文件 → 201" "has id" "$RESP" }
if ($RESP -match '"mime_type"') { pass "上传返回 MIME 类型" } else { fail "上传返回 MIME 类型" "has mime_type" "$RESP" }
$diskPath = Join-Path (Join-Path $TEST_DIR "files") "$FILE1_ID.txt"
if (Test-Path $diskPath) { pass "文件持久化到磁盘" } else { fail "文件持久化到磁盘" "file exists" "not found" }

# 上传超大文件
$CODE = curl.exe -s -o nul -w "%{http_code}" -X POST "$SITE_URL/api/files" -H "Authorization: Bearer $TOKEN" -F "file=@$bigFile" 2>$null
if ($CODE -eq "400") { pass "超大文件被拦截 → 400" } else { fail "超大文件被拦截 → 400" "400" "$CODE" }

# 本地路径导入 (ConvertTo-Json 自动处理转义)
$RESP = api_json POST "/api/files/local" @{ path = $uploadFile }
$FILE2_ID = json_field $RESP "id"
if ($RESP -match '"id"') { pass "本地路径导入 → 201" } else { fail "本地路径导入 → 201" "has id" "$RESP" }

# 导入不存在路径
$CODE = api_code POST "/api/files/local" @{ path = "C:\does-not-exist-xyz" }
if ($CODE -eq "404") { pass "导入不存在路径 → 404" } else { fail "导入不存在路径 → 404" "404" "$CODE" }

# 文件列表
$RESP = api_json GET "/api/files"
$COUNT = ($RESP | ConvertFrom-Json).Count
if ($COUNT -eq 2) { pass "文件列表有 2 个" } else { fail "文件列表有 2 个" "2" "$COUNT" }

# 文件详情
$RESP = api_json GET "/api/files/$FILE1_ID"
if ($RESP -match '"name"') { pass "文件详情含 name" } else { fail "文件详情含 name" "has name" "$RESP" }
if ($RESP -match '"size"') { pass "文件详情含 size" } else { fail "文件详情含 size" "has size" "$RESP" }

# 文件详情含 shares
$RESP = api_json GET "/api/files/$FILE1_ID"
if ($RESP -match '"shares"') { pass "文件详情含 shares 字段" } else { fail "文件详情含 shares 字段" "has shares" "$RESP" }

# 不存在的文件
$CODE = api_code GET "/api/files/99999"
if ($CODE -eq "404") { pass "不存在文件 → 404" } else { fail "不存在文件 → 404" "404" "$CODE" }

# ========================================================
# 5. 分享管理 API
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[5/8] 分享管理 API$([char]0x1b)[0m"

# 无限制分享
$RESP = api_json POST "/api/shares" @{ file_id = [int64]$FILE1_ID }
$SHARE1_CODE = json_field $RESP "code"
$SHARE1_ID = json_field $RESP "id"
if ($RESP -match '"has_password":false') { pass "无限制分享 → has_password=false" } else { fail "无限制分享 → has_password=false" "false" "$RESP" }

# 带密码分享
$RESP = api_json POST "/api/shares" @{ file_id = [int64]$FILE1_ID; password = "mypassword" }
$SHARE2_CODE = json_field $RESP "code"
if ($RESP -match '"has_password":true') { pass "密码分享 → has_password=true" } else { fail "密码分享 → has_password=true" "true" "$RESP" }

# 下载次数限制
$RESP = api_json POST "/api/shares" @{ file_id = [int64]$FILE1_ID; max_downloads = 2 }
$SHARE3_CODE = json_field $RESP "code"
if ($RESP -match '"max_downloads":2') { pass "下载限制分享 → max_downloads=2" } else { fail "下载限制分享 → max_downloads=2" "2" "$RESP" }

# 有效期分享 (5 秒)
$RESP = api_json POST "/api/shares" @{ file_id = [int64]$FILE1_ID; expires_in = 5 }
$SHARE4_CODE = json_field $RESP "code"
if ($RESP -match '"expires_at"') { pass "有效期分享 → 含 expires_at" } else { fail "有效期分享 → 含 expires_at" "present" "$RESP" }

# 全保护分享
$RESP = api_json POST "/api/shares" @{ file_id = [int64]$FILE1_ID; password = "strong"; max_downloads = 1; expires_in = 3600 }
$SHARE5_CODE = json_field $RESP "code"
$SHARE5_ID = json_field $RESP "id"
if ($RESP -match "$SITE_URL/s/$SHARE5_CODE") { pass "share_url 正确" } else { fail "share_url 正确" "$SITE_URL/s/$SHARE5_CODE" "$RESP" }

# 分享不存在文件
$CODE = api_code POST "/api/shares" @{ file_id = 99999 }
if ($CODE -eq "404") { pass "分享不存在文件 → 404" } else { fail "分享不存在文件 → 404" "404" "$CODE" }

# 列出分享
$RESP = api_json GET "/api/shares"
$COUNT = ($RESP | ConvertFrom-Json).Count
if ($COUNT -eq 5) { pass "分享列表有 5 个" } else { fail "分享列表有 5 个" "5" "$COUNT" }

# 分享详情
$RESP = api_json GET "/api/shares/$SHARE5_ID"
if ($RESP -match $SHARE5_CODE) { pass "分享详情正确" } else { fail "分享详情正确" $SHARE5_CODE "$RESP" }

# 不存在分享
$CODE = api_code GET "/api/shares/99999"
if ($CODE -eq "404") { pass "不存在分享 → 404" } else { fail "不存在分享 → 404" "404" "$CODE" }

# ---------- PATCH 更新分享 ----------

# 更新密码
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ password = "updatedpass" }
if ($RESP -match '"has_password":true') { pass "PATCH 更新密码成功" } else { fail "PATCH 更新密码成功" "true" "$RESP" }

# 清除密码
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ password = "" }
if ($RESP -match '"has_password":false') { pass "PATCH 清除密码" } else { fail "PATCH 清除密码" "false" "$RESP" }

# 更新下载次数限制
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ max_downloads = 100 }
if ($RESP -match '"max_downloads":100') { pass "PATCH 更新 max_downloads" } else { fail "PATCH 更新 max_downloads" "100" "$RESP" }

# 重置下载计数
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ download_count = 0 }
if ($RESP -match '"download_count":0') { pass "PATCH 重置 download_count" } else { fail "PATCH 重置 download_count" "0" "$RESP" }

# 更新有效期
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ expires_in = 7200 }
if ($RESP -match '"expires_at"') { pass "PATCH 更新有效期" } else { fail "PATCH 更新有效期" "has expires_at" "$RESP" }

# 清除有效期
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ expires_in = 0 }
if ($RESP -match '"expires_at":""') { pass "PATCH 清除有效期" } else { fail "PATCH 清除有效期" '""' "$RESP" }

# 禁用分享
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ is_active = $false }
if ($RESP -match '"is_active":false') { pass "PATCH 禁用分享" } else { fail "PATCH 禁用分享" "false" "$RESP" }

# 启用分享
$RESP = api_json PATCH "/api/shares/$SHARE5_ID" @{ is_active = $true }
if ($RESP -match '"is_active":true') { pass "PATCH 启用分享" } else { fail "PATCH 启用分享" "true" "$RESP" }

# 空请求体应返回 400
$CODE = api_code PATCH "/api/shares/$SHARE5_ID" @{}
if ($CODE -eq "400") { pass "PATCH 空字段 → 400" } else { fail "PATCH 空字段 → 400" "400" "$CODE" }

# PATCH 不存在分享
$CODE = api_code PATCH "/api/shares/99999" @{ max_downloads = 5 }
if ($CODE -eq "404") { pass "PATCH 不存在分享 → 404" } else { fail "PATCH 不存在分享 → 404" "404" "$CODE" }

# ---------- 按分享码操作 ----------

# 按码查询
$RESP = api_json GET "/api/shares/$SHARE1_CODE"
if ($RESP -match $SHARE1_CODE) { pass "按码查询分享成功" } else { fail "按码查询分享成功" $SHARE1_CODE "$RESP" }

# 按码更新（创建临时分享）
$RESP = api_json POST "/api/shares" @{ file_id = [int64]$FILE1_ID }
$TMP_CODE = json_field $RESP "code"
$RESP = api_json PATCH "/api/shares/$TMP_CODE" @{ max_downloads = 50 }
if ($RESP -match '"max_downloads":50') { pass "按码更新分享成功" } else { fail "按码更新分享成功" "50" "$RESP" }

# 按码删除
$CODE = api_code DELETE "/api/shares/$TMP_CODE"
if ($CODE -eq "200") { pass "按码删除分享成功" } else { fail "按码删除分享成功" "200" "$CODE" }

# 无效分享码
$CODE = api_code GET "/api/shares/abc"
if ($CODE -eq "400") { pass "无效分享标识 → 400" } else { fail "无效分享标识 → 400" "400" "$CODE" }

# ---------- 分享码格式校验 ----------
$ALL_MIXED = $true
foreach ($sc in @($SHARE1_CODE, $SHARE2_CODE, $SHARE3_CODE, $SHARE5_CODE)) {
    if ($sc -and (-not ($sc -match '[0-9]' -and $sc -match '[a-z]'))) {
        $ALL_MIXED = $false
    }
}
if ($SHARE1_CODE -and $ALL_MIXED) { pass "分享码含字母和数字" } else { fail "分享码含字母和数字" "mixed" "pure" }

# ---------- 文件详情 shares 验证 ----------
$RESP = api_json GET "/api/files/$FILE1_ID"
if ($RESP -match '"has_password"') { pass "文件详情 shares 含 has_password" } else { fail "文件详情 shares 含 has_password" "present" "$RESP" }

# ========================================================
# 6. 公开访问
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[6/8] 公开访问$([char]0x1b)[0m"

# 无密码分享 — 直接下载
$PAGE = curl.exe -s "$SITE_URL/s/$SHARE1_CODE" 2>$null
if ($PAGE -match '/s/.*/download') { pass "无密码页含下载按钮" } else { fail "无密码页含下载按钮" "present" "not found" }

$CONTENT = curl.exe -s "$SITE_URL/s/$SHARE1_CODE/download" 2>$null
if ($CONTENT -match "Hello Togos") { pass "无密码直接下载成功" } else { fail "无密码直接下载成功" "Hello Togos" "$CONTENT" }

# 有密码分享 — 需要密码
$PAGE = curl.exe -s "$SITE_URL/s/$SHARE2_CODE" 2>$null
if ($PAGE -match 'password') { pass "密码分享页含输入框" } else { fail "密码分享页含输入框" "present" "not found" }

$CODE = pub_code GET "/s/$SHARE2_CODE/download"
if ($CODE -eq "403") { pass "未验证密码下载 → 403" } else { fail "未验证密码下载 → 403" "403" "$CODE" }

# 错误密码
$CODE = pub_code POST "/s/$SHARE2_CODE" "password=wrong"
if ($CODE -eq "302") { pass "错误密码 → 302" } else { fail "错误密码 → 302" "302" "$CODE" }

# 正确密码后下载
$COOKIE_FILE = Join-Path $TMP_DIR "cookies.txt"
curl.exe -s -X POST "$SITE_URL/s/$SHARE2_CODE" -d "password=mypassword" -c $COOKIE_FILE 2>$null | Out-Null
$CONTENT = curl.exe -s -b $COOKIE_FILE "$SITE_URL/s/$SHARE2_CODE/download" 2>$null
if ($CONTENT -match "Hello Togos") { pass "密码验证后下载成功" } else { fail "密码验证后下载成功" "Hello Togos" "$CONTENT" }

# 下载次数限制 — 2 次后拒绝 (下载端点 302 重定向到分享页面)
curl.exe -s "$SITE_URL/s/$SHARE3_CODE/download" 2>$null | Out-Null
curl.exe -s "$SITE_URL/s/$SHARE3_CODE/download" 2>$null | Out-Null
$CODE = curl.exe -s -o nul -w "%{http_code}" -L "$SITE_URL/s/$SHARE3_CODE/download" 2>$null
if ($CODE -eq "410") { pass "下载次数用尽 → 410" } else { fail "下载次数用尽 → 410" "410" "$CODE" }

# 全保护分享（密码 + 1次下载，用新分享，SHARE5 已被 PATCH 修改）
$RESP6 = api_json POST "/api/shares" @{ file_id = [int64]$FILE1_ID; password = "strong2"; max_downloads = 1 }
$SHARE6_CODE = json_field $RESP6 "code"
curl.exe -s -X POST "$SITE_URL/s/$SHARE6_CODE" -d "password=strong2" -c $COOKIE_FILE 2>$null | Out-Null
curl.exe -s -b $COOKIE_FILE "$SITE_URL/s/$SHARE6_CODE/download" 2>$null | Out-Null
$CODE = curl.exe -s -o nul -w "%{http_code}" -L -b $COOKIE_FILE "$SITE_URL/s/$SHARE6_CODE/download" 2>$null
if ($CODE -eq "410") { pass "全保护分享次数用尽 → 410" } else { fail "全保护分享次数用尽 → 410" "410" "$CODE" }

# 不存在的分享
$CODE = pub_code GET "/s/zzzzzzzz"
if ($CODE -eq "404") { pass "不存在分享 → 404" } else { fail "不存在分享 → 404" "404" "$CODE" }

# 禁用分享测试
api_json PATCH "/api/shares/$SHARE1_ID" @{ is_active = $false } | Out-Null
$CODE = pub_code GET "/s/$SHARE1_CODE"
if ($CODE -eq "410") { pass "禁用分享访问 → 410" } else { fail "禁用分享访问 → 410" "410" "$CODE" }
api_json PATCH "/api/shares/$SHARE1_ID" @{ is_active = $true } | Out-Null
$CODE = pub_code GET "/s/$SHARE1_CODE"
if ($CODE -eq "200") { pass "重新启用后恢复 → 200" } else { fail "重新启用后恢复 → 200" "200" "$CODE" }

# 有效期分享 — 等 6 秒后过期
Write-Host "  等待 6 秒验证过期..."
Start-Sleep -Seconds 6
$CODE = curl.exe -s -o nul -w "%{http_code}" -L "$SITE_URL/s/$SHARE4_CODE/download" 2>$null
if ($CODE -eq "410") { pass "过期分享下载 → 410" } else { fail "过期分享下载 → 410" "410" "$CODE" }

# ========================================================
# 7. 删除
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[7/8] 删除操作$([char]0x1b)[0m"

$RESP = api_json DELETE "/api/shares/$SHARE5_ID"
if ($RESP -match '"message"') { pass "删除分享 → 200" } else { fail "删除分享 → 200" "has message" "$RESP" }

$CODE = pub_code GET "/s/$SHARE5_CODE"
if ($CODE -eq "404") { pass "删除后分享不可访问" } else { fail "删除后分享不可访问" "404" "$CODE" }

$RESP = api_json DELETE "/api/files/$FILE2_ID"
if ($RESP -match '"message"') { pass "删除文件 → 200" } else { fail "删除文件 → 200" "has message" "$RESP" }

$COUNT = (api_json GET "/api/files" | ConvertFrom-Json).Count
if ($COUNT -eq 1) { pass "文件列表剩余 1 个" } else { fail "文件列表剩余 1 个" "1" "$COUNT" }

# ========================================================
# 8. 速率限制 & 其他
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m[8/8] 速率限制与边界$([char]0x1b)[0m"

# 速率限制 (30 req/min per IP for /s/*)
Write-Host "  发送 35 次请求测试速率限制..."
$HIT = $false
for ($i = 1; $i -le 35; $i++) {
    $CODE = curl.exe -s -o nul -w "%{http_code}" "$SITE_URL/s/$SHARE1_CODE" 2>$null
    if ($CODE -eq "429") { $HIT = $true; break }
}
if ($HIT) { pass "速率限制触发 → 429" } else { fail "速率限制触发 → 429" "429 hit" "never hit" }

# API 路由不受速率限制
$CODE = api_code GET "/api/files"
if ($CODE -eq "200") { pass "API 不受速率限制影响" } else { fail "API 不受速率限制影响" "200" "$CODE" }

# 不存在路由
$CODE = api_code GET "/api/nonexistent"
if ($CODE -eq "404") { pass "不存在路由 → 404" } else { fail "不存在路由 → 404" "404" "$CODE" }

# 根路径
$CODE = curl.exe -s -o nul -w "%{http_code}" "$SITE_URL/" 2>$null
if ($CODE -eq "404") { pass "根路径 → 404" } else { fail "根路径 → 404" "404" "$CODE" }

# ========================================================
# 结果
# ========================================================
Write-Host ""
Write-Host "$([char]0x1b)[0;33m========================================$([char]0x1b)[0m"
Write-Host "$([char]0x1b)[0;32m通过: $PASS$([char]0x1b)[0m"
Write-Host "$([char]0x1b)[0;31m失败: $FAIL$([char]0x1b)[0m"
Write-Host ""

cleanup

if ($FAIL -eq 0) {
    Write-Host "$([char]0x1b)[0;32m全部测试通过！$([char]0x1b)[0m"
    exit 0
} else {
    Write-Host "$([char]0x1b)[0;31m存在 $FAIL 个失败用例$([char]0x1b)[0m"
    exit 1
}
