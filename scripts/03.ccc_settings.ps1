# ccc_settings_local.ps1
# 数据源 = ccc.exe (Go 单 exe,kdbx 直读 + 内存合成 settings JSON)
#   - profile 列表: ccc list        → 解析 [kdbx] <name> 行
#   - profile 设置: ccc export <name> → 拿到合成 settings JSON 字符串
#   - 凭据缺失: ccc list stderr 含 "cred store" 时自动拉起 `ccc cred store` 后重试
# 切换行为: 合成 settings JSON + current_Model_Provider 覆盖 → merge-write ~/.claude/settings.json
# 旧的 ~/.claude/settings.json merge 行为不变(同名键递归覆盖)
# 菜单尾注:Get-CccProfileMeta(ccc list --tsv)拿 price/context, 在名字后显示 【context】【price】
#   (排版与 ccc_switch.ps1 同源, 公共实现见 ccc_common.ps1)
#
# 原 cc_switcher_kdbx.ps1 的旧数据链(虚拟环境引导 / kdbx 读 / 合成器)已删。
# 整个脚本纯 PowerShell + ccc.exe,无外部脚本语言链依赖。
#
# 用法:  pwsh -NoProfile -ExecutionPolicy Bypass -File ccc_settings_local.ps1
[CmdletBinding()]
param(
    [string]$CccExe     = '',   # 默认: <ScriptDir>\..\dst\ccc.exe
    [string]$SettingsPath = (Join-Path $env:USERPROFILE '.claude/settings.json')
)

$ErrorActionPreference = "Stop"
# 双向 UTF-8: 输出给原生进程的管道 + 读取原生进程的 stdout
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# 公共工具: ccc.exe 解析 / .env 读写 / FAIL/STDERR 包装 / settings json 合并等。
# 三个 PS1 脚本同名副本已统一搬到这里, 见 ccc_common.ps1。
. (Join-Path $PSScriptRoot 'ccc_common.ps1')

# ---------- ccc export <name> -> settings JSON 字符串 ----------
# 失败(非零 exit)→ 抛错由调用方处理。
# & 捕获的 stdout 是字符串数组;Out-String 拼成单串,后续 ConvertFrom-Json 直接吃。
function Get-CccSettings {
    param([Parameter(Mandatory)][string]$Profile)
    $exe = Get-CccExe -CccExe $CccExe -Require
    $stdout = & $exe export $Profile | Out-String
    if ($LASTEXITCODE -ne 0) {
        throw "ccc export $Profile failed (exit=$LASTEXITCODE)"
    }
    return $stdout
}

# ---------- settings.json 当前 provider ----------
function Get-CurrentProvider {
    if (Test-Path -LiteralPath $SettingsPath) {
        try {
            $j = Get-Content -LiteralPath $SettingsPath -Raw | ConvertFrom-Json
            return $j.current_Model_Provider
        } catch { return $null }
    }
    return $null
}

# ---------- 启动回填: 无 [current] 时从 .env CURRENT_MODEL 还原 settings.json ----------
# 回填写出 settings.json 即处于明文态, 走完整切换链 (含右键菜单互斥同步为明文),
# 否则明文 settings 与加密菜单并存, 互斥被破坏。
# profile 不在 ccc list 中只告警不中断启动。
function Restore-CurrentFromEnv {
    param([Parameter(Mandatory)]$Profiles)
    $envFile = Get-EnvFilePath -CccExe (Get-CccExe -CccExe $CccExe -Require)
    if (-not (Test-Path -LiteralPath $envFile)) { return }
    $model = Get-CurrentModel -Path $envFile
    if (-not $model) { return }
    if ($Profiles -notcontains $model) {
        Write-Host "[warn] .env CURRENT_MODEL='$model' 不在 ccc list profile 中, 跳过回填" -ForegroundColor Yellow
        return
    }
    Write-Host "[info] settings.json 无 [current], 从 .env CURRENT_MODEL 回填: $model" -ForegroundColor Yellow
    Switch-Configuration -Selected $model
}

