# ccc_switch.ps1
# 切换 dst/.env 的 CURRENT_MODEL=<profile>,**不写** ~/.claude/settings.json
# 与 ccc_settings.ps1 互补:那个走 settings.json 路径,这个走 CURRENT_MODEL 路径
# 服务场景: Explorer 右键菜单链(add_claude_menu_ccc.reg → ccc last → 读 CURRENT_MODEL)
#   - 用户先在本菜单选定 profile → dst/.env 的 CURRENT_MODEL= 更新
#   - 之后右键启动走 ccc last,无需再选 profile
#
# 数据源同 ccc_settings.ps1:ccc list 拿 profile 名 + kdbx 凭据自动 store 路径
# 菜单尾注:Get-CccProfileMeta(ccc list --tsv)拿 price/context, 在名字后显示 【context】【price】
# 写盘算法(等同 todolist T3):
#   按行读 dst/.env, 存在 CURRENT_MODEL= 行则替换; 否则追加在文件末尾
#   其余行(注释 / 其它键 / 空行)原样保留
#
# 用法:  pwsh -NoProfile -ExecutionPolicy Bypass -File ccc_switch.ps1
[CmdletBinding()]
param(
    [string]$CccExe     = '',   # 默认: <ScriptDir>\..\dst\ccc.exe
    [string]$EnvPath    = ''    # 默认: <ScriptDir>\..\dst\.env (可不存在,首次切换会创建)
)

$ErrorActionPreference = "Stop"
# 双向 UTF-8: 输出给原生进程的管道 + 读取原生进程的 stdout
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# 公共工具: ccc.exe 解析 / .env 读写 / FAIL/STDERR 包装 / settings json 合并等。
# 三个 PS1 脚本同名副本已统一搬到这里, 见 ccc_common.ps1。
. (Join-Path $PSScriptRoot 'ccc_common.ps1')

# ---------- env 文件路径 ----------
# -EnvPath 绝对路径 → 用它
# 否则: <ScriptDir>\..\dst\.env (首次切换会自动新建文件, 不要求预先存在)
function Get-EnvPath {
    if ($EnvPath -ne '') {
        $parent = Split-Path -Parent $EnvPath
        if (-not (Test-Path -LiteralPath $parent)) {
            Write-Host "env parent dir not found: $parent" -ForegroundColor Red
            Fail-And-Exit 1
        }
        return $EnvPath
    }
    return (Join-Path $ScriptDir '..\dst\.env')
}

# ---------- 互斥: 有效切换后同步右键菜单为加密模式 ----------
# 本脚本 = 加密路径(写 dst/.env CURRENT_MODEL,右键菜单走 ccc last),有效切换后必须:
#   1. 右键菜单幂等 set 为 ccc last + 粉碎 ~/.claude/settings.json。
#      全部由 ccc setMode ENCRYPTED 一条命令搞定 (src/mode.go runSetModeEncrypted):
#      菜单已是加密零写入, 未安装直接装加密, 粉碎失败只 warn (exit 0) — 与本脚本
#      之前手动拼 set_rightmenu_encryption + filePulverizer 的语义完全一致。
# 闸门只挡 SHOWSETTINGS (Phase 2), ENCRYPTED 方向永远允许, 此函数不会收到 exit 4.
function Sync-RightMenuEncrypted {
    try {
        $exe = Get-CccExe
        $out = & $exe setMode ENCRYPTED 2>&1
        $rc = $LASTEXITCODE
        if ($rc -ne 0) {
            Write-Host "[warn] ccc setMode ENCRYPTED exit=$rc : $out" -ForegroundColor Yellow
            return
        }
        if ($out) { Write-Host "       $out" -ForegroundColor DarkGray }
        Write-Host '[ok] 右键菜单 = 加密 (ccc last) + settings.json 已清理/粉碎' -ForegroundColor Green
    } catch {
        Write-Host "[warn] 同步右键菜单为加密失败: $_" -ForegroundColor Yellow
    }
}

