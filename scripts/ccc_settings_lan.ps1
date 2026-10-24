# ccc_settings_lan.ps1
# 目的: 用**本地** ccc.exe 的 profile, 改**远端 192.168.1.123** 的 ~/.claude/settings.json
#   - profile 列表: 本地 `ccc list`     → 解析 [kdbx] <name> 行
#   - profile 设置: 本地 `ccc export`   → 合成 settings JSON 字符串
#   - 远端读写: ssh + base64 管道传明文 JSON, 远端不装 ccc / claude 侧任何东西
#   - 远端 target 目录不存在则自动创建
#
# 写盘语义(与 ccc_settings_local.ps1 的刻意差异):
#   env 块**整体替换**为 ccc export 的 env —— profile 的 env 即权威。
#   理由: 远端 settings.json 里可能残留上一个 provider 的凭据(如 ANTHROPIC_API_KEY),
#   纯递归合并会让新旧凭据并存; 与 ccc 的 stripAnthropicEnv(启动前丢弃父进程全部
#   ANTHROPIC_*)同一思路。远端其余顶层键(hooks / plugins / marketplaces / model /
#   theme / permissions ...)原样保留。
#   modelPicker 由 ccc export 直接提供(非 openrouter profile 生成三档;openrouter 不生成该键)。
#   export 不带该键时, 远端残留的同名键会被**删除** —— 否则切到 openrouter 后
#   /model 里仍是上一个供应商的模型(旧 compose 行为即"该键整个不写")。
#
# 不做 .env / CURRENT_MODEL: 本脚本只走 settings.json 一条路径,
#   CURRENT_MODEL 那套仍是 ccc_nosetting_last.ps1 的职责, 此处不引入。
#
# 菜单尾注:Get-CccProfileMeta(ccc list --tsv)拿 price/context, 在名字后显示 【context】【price】
#   (排版与 ccc_switch.ps1 同源, 公共实现见 ccc_common.ps1)
#
# 用法:
#   pwsh -NoProfile -ExecutionPolicy Bypass -File ccc_settings_lan.ps1
#   pwsh ... -File ccc_settings_lan.ps1 -DryRun                 # 只算不写, 打印 env 差异
#   pwsh ... -File ccc_settings_lan.ps1 -RemoteHost yg@other    # 换远端
[CmdletBinding()]
param(
    [string]$CccExe         = '',                      # 默认: <ScriptDir>\..\dst\ccc.exe
    [string]$RemoteHost     = 'yg@192.168.1.123',
    [string]$RemoteSettings = '/home/yg/.claude/settings.json',
    [string]$SshExe         = 'ssh',
    [int]$ConnectTimeout    = 10,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
# 双向 UTF-8: 输出给原生进程的管道 + 读取原生进程的 stdout
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }

# 公共工具: ccc.exe 解析 / .env 读写 / FAIL/STDERR 包装 / settings json 合并等。
# 三个 PS1 脚本同名副本已统一搬到这里, 见 ccc_common.ps1。
. (Join-Path $PSScriptRoot 'ccc_common.ps1')

# 头部信息块的黄色提示行 (Show-Menu 用; 非 DRY-RUN 时为空)
$dryNotice = if ($DryRun) { 'DRY-RUN (不写远端)' } else { '' }

# ---------- ccc export <name> -> settings JSON 字符串 ----------
# 失败(非零 exit)→ 抛错由调用方处理。
# & 捕获的 stdout 是字符串数组;Out-String 拼成单串,后续 ConvertFrom-Json 直接吃。
# 用 Get-CccExe -Require: 远端 SSH 脚本缺 ccc.exe 直接挂,不要 fallback 到 PATH 走错版本。
function Get-CccSettings {
    param([Parameter(Mandatory)][string]$Profile)
    $exe = Get-CccExe -CccExe $CccExe -Require
    $stdout = & $exe export $Profile | Out-String
    if ($LASTEXITCODE -ne 0) {
        throw "ccc export $Profile failed (exit=$LASTEXITCODE)"
    }
    return $stdout
}

# ============ 远端层 (ssh + base64) ============

function Get-SshArgs {
    return @('-o', 'BatchMode=yes', '-o', "ConnectTimeout=$ConnectTimeout")
}

