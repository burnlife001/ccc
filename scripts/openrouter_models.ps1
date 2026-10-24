# openrouter_models.ps1
# OpenRouter 低价模型 → kdbx 菜单:
#   1. 拉取模型清单(按输入价 in 升序,只保留上下文 ≥256K 的模型)→ <脚本目录>\openrouter-models.md
#   2. 按 index 把模型并入 kdbx(cc/openrouter):
#        - model id 未命中(去 '~' 前缀归一化)→ 追加新条目
#          (model 字段写去掉 '~' 的裸 id,如 z-ai/glm-flash-latest)
#        - 已命中但 price 过期 → 就地刷新 price 字段(只改 price, name 等不动)
#        - 已命中且库里 model 带 '~' 前缀 → 顺手归一化成裸 id
#        - 无任何改动 → up-to-date, 不写
#        - 全部处理完后整条 profiles 按 输入价 $/M 升序重排(无价者殿后)
#   3. 更新 kdbx(cc/openrouter)里已有模型的 price / context(按 OpenRouter 实时目录);
#      目录价格与库里一致 → 不写盘(不备份、不重排)
#   4. 删除 kdbx(cc/openrouter)里的某个模型(子菜单列出现有模型 → 选一条 → 直接删,
#      不再二次确认;删完列出剩余模型)
#
# 数据源与写盘全部走 Go 侧(格式化/解析单测在 Go,ps1 只转发 + 显示):
#   - 菜单 1 → ccc models list --out <md>
#   - 菜单 2 → ccc kdbx add-models --md <md> --index "<串>"
#   - 菜单 3 → ccc kdbx price
#   - 菜单 4 → ccc kdbx list-models(列模型) + ccc kdbx rm-model --name <profile>(删)
#
# 用法:  pwsh -NoProfile -ExecutionPolicy Bypass -File openrouter_models.ps1
[CmdletBinding()]
param(
    [string]$CccExe = '',   # 默认: <ScriptDir>\..\dst\ccc.exe (不存在走 PATH)
    [string]$MdPath = ''    # 清单路径;默认 <ScriptDir>\openrouter-models.md
)

$ErrorActionPreference = "Stop"
# 双向 UTF-8: 输出给原生进程的管道 + 读取原生进程的 stdout
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# 公共工具: ccc.exe 定位 / 错误退出。不复制粘贴 exe 定位逻辑。
. (Join-Path $PSScriptRoot 'ccc_common.ps1')

# ---------- 路径 ----------
function Get-MdFilePath {
    if ($MdPath -ne '') { return $MdPath }
    return (Join-Path $ScriptDir 'openrouter-models.md')
}

# ---------- 菜单 1:拉取清单 ----------
function Invoke-PullModels {
    $exe = Get-CccExe -CccExe $CccExe -Require
    $mdPath = Get-MdFilePath
    Write-Host "正在拉取 OpenRouter 模型清单 (按输入价 in 升序, 上下文 >=256K) ..." -ForegroundColor Cyan
    Write-Host ""

    $res = Invoke-Ccc -Command @('models', 'list', '--out', $mdPath) -RedirectStderr -Exe $exe
    if ($res.Rc -ne 0) {
        Write-Host "拉取失败 (exit=$($res.Rc)):" -ForegroundColor Red
        if ($res.ErrText) { Write-Host $res.ErrText -ForegroundColor Red }
        else { $res.Out | ForEach-Object { Write-Host "  $_" } }
        return
    }
    if ($res.Out) { $res.Out | ForEach-Object { Write-Host "  $_" } }
    Write-Host ""
    Write-Host "[ok] 清单已生成: $mdPath" -ForegroundColor Green
    Write-Host "     输入 2 可以按 index 把模型并入 kdbx" -ForegroundColor DarkGray
}