# ---------- 启动校验: 非加密模式 → 切加密 ----------
# ccc_switch 是加密路径。启动时若右键菜单不是加密 (明文/未安装), 说明系统处于
# 明文态 (settings.json 存在): 先把它的 current_Model_Provider 迁入
# .env CURRENT_MODEL (ccc last 启动链读它), 再走 Sync-RightMenuEncrypted
# (菜单 set 加密 + 粉碎 settings.json)。
# settings.json 无 current_Model_Provider → 保留 .env 原值, 仍切加密。
# 唯一查询口: ccc setMode (无参),NONE 视为非加密,维持旧 Get-RightMenuTag 语义。
# 闸门只挡 SHOWSETTINGS (Phase 2), 加密方向永远允许, 此函数不会收到 exit 4.
function Assert-EncryptedModeOnStart {
    $exe = Get-CccExe
    $errFile = Join-Path $env:TEMP ("ccc_setmode_query_err_{0}.txt" -f [guid]::NewGuid().ToString('N'))
    $cur = $null
    try {
        $cur = (& $exe setMode 2> $errFile).Trim()
        $rc = $LASTEXITCODE
    } finally {
        Remove-Item -LiteralPath $errFile -Force -ErrorAction SilentlyContinue
    }
    if ($rc -ne 0) {
        Write-Host "[warn] ccc setMode (查询) exit=$rc, 跳过启动校验" -ForegroundColor Yellow
        return
    }
    if ($cur -eq 'ENCRYPTED') { return }

    Write-Host "[info] 当前非加密模式 (query=$cur), 执行切加密 ..." -ForegroundColor Yellow

    $settingsPath = Join-Path $env:USERPROFILE '.claude\settings.json'
    if (Test-Path -LiteralPath $settingsPath) {
        $model = $null
        try { $model = (Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json).current_Model_Provider } catch {}
        if ($model) {
            try {
                Set-CurrentModel -Path $EnvFile -Profile $model
                Write-Host "[ok] settings.json 当前模型 '$model' 已写入 $EnvFile" -ForegroundColor Green
            } catch {
                Write-Host "[warn] 写 $EnvFile 失败: $_ (.env 保留原值)" -ForegroundColor Yellow
            }
        } else {
            Write-Host '[warn] settings.json 无 current_Model_Provider, 保留 .env 原 CURRENT_MODEL' -ForegroundColor Yellow
        }
    } else {
        Write-Host "[info] $settingsPath 不存在, .env CURRENT_MODEL 保持原值" -ForegroundColor DarkGray
    }

    # 切加密: 右键菜单 ccc last + 粉碎 settings.json (幂等)
    Sync-RightMenuEncrypted
}

# ---------- 显示宽度 / 菜单行 / 头部 / 底部 / 选择读取 ----------
# 已搬进 ccc_common.ps1 (菜单排版三脚本共用一份实现), 此处不再私有一份。
# 本脚本独有的是 Sync-RightMenuEncrypted / Assert-EncryptedModeOnStart 那条加密链。

# ---------- 菜单 ----------
function Show-Menu {
    param($Entries, [string]$Current, [hashtable]$Meta)

    Write-MenuHeader -Title 'Claude Code CURRENT_MODEL Switcher (no settings.json write)' -Info ([ordered]@{
        ccc    = (Get-CccExe -CccExe $CccExe -Require)
        target = $EnvFile
        effect = 'Explorer 右键菜单 → ccc last → 选中 profile'
        tail   = '【context】【price $/M】(仅 openrouter 系有元数据时显示)'
    })

    Write-MenuRows -Entries $Entries -Current $Current -Meta $Meta
    Write-MenuFooter
    return (Read-MenuChoice -Entries $Entries)
}

# ---------- 切换 ----------
function Switch-CurrentModel {
    param($Selected)

    if ([string]::IsNullOrEmpty($Selected)) { return }

    try {
        Set-CurrentModel -Path $EnvFile -Profile $Selected
        Write-Host ""
        Write-Host "CURRENT_MODEL -> $Selected" -ForegroundColor Green
        Write-Host "$EnvFile updated (其它键原样保留)" -ForegroundColor Green

        # 互斥: 有效切换 → 右键菜单同步为加密模式 (ccc last) + 粉碎明文 settings.json
        Sync-RightMenuEncrypted
    }
    catch {
        Write-Host ""
        Write-Host "Failed to switch: $_" -ForegroundColor Red
    }
}

# ============== main ==============
# dot-source 时只加载函数, 供测试 harness 复用
if ($MyInvocation.InvocationName -eq '.') { return }

Write-Host "Loading profiles from ccc list ..." -ForegroundColor Green

$EnvFile = Get-EnvPath

try {
    # 启动校验: 非加密模式 → 模型迁入 .env + 菜单切加密 + 粉碎 settings.json
    Assert-EncryptedModeOnStart

    $Profiles = Get-CccProfiles -CccExe $CccExe
    if (-not $Profiles -or $Profiles.Count -eq 0) {
        throw "no profiles from ccc list (check kdbx group / path)"
    }

    $Current = Get-CurrentModel -Path $EnvFile
    $Meta    = Get-CccProfileMeta -CccExe $CccExe   # name->@{Model;Price;Context}; 失败则空表(降级不显示)

    while ($true) {
        try { Clear-Host } catch { }   # 重定向/管道下无控制台, 忽略
        $sel = Show-Menu -Entries $Profiles -Current $Current -Meta $Meta
        if ($null -eq $sel) { break }
        Switch-CurrentModel $sel
        $Current = Get-CurrentModel -Path $EnvFile   # 刷新 [CURRENT_MODEL] 标记
        Pause
    }
}
catch {
    Write-Host ""
    Write-Host "FATAL: $_" -ForegroundColor Red
    Fail-And-Exit 1
}