# ---------- 远端 shell 里的路径字面量 ----------
# 远端命令用单引号包路径, 因此路径自身不能含单引号 —— 提前挡掉, 不做转义技巧。
function Get-RemotePathQuoted {
    if ($RemoteSettings -match "'") {
        throw "-RemoteSettings 不能含单引号: $RemoteSettings"
    }
    return "'" + $RemoteSettings + "'"
}

function Get-RemoteDir {
    $i = $RemoteSettings.LastIndexOf('/')
    if ($i -le 0) { throw "-RemoteSettings 必须是绝对路径: $RemoteSettings" }
    return $RemoteSettings.Substring(0, $i)
}

# ---------- 跑一条远端命令 ----------
# 返回 stdout 行数组(stderr 收进临时文件, 失败时并入异常消息)。
# -AllowFail: 非零退出不抛错, 由调用方按 $LASTEXITCODE 判断(读取探测用)。
function Invoke-Remote {
    param([Parameter(Mandatory)][string]$Command, [switch]$AllowFail)
    $exe = $SshExe
    $sshArgs = Get-SshArgs
    $errFile = Join-Path $env:TEMP ("ccc_ssh_err_{0}.txt" -f [guid]::NewGuid().ToString('N'))
    $stdout = $null
    $rc = 0
    $errText = ''
    try {
        $stdout = & $exe @sshArgs $RemoteHost $Command 2> $errFile
        $rc = $LASTEXITCODE
        $errText = if (Test-Path -LiteralPath $errFile) { (Get-Content -LiteralPath $errFile -Raw -Encoding UTF8) } else { '' }
    } finally {
        Remove-Item -LiteralPath $errFile -Force -ErrorAction SilentlyContinue
    }
    if ($rc -ne 0 -and -not $AllowFail) {
        throw "ssh $RemoteHost failed (exit=$rc): $($errText.Trim())"
    }
    return ,@($stdout)
}

# ---------- 探测远端: 主机名 / 目录 / 文件 ----------
# 返回 @{ Host=...; DirExists=bool; FileExists=bool }。一个 ssh 往返。
function Test-Remote {
    $dirQ = "'" + (Get-RemoteDir) + "'"
    $fileQ = Get-RemotePathQuoted
    $cmd = "echo __HOST__=`$(hostname); " +
           "if [ -d $dirQ ]; then echo __DIR_OK__; else echo __DIR_MISSING__; fi; " +
           "if [ -f $fileQ ]; then echo __FILE_OK__; else echo __FILE_MISSING__; fi"
    $lines = (Invoke-Remote -Command $cmd) -join "`n"
    return [PSCustomObject]@{
        Host       = if ($lines -match '__HOST__=(.+)') { $Matches[1].Trim() } else { '?' }
        DirExists  = ($lines -match '__DIR_OK__')
        FileExists = ($lines -match '__FILE_OK__')
    }
}

# ---------- base64 <-> UTF-8 文本 ----------
function ConvertTo-B64   { param([string]$Text) [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($Text)) }
function ConvertFrom-B64 { param([string]$B64)  [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($B64)) }

# ---------- 读远端 settings.json -> PSCustomObject (不存在返回 $null) ----------
# 用 base64 -w0 走管道: 绕开换行/编码/引号在 ssh 往返中的损耗, 字节级不丢。
function Get-RemoteSettings {
    $q = Get-RemotePathQuoted
    $cmd = "if [ -f $q ]; then base64 -w0 $q; else echo __MISSING__; fi"
    $b64 = ((Invoke-Remote -Command $cmd) -join '').Trim()
    if ($b64 -eq '' -or $b64 -eq '__MISSING__') { return $null }
    $text = ConvertFrom-B64 $b64
    return ($text | ConvertFrom-Json)
}