# ---------- 菜单 2:按 index 并入 kdbx(追加新模型 + 就地刷新过期 price + 按价升序重排) ----------
function Invoke-AddModels {
    $mdPath = Get-MdFilePath
    if (-not (Test-Path -LiteralPath $mdPath)) {
        Write-Host "清单不存在: $mdPath" -ForegroundColor Yellow
        Write-Host "请先执行菜单 1 拉取模型清单(不退出脚本)。" -ForegroundColor Yellow
        return
    }

    # 把清单前 10 行(编号 + 模型 id)打出来,方便对着输 index
    Write-Host ""
    Write-Host "清单前 10 个模型:" -ForegroundColor Cyan
    $rows = Get-Content -LiteralPath $mdPath -Encoding UTF8 |
        Where-Object { $_ -match '^\|\s*\d+\s*\|' } |
        Select-Object -First 10
    foreach ($r in $rows) { Write-Host "  $r" }
    Write-Host ""

    $indices = Read-Host "输入要并入的模型 index (空格或逗号分隔, 例 3,7 12);已存在且价格过期会就地刷新并全量按价升序重排"
    if ([string]::IsNullOrWhiteSpace($indices)) {
        Write-Host "未输入 index, 返回菜单。" -ForegroundColor Yellow
        return
    }

    $exe = Get-CccExe -CccExe $CccExe -Require
    Write-Host ""
    Write-Host "正在把模型并入 kdbx (追加新模型 / 刷新过期 price / 按价升序重排) ..." -ForegroundColor Cyan
    Write-Host "使用 exe: $exe" -ForegroundColor DarkGray
    Write-Host ""

    # index 串解析在 Go 侧(parseIndexList,单测覆盖),ps1 只原样转发
    $res = Invoke-Ccc -Command @('kdbx', 'add-models', '--md', $mdPath, '--index', $indices) -RedirectStderr -Exe $exe
    if ($res.Out) { $res.Out | ForEach-Object { Write-Host "  $_" } }
    if ($res.Rc -ne 0) {
        Write-Host ""
        Write-Host "操作失败 (exit=$($res.Rc)):" -ForegroundColor Red
        if ($res.ErrText) { Write-Host "  $($res.ErrText)" -ForegroundColor Red }
        return
    }
    Write-Host ""
    Write-Host "[ok] 并入完成" -ForegroundColor Green
}

# ---------- 菜单 3:更新 kdbx 里已有模型的价格 ----------
function Invoke-UpdatePrices {
    # 先定位 ccc.exe:找不到就明确报错,而不是让 Invoke-Ccc 抛 CommandNotFoundException。
    $exe = Get-CccExe -CccExe $CccExe -Require

    Write-Host "正在按 OpenRouter 实时目录刷新 kdbx 里已有模型的 price / context ..." -ForegroundColor Cyan
    Write-Host "使用 exe: $exe" -ForegroundColor DarkGray
    Write-Host ""

    $res = Invoke-Ccc -Command @('kdbx', 'price') -RedirectStderr -Exe $exe
    if ($res.Out) { $res.Out | ForEach-Object { Write-Host "  $_" } }
    if ($res.Rc -ne 0) {
        Write-Host ""
        Write-Host "更新失败 (exit=$($res.Rc)):" -ForegroundColor Red
        if ($res.ErrText) { Write-Host "  $($res.ErrText)" -ForegroundColor Red }
        return
    }
    Write-Host ""
    # Go 侧判定:价格/上下文与目录一致 → 不写盘(不备份、不重排)。
    $unchanged = ($res.Out | Where-Object { "$_" -match 'not written' }).Count -gt 0
    if ($unchanged) {
        Write-Host "[ok] 价格无变化 —— 未写入 .kdbx(文件未动,也没生成备份)" -ForegroundColor Green
    } else {
        Write-Host "[ok] 价格已刷新(整条 profiles 按新价重排)" -ForegroundColor Green
    }
}

# ---------- 菜单 4:删除 kdbx 里的模型(子菜单选一条) ----------
# 现有模型行:每行 = @(name, model, price, context)。
# 用 ArrayList 装(脚本级变量),避免 PowerShell 数组返回时被摊平。
$script:OpenRouterModelRows = New-Object System.Collections.ArrayList

# 读 cc/openrouter 现有模型 -> $script:OpenRouterModelRows;失败返回 $false。
function Get-OpenRouterModelRows {
    param([string]$Exe = '')
    $res = Invoke-Ccc -Command @('kdbx', 'list-models') -RedirectStderr -Exe $Exe
    if ($res.Rc -ne 0) {
        Write-Host "读取模型列表失败 (exit=$($res.Rc)):" -ForegroundColor Red
        if ($res.ErrText) { Write-Host "  $($res.ErrText)" -ForegroundColor Red }
        return $false
    }

    $rows = New-Object System.Collections.ArrayList
    foreach ($line in $res.Out) {
        if ($null -eq $line) { continue }
        $t = "$line" -split "`t"
        if ($t.Count -lt 1 -or [string]::IsNullOrWhiteSpace($t[0])) { continue }
        $model = if ($t.Count -gt 1) { $t[1] } else { '' }
        $price = if ($t.Count -gt 2) { $t[2] } else { '' }
        $ctx   = if ($t.Count -gt 3) { $t[3] } else { '' }
        [void]$rows.Add(@($t[0].Trim(), $model, $price, $ctx))
    }
    $script:OpenRouterModelRows = $rows
    return $true
}

