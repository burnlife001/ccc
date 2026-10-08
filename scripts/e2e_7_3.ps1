#!/usr/bin/env pwsh
# e2e_7_3.ps1 — 验收 7.3:连续执行 6.1 五十次,无句柄/进程泄漏,全部 exit code 0
# 用法:pwsh -File scripts/e2e_7_3.ps1
#
# 7.3 验收定义(docs/ccs验收标准.md):
#   - 6.1 = `ccc minimax_m3 -p "回复OK两个字"` 输出 OK, exit 0
#   - 7.3 = 上述命令连续 50 次,无句柄/进程泄漏,全 exit 0
#
# 收集:
#   - 每轮 exit code
#   - 进程句柄数(ccc.exe + claude.exe)
#   - 父进程 (PowerShell / 当前 shell) 句柄基线
#   - claude.exe 残留数(必须为 0)

$ErrorActionPreference = "Stop"
$CCC = "D:/Programs/ccc/ccc.exe"
$PROFILE = "minimax_m3"
$PROMPT = "回复OK两个字"
$RUNS = 50

# 输出文件
$LOGDIR = Join-Path (Split-Path $PSScriptRoot -Parent) "tmp"
if (-not (Test-Path $LOGDIR)) { New-Item -ItemType Directory -Path $LOGDIR | Out-Null }
$LOGFILE = Join-Path $LOGDIR "e2e_7_3.log"
$RESFILE = Join-Path $LOGDIR "e2e_7_3_results.json"
"" | Set-Content -Path $LOGFILE -Encoding UTF8

function Log($msg) {
    $ts = (Get-Date).ToString("yyyy-MM-ddTHH:mm:ss.fffZ")
    $line = "[$ts] $msg"
    Write-Host $line
    Add-Content -Path $LOGFILE -Value $line -Encoding UTF8
}

function Count-Handles($procName) {
    $sum = 0
    Get-Process -Name $procName -ErrorAction SilentlyContinue | ForEach-Object {
        try { $sum += $_.HandleCount } catch {}
    }
    return $sum
}

function Count-Procs($procName) {
    return @(Get-Process -Name $procName -ErrorAction SilentlyContinue).Count
}

Log "=== E2E 7.3 START ==="
Log "CCC=$CCC  PROFILE=$PROFILE  PROMPT=$PROMPT  RUNS=$RUNS"
Log "Initial: ccc handles=$(Count-Handles 'ccc'), claude handles=$(Count-Handles 'claude'), ccc procs=$(Count-Procs 'ccc'), claude procs=$(Count-Procs 'claude')"

$results = New-Object System.Collections.Generic.List[object]
$baseCcc = Count-Handles 'ccc'
$baseClaude = Count-Handles 'claude'
Log "Baseline handles: ccc=$baseCcc  claude=$baseClaude"

$swTotal = [System.Diagnostics.Stopwatch]::StartNew()

for ($i = 1; $i -le $RUNS; $i++) {
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $out = & $CCC $PROFILE -p $PROMPT 2>&1
    $ec = $LASTEXITCODE
    $sw.Stop()

    $cccH = Count-Handles 'ccc'
    $claudeH = Count-Handles 'claude'
    $cccN = Count-Procs 'ccc'
    $claudeN = Count-Procs 'claude'

    # 截断输出避免日志爆炸
    $outTrim = ($out | Out-String).Trim()
    if ($outTrim.Length -gt 200) { $outTrim = $outTrim.Substring(0, 200) + "..." }

    $obj = [PSCustomObject]@{
        i = $i
        ec = $ec
        ms = $sw.ElapsedMilliseconds
        ccc_handles = $cccH
        claude_handles = $claudeH
        ccc_procs = $cccN
        claude_procs = $claudeN
        out_tail = $outTrim
    }
    $results.Add($obj)

    # 控制台紧凑输出
    $ecMark = if ($ec -eq 0) { "✓" } else { "✗$ec" }
    Log "[$i/$RUNS] ${ecMark}  ${ms}ms  ccc=${cccH}h/${cccN}p  claude=${claudeH}h/${claudeN}p"

    # 进程泄漏检查:每轮结束后,ccc 应该已经退出(短进程),claude 应该已经被 shell wait
    # 如果出现连续 ≥2 轮有 ccc/claude 残留进程,需要警告
    if ($claudeN -gt 0 -and $i -lt $RUNS) {
        Log "  WARN: claude.exe 残留进程数=$claudeN(本应已 wait)"
    }
}

$swTotal.Stop()

# 终态检查
Start-Sleep -Seconds 2  # 等所有子进程自然退出
$finalCcc = Count-Handles 'ccc'
$finalClaude = Count-Handles 'claude'
$finalCccN = Count-Procs 'ccc'
$finalClaudeN = Count-Procs 'claude'

$failed = @($results | Where-Object { $_.ec -ne 0 })
$ok = @($results | Where-Object { $_.ec -eq 0 })

Log ""
Log "=== E2E 7.3 END ==="
Log "Total time: $($swTotal.Elapsed) ($([int]$swTotal.Elapsed.TotalSeconds)s)"
Log "Successful runs: $($ok.Count) / $RUNS"
Log "Failed runs: $($failed.Count)"
if ($failed.Count -gt 0) {
    Log "Failure indices: $(($failed | ForEach-Object { $_.i }) -join ',')"
    $failed | ForEach-Object { Log "  run #$($_.i): ec=$($_.ec) tail=$($_.out_tail)" }
}
Log "Final handle counts (after 2s drain): ccc=${finalCcc}h/${finalCccN}p  claude=${finalClaude}h/${finalClaudeN}p"
Log "Final delta vs baseline: ccc=$($finalCcc - $baseCcc)  claude=$($finalClaude - $baseClaude)"

# 写 JSON 结果供后续解析
$results | ConvertTo-Json -Depth 3 | Set-Content -Path $RESFILE -Encoding UTF8
Log "Results: $RESFILE"
Log "Log: $LOGFILE"

# 退出码:全 0 = 0; 有失败 = 1
if ($failed.Count -gt 0) { exit 1 }
exit 0