# ---------- 写远端 settings.json (原子替换 + 保留权限) ----------
# 1) base64 -d 落 <path>.tmp (stdin), chmod 沿用旧文件权限(旧文件不存在则 chmod 600)
# 2) mv -f 原子替换
function Set-RemoteSettings {
    param([Parameter(Mandatory)][string]$JsonText)

    $q    = Get-RemotePathQuoted
    $dirQ = "'" + (Get-RemoteDir) + "'"
    $b64  = ConvertTo-B64 $JsonText

    $cmd = "set -e; " +
           "mkdir -p $dirQ; " +
           "base64 -d > $q.tmp; " +
           "chmod --reference=$q $q.tmp 2>/dev/null || chmod 600 $q.tmp; " +
           "mv -f $q.tmp $q; " +
           "echo __WROTE__"

    $exe = $SshExe
    $sshArgs = Get-SshArgs
    $errFile = Join-Path $env:TEMP ("ccc_ssh_err_{0}.txt" -f [guid]::NewGuid().ToString('N'))
    $out = $null
    $rc = 0
    $errText = ''
    try {
        # base64 全是 ASCII, 经 $OutputEncoding(UTF8 无 BOM)管道进 ssh stdin, 无损
        $out = ($b64 | & $exe @sshArgs $RemoteHost $cmd 2> $errFile) -join "`n"
        $rc = $LASTEXITCODE
        $errText = if (Test-Path -LiteralPath $errFile) { (Get-Content -LiteralPath $errFile -Raw -Encoding UTF8) } else { '' }
    } finally {
        Remove-Item -LiteralPath $errFile -Force -ErrorAction SilentlyContinue
    }
    if ($rc -ne 0) {
        throw "远端写入失败 (exit=$rc): $($errText.Trim())"
    }
    if ($out -notmatch '__WROTE__') {
        throw "远端写入未确认 (stdout=$out)"
    }
}

# ---------- 远端当前 provider (菜单 [current] 标记) ----------
function Get-RemoteProvider {
    try {
        $j = Get-RemoteSettings
        if ($null -eq $j) { return $null }
        return $j.current_Model_Provider
    } catch { return $null }
}

# ---------- 合并: 远端为底, export 为顶; env 块整体替换 ----------
# env 替换是**刻意**的(见文件头注释): 不留远端上一个 provider 的凭据残根。
# 返回 @{ Merged=对象; DroppedEnv=远端 env 里被丢弃的键; OverwrittenEnv=被覆盖的键 }
function Merge-RemoteAndExport {
    param([PSCustomObject]$Remote, [PSCustomObject]$Export, [string]$Profile)

    $result = [ordered]@{}
    if ($null -ne $Remote) {
        foreach ($p in $Remote.PSObject.Properties) { $result[$p.Name] = $p.Value }
    }

    $dropped = @()
    $overwritten = @()

    foreach ($p in $Export.PSObject.Properties) {
        if ($p.Name -eq 'env' -and $p.Value -is [PSCustomObject]) {
            $oldEnv = if ($null -ne $Remote) { $Remote.env } else { $null }
            if ($oldEnv -is [PSCustomObject]) {
                $newKeys = @($p.Value.PSObject.Properties.Name)
                $dropped = @($oldEnv.PSObject.Properties.Name | Where-Object { $newKeys -notcontains $_ })
                $overwritten = @($oldEnv.PSObject.Properties.Name | Where-Object { $newKeys -contains $_ })
            }
            $result[$p.Name] = $p.Value
            continue
        }
        if ($p.Value -is [PSCustomObject] -and $result[$p.Name] -is [PSCustomObject]) {
            $result[$p.Name] = Merge-JsonObject -Source $p.Value -Target $result[$p.Name]
        } else {
            $result[$p.Name] = $p.Value
        }
    }

    # ps1 菜单用此字段标记当前选择; ccc export 不含它, 这里盖一层
    $result['current_Model_Provider'] = $Profile

    # export 不含 modelPicker(openrouter profile 整个键不写)→ 不能把远端上一个 provider
    # 的 picker 留在原地, 否则 /model 里仍是旧供应商的模型。整键删除, 与旧 compose 行为一致。
    $droppedPicker = $false
    if (-not ($Export.PSObject.Properties.Name -contains 'modelPicker') -and $result.Contains('modelPicker')) {
        $result.Remove('modelPicker')
        $droppedPicker = $true
    }

    return [PSCustomObject]@{
        Merged         = [PSCustomObject]$result
        DroppedEnv     = $dropped
        OverwrittenEnv = $overwritten
        DroppedPicker  = $droppedPicker
    }
}

# ---------- 菜单 ----------
# 排版(分隔线 / 头部信息块 / 尾注【context】【price】按列对齐 / [current] / 0. exit)
# 走 ccc_common.ps1 的共用实现, 与 ccc_switch.ps1 观感一致。
function Show-Menu {
    param($Entries, [string]$Current, [hashtable]$Meta)

    Write-MenuHeader -Title 'Claude Code Settings Switcher (LAN / remote via ssh)' -Info ([ordered]@{
        ccc    = (Get-CccExe -CccExe $CccExe -Require)
        remote = $RemoteHost
        target = $RemoteSettings
        effect = '远端 settings.json 直改, 远端 claude 重开会话生效'
        tail   = '【context】【price $/M】(仅 openrouter 系有元数据时显示)'
    }) -Notice $dryNotice

    Write-MenuRows -Entries $Entries -Current $Current -Meta $Meta
    Write-MenuFooter
    return (Read-MenuChoice -Entries $Entries)
}