# 打印 $script:OpenRouterModelRows(编号 / 名称 / 模型 id / 价格 / 上下文)。
function Write-OpenRouterModelList {
    param([string]$Title = 'kdbx (cc/openrouter) 现有模型:')
    Write-Host ""
    Write-Host $Title -ForegroundColor Cyan
    for ($i = 0; $i -lt $script:OpenRouterModelRows.Count; $i++) {
        $r = $script:OpenRouterModelRows[$i]
        $price = if ($r[2]) { $r[2] } else { '-' }
        $ctx   = if ($r[3]) { $r[3] } else { '-' }
        Write-Host ("  {0,2}. {1,-26} {2,-34} 价格 {3,-14} 上下文 {4}" -f ($i + 1), $r[0], $r[1], $price, $ctx)
    }
    Write-Host ""
}

function Invoke-RemoveModel {
    $exe = Get-CccExe -CccExe $CccExe -Require
    Write-Host "使用 exe: $exe" -ForegroundColor DarkGray

    # 列出现有模型(TSV: name / model / price / context),编号后由用户选一条。
    if (-not (Get-OpenRouterModelRows -Exe $exe)) { return }
    $rows = $script:OpenRouterModelRows
    if ($rows.Count -eq 0) {
        Write-Host "kdbx (cc/openrouter) 里没有可删的模型。" -ForegroundColor Yellow
        return
    }

    Write-OpenRouterModelList

    $ans = (Read-Host "输入要删除的编号 (1-$($rows.Count)); 直接回车取消").Trim()
    if ($ans -eq '') {
        Write-Host "已取消。" -ForegroundColor DarkGray
        return
    }
    $idx = 0
    if (-not [int]::TryParse($ans, [ref]$idx) -or $idx -lt 1 -or $idx -gt $rows.Count) {
        Write-Host "编号不合法 (只接受 1-$($rows.Count))。" -ForegroundColor Yellow
        return
    }
    $target = $rows[$idx - 1]

    Write-Host ""
    Write-Host "正在删除:" -ForegroundColor Yellow
    Write-Host "  profile: $($target[0])" -ForegroundColor Yellow
    Write-Host "  model  : $($target[1])" -ForegroundColor Yellow

    # 删掉当前模型(.env CURRENT_MODEL)会留下悬空引用 —— 提示一句(不再二次确认)。
    $cur = Get-CurrentModel -Path (Get-EnvFilePath -CccExe $exe)
    if ($cur -and $cur -eq $target[0]) {
        Write-Host "  注意: 这正是 .env 里的 CURRENT_MODEL —— 删掉后 ccc last / 切模型会找不到它。" -ForegroundColor Red
        Write-Host "        (备份 apikey.kdbx.bak-<时间戳> 会留着,可手工改回 CURRENT_MODEL 或恢复文件)" -ForegroundColor DarkYellow
    }

    Write-Host ""
    $res = Invoke-Ccc -Command @('kdbx', 'rm-model', '--name', $target[0]) -RedirectStderr -Exe $exe
    if ($res.Out) { $res.Out | ForEach-Object { Write-Host "  $_" } }
    if ($res.Rc -ne 0) {
        Write-Host ""
        Write-Host "删除失败 (exit=$($res.Rc)):" -ForegroundColor Red
        if ($res.ErrText) { Write-Host "  $($res.ErrText)" -ForegroundColor Red }
        return
    }
    Write-Host ""
    Write-Host "[ok] 已删除 $($target[0])" -ForegroundColor Green

    # 删完把剩下的模型列出来(菜单 Pause 会停住, 便于看清)。
    if (Get-OpenRouterModelRows -Exe $exe) {
        Write-OpenRouterModelList -Title '删除后剩余模型:'
    }
}

# ============== main ==============
while ($true) {
    try { Clear-Host } catch { }
    Write-Host "======== ccc OpenRouter 模型工具 ========" -ForegroundColor Cyan
    Write-Host "  1. 拉取模型清单 → openrouter-models.md"
    Write-Host "  2. 添加模型到.kdbx (追加 + 刷新price)"
    Write-Host "  3. 更新.kdbx中的模型价格(无变化不写盘)"
    Write-Host "  4. 删除.kdbx中的模型"
    Write-Host "  0. 退出"
    Write-Host ""
    $choice = Read-Host "请选择 (0-4)"
    switch ($choice) {
        "1" { Invoke-PullModels; Pause }
        "2" { Invoke-AddModels; Pause }
        "3" { Invoke-UpdatePrices; Pause }
        "4" { Invoke-RemoveModel; Pause }
        "0" { exit 0 }
        default { }
    }
}