# ---------- 互斥: 有效切换后同步右键菜单为明文模式 ----------
# 本脚本 = 明文路径(写 ~/.claude/settings.json),有效切换后必须保证右键菜单走 claude。
# 切明文含 settings.json 还原;模型解析失败 (exit 3) 仅 warn,本脚本是交互菜单,
# 用户接着选 profile 会再触发 Sync-RightMenuPlain。
# 唯一入口: ccc setMode SHOWSETTINGS (src/mode.go runSetModeShowSettings)。
function Sync-RightMenuPlain {
    param([string]$Profile = '')
    try {
        $exe = Get-CccExe -CccExe $CccExe -Require
        # 显式传 profile 时直达状态机,跳过 .env/kdbx 探测,
        # 避免用户选的 profile 被 .env 旧值覆盖回去。
        if ($Profile) {
            $out = & $exe setMode SHOWSETTINGS $Profile 2>&1
        } else {
            $out = & $exe setMode SHOWSETTINGS 2>&1
        }
        $rc = $LASTEXITCODE
        # 启动时已前置闸门检测, 正常路径不会走到这里; 保留兜底防运行期间闸门被打开。
        if ($rc -eq 3) {
            Write-Host '[warn] ccc setMode SHOWSETTINGS: 当前模型不可解析 (无 .env CURRENT_MODEL 且 .cred/kdbx 均无 profile)。请选择 profile 后重试。' -ForegroundColor Yellow
            return
        }
        if ($rc -eq 4) {
            # 公共机闸门拒绝 (Phase 2): 仅挡 SHOWSETTINGS. 不当作通用失败刷屏,
            # 明确告诉用户去找 ccc_manager.ps1 菜单 5 关闸门。
            Write-Host '[warn] 公共机模式禁止切明文' -ForegroundColor Yellow
            return
        }
        if ($rc -ne 0) {
            Write-Host "[warn] ccc setMode SHOWSETTINGS exit=$rc : $out" -ForegroundColor Yellow
            return
        }
        if ($out) { Write-Host "       $out" -ForegroundColor DarkGray }
        Write-Host '[ok] 右键菜单 = 明文 (claude) + settings.json 已从 profile 还原' -ForegroundColor Green
    } catch {
        Write-Host "[warn] 同步右键菜单为明文失败: $_" -ForegroundColor Yellow
    }
}

# ---------- 菜单 ----------
# 排版(分隔线 / 头部信息块 / 尾注【context】【price】按列对齐 / [current] / 0. exit)
# 走 ccc_common.ps1 的共用实现, 与 ccc_switch.ps1 观感一致。
function Show-Menu {
    param($Entries, [string]$Current, [hashtable]$Meta)

    Write-MenuHeader -Title 'Claude Code Settings Switcher (ccc.exe)' -Info ([ordered]@{
        ccc    = (Get-CccExe -CccExe $CccExe -Require)
        target = $SettingsPath
        effect = '明文路径: 写 settings.json + 右键菜单走 claude'
        tail   = '【context】【price $/M】(仅 openrouter 系有元数据时显示)'
    })

    Write-MenuRows -Entries $Entries -Current $Current -Meta $Meta
    Write-MenuFooter
    return (Read-MenuChoice -Entries $Entries)
}