# ---------- 切换: 本地 export -> 合并 -> 写远端 ----------
function Switch-RemoteConfiguration {
    param($Selected)

    if ([string]::IsNullOrEmpty($Selected)) { return }

    try {
        $exportJson = Get-CccSettings -Profile $Selected | ConvertFrom-Json
        $remoteJson = Get-RemoteSettings

        $m = Merge-RemoteAndExport -Remote $remoteJson -Export $exportJson -Profile $Selected

        if ($m.DroppedEnv.Count -gt 0) {
            Write-Host ""
            Write-Host "env 丢弃(远端遗留, export 里没有):" -ForegroundColor DarkYellow
            foreach ($k in $m.DroppedEnv) { Write-Host "  - $k" -ForegroundColor DarkYellow }
        }
        if ($m.OverwrittenEnv.Count -gt 0) {
            Write-Host ("env 覆盖: {0}" -f ($m.OverwrittenEnv -join ', ')) -ForegroundColor DarkGray
        }
        if ($m.DroppedPicker) {
            Write-Host "modelPicker 已删除(该 profile 不生成此键, 避免残留旧供应商选项)" -ForegroundColor DarkYellow
        }

        $jsonText = ($m.Merged | ConvertTo-Json -Depth 10) + "`n"

        if ($DryRun) {
            Write-Host ""
            Write-Host "DRY-RUN: 未写远端. 合成后 $($jsonText.Length) 字节, 目标 $RemoteSettings" -ForegroundColor Yellow
            return
        }

        Set-RemoteSettings -JsonText $jsonText

        # 回读校验: 远端文件真解析得出所选 provider 才算成功
        $back = Get-RemoteProvider
        if ($back -ne $Selected) {
            throw "回读校验失败: 远端 current_Model_Provider='$back', 期望 '$Selected'"
        }

        Write-Host ""
        Write-Host "Switched to: $Selected" -ForegroundColor Green
        Write-Host "$RemoteHost : $RemoteSettings 已更新" -ForegroundColor Green
        Write-Host "远端 claude 需重开会话生效" -ForegroundColor DarkGray
    }
    catch {
        Write-Host ""
        Write-Host "Failed to switch: $_" -ForegroundColor Red
    }
}

# ============== main ==============
# dot-source 时只加载函数, 供测试 harness 复用
if ($MyInvocation.InvocationName -eq '.') { return }

$RemoteSettings = $RemoteSettings.Replace('\', '/')   # 目标始终是远端 POSIX 路径
[void](Get-RemotePathQuoted)                          # 提前挡掉含单引号的非法路径

Write-Host "Loading profiles from ccc list ..." -ForegroundColor Green
Write-Host "Probing $RemoteHost ..." -ForegroundColor Green

try {
    $probe = Test-Remote
    Write-Host ("remote: {0}  target: {1}  ({2})" -f `
        $probe.Host, $RemoteSettings, $(if ($probe.FileExists) { 'exists, will overwrite' } else { 'missing, will create' })) -ForegroundColor DarkGray

    $Profiles = Get-CccProfiles -CccExe $CccExe
    if (-not $Profiles -or $Profiles.Count -eq 0) {
        throw "no profiles from ccc list (check kdbx group / path)"
    }

    $Current = Get-RemoteProvider
    $Meta    = Get-CccProfileMeta -CccExe $CccExe   # name->@{Model;Price;Context}; 失败则空表(降级不显示)

    while ($true) {
        try { Clear-Host } catch { }   # 重定向/管道下无控制台, 忽略
        $sel = Show-Menu -Entries $Profiles -Current $Current -Meta $Meta
        if ($null -eq $sel) { break }
        Switch-RemoteConfiguration $sel
        $Current = Get-RemoteProvider   # 刷新 [current] 标记
        Pause
    }
}
catch {
    Write-Host ""
    Write-Host "FATAL: $_" -ForegroundColor Red
    Fail-And-Exit 1
}
