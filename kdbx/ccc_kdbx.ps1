# cc_switcher_kdbx.ps1
# 数据源 = .kdbx:  cc 组 = 7 条供应商记录, 菜单 = profiles 扁平展开 12 行
# 合成:  src/compose_settings.py  (骨架 + base_url + password token + profile 覆盖 + extra_env)
# 读取:  src/kdbx_read_entries.py  (主密码由 src/kdbx_cred.py 从 DPAPI 凭据文件取, 不经 PowerShell)
#   -> 凭据文件 = 同目录 .kdbx_master.cred (CryptProtectData, 当前用户范围; 不用 Credential Manager,
#      该机 generic 凭据会丢; 不经 PS 管道, 5.1 管道会给 stdin 加 BOM/折行)
#   -> 凭据缺失 (exit=3) 时自动拉起 kdbx_cred.py store (getpass 输入 + 试开 kdbx 校验后才落盘)
#
# UX 沿用 cc_switcher.ps1:  Clear-Host -> Menu -> Switch -> Pause -> Loop
# 切换行为: 合成结果是完整 settings JSON -> 直接覆盖 ~/.claude/settings.json (不再 merge)
#
# 用法:  pwsh -NoProfile -ExecutionPolicy Bypass -File cc_switcher_kdbx.ps1
[CmdletBinding()]
param(
    [string]$KdbxPath   = '',
    [string]$KdbxGroup  = 'cc',
    [string]$SettingsPath = (Join-Path $env:USERPROFILE '.claude/settings.json')
)

$ErrorActionPreference = "Stop"
# 双向 UTF-8: 输出给原生进程的管道 + 读取原生进程的 stdout (5.1 默认 ASCII/GBK 都会坏事)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }

# 5.1 坑: [CmdletBinding()] 下 param() 默认值里 $PSScriptRoot 为空 -> 默认值放 body 里解析
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $KdbxPath) { $KdbxPath = Join-Path $ScriptDir 'apikey.kdbx' }

# ---------- 失败: 打印错误 -> 暂停 -> 退出 (避免双击运行时窗口闪退看不见错误) ----------
function Fail-And-Exit {
    param([int]$Code)
    Write-Host ""
    Write-Host "=============================================" -ForegroundColor Red
    Write-Host " FAILED (exit=$Code). Press Enter to close..." -ForegroundColor Red
    Write-Host "=============================================" -ForegroundColor Red
    if ([Environment]::UserInteractive) {
        try { Pause } catch { }
    }
    exit $Code
}

# ---------- .venv 引导: 缺失则 uv 创建并装 pykeepass ----------
function Ensure-Venv {
    $VenvPy = Join-Path $ScriptDir ".venv\Scripts\python.exe"
    if (Test-Path $VenvPy) { return }
    Write-Host ""
    Write-Host ".venv not found, bootstrapping with uv ..." -ForegroundColor Yellow
    Push-Location $ScriptDir
    try {
        & uv venv .venv
        if ($LASTEXITCODE -ne 0) { throw "uv venv failed (exit=$LASTEXITCODE)" }
        & uv pip install --python .venv pykeepass
        if ($LASTEXITCODE -ne 0) { throw "uv pip install failed (exit=$LASTEXITCODE)" }
    } finally {
        Pop-Location
    }
    Write-Host ".venv ready." -ForegroundColor Green
}

# ---------- kdbx -> 供应商记录 TSV ----------
# 输出 TSV: title \t base64(password) \t base64(notes)
function Get-KdbxEntries {
    $VenvPy = Join-Path $ScriptDir ".venv\Scripts\python.exe"
    $Reader = Join-Path $ScriptDir "src\kdbx_read_entries.py"
    $CredPy = Join-Path $ScriptDir "src\kdbx_cred.py"

    & $VenvPy -X utf8 $Reader --kdbx $KdbxPath --group $KdbxGroup
    $rc = $LASTEXITCODE

    if ($rc -eq 3) {
        # 凭据缺失 -> 交互式 store (python getpass, 试开 kdbx 校验通过才落盘) -> 重试一次
        Write-Host ""
        Write-Host "=============================================" -ForegroundColor Cyan
        Write-Host " creating credential file" -ForegroundColor Green
        Write-Host " enter password to save." -ForegroundColor Green
        Write-Host "=============================================" -ForegroundColor Cyan
        Write-Host ""
        & $VenvPy -X utf8 $CredPy store --kdbx $KdbxPath
        $setupExit = $LASTEXITCODE
        if ($setupExit -ne 0) {
            Write-Host ""
            Write-Host "Credential setup failed (exit=$setupExit). Aborting." -ForegroundColor Red
            Fail-And-Exit $setupExit
        }
        Write-Host ""
        Write-Host "Setup OK. Retrying kdbx read..." -ForegroundColor Yellow
        & $VenvPy -X utf8 $Reader --kdbx $KdbxPath --group $KdbxGroup
        $rc = $LASTEXITCODE
    }

    if ($rc -ne 0) {
        Write-Host "python exit=$rc" -ForegroundColor Red
        Fail-And-Exit $rc
    }
}