# ---------- 切换 ----------
function Switch-Configuration {
    param($Selected)

    if ([string]::IsNullOrEmpty($Selected)) { return }

    try {
        # 拿 settings JSON: 走 ccc export,不再走旧的 compose 脚本
        $sourceJson = Get-CccSettings -Profile $Selected | ConvertFrom-Json

        # ccc 合成的 JSON 不含 current_Model_Provider(ps1 旧菜单用此字段标记当前选择),
        # 由 ps1 在写盘前盖一层,让 Get-CurrentProvider 的 [current] 标记继续工作。
        if ($sourceJson.PSObject.Properties.Name -contains 'current_Model_Provider') {
            $sourceJson.current_Model_Provider = $Selected
        } else {
            $sourceJson | Add-Member -NotePropertyName 'current_Model_Provider' -NotePropertyValue $Selected
        }

        $SettingsDir = Split-Path -Parent $SettingsPath
        if ($SettingsDir -and -not (Test-Path -LiteralPath $SettingsDir)) {
            New-Item -Path $SettingsDir -ItemType Directory -Force | Out-Null
        }

        # 已有 settings.json 完整保留, source 同名键递归覆盖; 其余键原样保留。
        # 写盘走 common 的 Write-SettingsJson -Merge 版 (settings_local 旧行为本就 merge,
        # 与 manager 的 -Merge 缺省(=覆盖)刻意不同,见整改建议 §3.2).
        Write-SettingsJson -SourceJson $sourceJson -Path $SettingsPath -Merge

        Write-Host ""
        Write-Host "Switched to: $Selected" -ForegroundColor Green
        Write-Host "settings.json merged with existing keys preserved" -ForegroundColor Green

        # 互斥: 有效切换 → 右键菜单同步为明文模式 (claude)
        # 显式把 $Selected 透传给状态机,绕过 .env 旧值覆盖。
        Sync-RightMenuPlain -Profile $Selected
    }
    catch {
        Write-Host ""
        Write-Host "Failed to switch: $_" -ForegroundColor Red
    }
}

# ---------- 启动闸门: 公共机禁止本脚本 (明文路径), 直接拦截不进菜单 ----------
# 本脚本 = 明文切换 (写 ~/.claude/settings.json), 公共机闸门 TRUE 时整个脚本无意义,
# 启动即拦截比按键切换后再被 setMode 拒绝 (exit 4) 更早更清晰。
# 查询口与 ccc_manager.ps1 对齐: `ccc publicMachine status`, stdout TRUE/FALSE。
function Assert-NotPublicMachine {
    param([Parameter(Mandatory)][string]$Exe)
    # 不走 Invoke-Ccc: 它内部 Get-CccExe 不带 -CccExe, 会忽略本脚本的参数覆盖;
    # 与本脚本其他调用 (Sync-RightMenuPlain) 一致, 直接 & $exe。
    $out = & $Exe publicMachine status 2>$null
    $rc = $LASTEXITCODE
    if ($rc -ne 0) {
        Write-Host "[warn] ccc publicMachine status 查询失败 (exit=$rc), 跳过闸门检测" -ForegroundColor Yellow
        return
    }
    if ("$out".Trim() -eq 'TRUE') {
        Write-Host "[fail] 公共机模式已开启, 禁止明文切换 (本脚本不适用)。" -ForegroundColor Red
        Write-Host "       关闭闸门: ccc_manager.ps1 菜单 4" -ForegroundColor Red
        Fail-And-Exit 1
    }
}

# ============== main ==============
# dot-source 时只加载函数, 供测试 harness 复用
if ($MyInvocation.InvocationName -eq '.') { return }

Assert-NotPublicMachine -Exe (Get-CccExe -CccExe $CccExe -Require)

Write-Host "Loading profiles from ccc list ..." -ForegroundColor Green

try {
    $Profiles = Get-CccProfiles -CccExe $CccExe
    if (-not $Profiles -or $Profiles.Count -eq 0) {
        throw "no profiles from ccc list (check .cred providers snapshot or kdbx group / path)"
    }

    $Current = Get-CurrentProvider
    if (-not $Current) {
        # 无 [current] → 从 .env CURRENT_MODEL 回填 settings.json,
        # 经 Switch-Configuration → Sync-RightMenuPlain 同步右键菜单为明文
        # (commit a707e9b 起;不显式走菜单而绕过会破坏明文 settings 与
        # 加密菜单的互斥契约,见整改建议 §0 / §3.3)。
        Restore-CurrentFromEnv -Profiles $Profiles
        $Current = Get-CurrentProvider
    }

    $Meta = Get-CccProfileMeta -CccExe $CccExe   # name->@{Model;Price;Context}; 失败则空表(降级不显示)

    while ($true) {
        try { Clear-Host } catch { }   # 重定向/管道下无控制台, 忽略
        $sel = Show-Menu -Entries $Profiles -Current $Current -Meta $Meta
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