# ---------- 合成: 供应商记录 -> 扁平 profile settings ----------
# 输入 TSV 行 (title\tb64pwd\tb64notes) -> 输出 TSV (profile\tb64json | !SKIP)
# 不走 PS 管道: 5.1 管道给原生 stdin 会按控制台宽度折行(长行被截成无 tab 的碎行)且加 BOM
function Invoke-Compose {
    param([string[]]$Rows)
    $VenvPy = Join-Path $ScriptDir ".venv\Scripts\python.exe"
    $Compose = Join-Path $ScriptDir "src\compose_settings.py"
    $tmpIn  = Join-Path $env:TEMP ("ccswitch_in_{0}.tsv"  -f [guid]::NewGuid().ToString('N'))
    $tmpOut = Join-Path $env:TEMP ("ccswitch_out_{0}.tsv" -f [guid]::NewGuid().ToString('N'))
    try {
        [System.IO.File]::WriteAllLines($tmpIn, [string[]]$Rows, [System.Text.UTF8Encoding]::new($false))
        $p = Start-Process -FilePath $VenvPy `
                           -ArgumentList @('-X','utf8',$Compose,'list') `
                           -RedirectStandardInput  $tmpIn `
                           -RedirectStandardOutput $tmpOut `
                           -NoNewWindow -Wait -PassThru
        if ($p.ExitCode -ne 0) {
            Write-Host "compose exit=$($p.ExitCode)" -ForegroundColor Red
            Fail-And-Exit $p.ExitCode
        }
        Get-Content $tmpOut -Encoding UTF8
    } finally {
        Remove-Item $tmpIn, $tmpOut -Force -ErrorAction SilentlyContinue
    }
}

# ---------- settings.json 当前 provider ----------
function Get-CurrentProvider {
    if (Test-Path $SettingsPath) {
        try {
            $j = Get-Content $SettingsPath -Raw | ConvertFrom-Json
            return $j.current_Model_Provider
        } catch { return $null }
    }
    return $null
}

# ---------- 递归深合并 JSON ----------
# 先复制 target 全部键值, 再用 source 强制覆盖同名键(同层递归合并对象)
function Merge-JsonObject {
    param([PSCustomObject]$Source, [PSCustomObject]$Target)

    $result = @{}

    foreach ($prop in $Target.PSObject.Properties) {
        $result[$prop.Name] = $prop.Value
    }

    foreach ($prop in $Source.PSObject.Properties) {
        $srcVal = $prop.Value
        $tgtVal = $result[$prop.Name]

        if ($srcVal -is [PSCustomObject] -and $tgtVal -is [PSCustomObject]) {
            $result[$prop.Name] = Merge-JsonObject -Source $srcVal -Target $tgtVal
        } else {
            $result[$prop.Name] = $srcVal
        }
    }

    return [PSCustomObject]$result
}

# ---------- 菜单 ----------
function Show-Menu {
    param($Entries, [string]$Current)

    Write-Host ""
    Write-Host "Claude Code Settings Switcher (kdbx source)" -ForegroundColor Cyan
    Write-Host "==========================================" -ForegroundColor Cyan
    Write-Host "  kdbx : $KdbxPath" -ForegroundColor DarkGray
    Write-Host "  group: $KdbxGroup  ($($Entries.Count) profiles)" -ForegroundColor DarkGray
    Write-Host "==========================================" -ForegroundColor Cyan

    for ($i = 0; $i -lt $Entries.Count; $i++) {
        $n   = $i + 1
        $e   = $Entries[$i]
        # "  N. " + title 占满到第29列(0-indexed), [ 落在第30列(1-indexed)
        $prefix = "  {0}. " -f $n
        $line   = ($prefix + $e.title).PadRight(29)
        Write-Host $line -NoNewline
        if ($e.title -eq $Current) {
            Write-Host "[current]" -ForegroundColor Green -NoNewline
        }
        if (-not $e.IsJson) {
            Write-Host "  (non-JSON, will skip)" -ForegroundColor Yellow -NoNewline
        }
        Write-Host ""
    }
    Write-Host "==========================================" -ForegroundColor Cyan
    Write-Host "  0. Exit"
    Write-Host ""
    $choice = Read-Host "Select profile (1-$($Entries.Count)) or 0 to exit"
    if ($choice -eq "0") { return $null }
    if ($choice -match "^\d+$") {
        $idx = [int]$choice
        if ($idx -ge 1 -and $idx -le $Entries.Count) { return $Entries[$idx - 1] }
    }
    Write-Host "Invalid choice" -ForegroundColor Red
    return $null
}

# ---------- 切换 ----------
function Switch-Configuration {
    param($Selected)

    if ($null -eq $Selected) { return }

    if (-not $Selected.IsJson) {
        Write-Host ""
        Write-Host "SKIP: entry '$($Selected.title)' notes is not a provider record, cannot switch." -ForegroundColor Yellow
        return
    }

    try {
        # 合法性二次校验
        $sourceJson = $Selected.Notes | ConvertFrom-Json

        $SettingsDir = Split-Path -Parent $SettingsPath
        if (-not (Test-Path $SettingsDir)) {
            New-Item -Path $SettingsDir -ItemType Directory -Force | Out-Null
        }

        # 已有 settings.json 完整保留, source 同名键递归覆盖; 其余键原样保留
        $targetJson = $null
        if (Test-Path $SettingsPath) {
            try {
                $targetJson = Get-Content $SettingsPath -Raw | ConvertFrom-Json
            } catch { $targetJson = $null }
        }
        $mergedJson = if ($null -ne $targetJson) {
            Merge-JsonObject -Source $sourceJson -Target $targetJson
        } else {
            $sourceJson
        }
        $mergedJson | ConvertTo-Json -Depth 10 | Set-Content -Path $SettingsPath -Encoding UTF8 -NoNewline

        Write-Host ""
        Write-Host "Switched to: $($Selected.title)" -ForegroundColor Green
        Write-Host "settings.json merged with existing keys preserved" -ForegroundColor Green
    }
    catch {
        Write-Host ""
        Write-Host "Failed to switch: $_" -ForegroundColor Red
    }
}

# ============== main ==============
# dot-source 时只加载函数, 供测试 harness 复用
if ($MyInvocation.InvocationName -eq '.') { return }

Write-Host "Loading kdbx entries (master password from DPAPI cred file) ..." -ForegroundColor Green

try {
    # 0) .venv 引导
    Ensure-Venv

    # 1) kdbx -> 供应商记录 TSV (凭据缺失会自动拉起交互式 store)
    $raw = Get-KdbxEntries
    if ($null -eq $raw) { throw "kdbx returned no data" }

    # 2) 合成: 扁平展开成 12 个 profile
    $composed = Invoke-Compose -Rows @($raw)
    if ($null -eq $composed) { throw "compose returned no data" }

    # 3) 解析 TSV: profile\tbase64(settings json)
    $Entries = New-Object System.Collections.ArrayList
    foreach ($line in ($composed -split "`n")) {
        if ($line.Trim() -eq "===END===") { break }
        if (-not $line.Contains("`t")) { continue }
        $parts = $line -split "`t", 2
        $title = $parts[0]
        if ($parts[1].Trim() -eq '!SKIP') {
            [void]$Entries.Add([PSCustomObject]@{ Title=$title; Notes=''; IsJson=$false })
            continue
        }
        $json = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($parts[1].Trim()))
        $isJson = $false
        try { $null = $json | ConvertFrom-Json; $isJson = $true } catch { $isJson = $false }
        [void]$Entries.Add([PSCustomObject]@{ Title=$title; Notes=$json; IsJson=$isJson })
    }

    if ($Entries.Count -eq 0) { throw "no profiles composed from group '$KdbxGroup'" }

    $Current = Get-CurrentProvider

    while ($true) {
        try { Clear-Host } catch { }   # 重定向/管道下无控制台, 忽略(终止性错误不吃 -EA)
        $sel = Show-Menu -Entries $Entries -Current $Current
        if ($null -eq $sel) { break }
        Switch-Configuration $sel
        $Current = Get-CurrentProvider   # 刷新 [current] 标记
        Pause
    }
}
catch {
    Write-Host ""
    Write-Host "FATAL: $_" -ForegroundColor Red
    Fail-And-Exit 1
}
