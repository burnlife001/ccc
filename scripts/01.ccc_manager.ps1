# ccc_manager.ps1 — ccc 统一管理入口
#   1. build                    go build → dst/ccc.exe
#   2. install                  生成默认 .env (如缺失, 供 cred store 读 CCC_KDBX_PATH)
#                               + ccc install (symlink + HKCU Path) + 种籽 .cred (kdbx 在场则覆盖式
#                                 `cred sync` 灌 masterPw+providers 全量 —— 无论 .cred 是否已存在;
#                                 缺席则仅当 .cred 缺失时 `cred store` 补 masterPw)
#                               + `ccc publicMachine init` 首次初始化闸门为 TRUE (默认上锁, 幂等, 不改写已有 flag)
#                               + 首次安装从 profile 列表(kdbx 编辑源或 .cred 快照)补默认 .env CURRENT_MODEL
#                                 (仅当 .env 尚无该键, 已有值不动; 走 ccc_common Set-CurrentModel)
#   3. toggle rightmenu encryption 切换右键菜单 (claude 明文 ⇄ ccc last kdbx),所有副作用
#                                       交给 `ccc setMode` 一条命令 (src/mode.go):
#                                         - 切加密: 菜单 ccc last + 粉碎 settings.json
#                                         - 切明文: 菜单 claude + 从 profile/.env 还原 settings.json
#                                       模型解析失败 (exit 3) → 兜底 ccc_settings_local.ps1 交互菜单。
#                                       公共机闸门拒绝 (exit 4, Phase 2) → 提示去菜单 4 关闭。
#   4. toggle public machine     切换公共机闸门 (dst/.cred 信封内的 public 字段, Phase 2):
#                                       - TRUE  -> off: 交互式输 kdbx 主密码;在场则解锁 kdbx 验证,
#                                                  缺席则与 .cred 已存主密码比对 —— 通过才写 FALSE
#                                       - FALSE -> on : 强制联动 setMode ENCRYPTED 再写 TRUE
#                                       唯一查询口: `ccc publicMachine status` (stdout TRUE/FALSE)
#   5. delete .cred              ccc cred delete (pulverize .cred + 连带清旧格式残留, src/pulverize.go)
#   6. delete apikey.kdbx        kdbx 已退化为可选编辑源 —— 本项销毁该编辑源(不影响 .cred 运行)。
#                                       ccc filePulverizer 同源;y/n 二次确认后执行,无兜底。
#                                       路径 = .env CCC_KDBX_PATH (与 Go 端 kdbxPath() 同源;
#                                       相对路径锚 <exeDir>),菜单行直接展示解析结果。
#   7. 生成 ccc_list.md        把 kdbx 里所有可用 profile 列成 `ccc <profile> -p` 清单,
#                                       写到脚本所在目录 scripts/ccc_list.md (数据源: `ccc list --tsv`)。
#   0. exit
#
# 用法: 仓库根目录 `pwsh ./ccc_manager.ps1`
# 切换方向由 `ccc setMode` 无参查询自报,菜单 3 调一次 `setMode <next>` 即完成全部副作用。
# 菜单 5/6 走 `ccc filePulverizer` 真粉碎,无 BAT 兜底;action 是破坏性的
# (菜单 5 跑完会要求重输 kdbx 主密码;菜单 3 切到加密模式后 settings.json 不可恢复;
# 菜单 6 只销毁 kdbx 编辑源,运行凭据 .cred 不受影响,但 providers 快照将失去再同步来源)。

[Console]::OutputEncoding = [Text.Encoding]::UTF8
$ErrorActionPreference = 'Stop'

# $PSScriptRoot = 此脚本所在的 scripts/ 子目录;仓库根是它的父目录。
# 兼容从仓库根或 scripts/ 任一目录启动,避免 Get-CccExe/Get-EnvFilePath 漏到 scripts/dst/。
$RepoRoot = Split-Path -Parent $PSScriptRoot
$ExeName  = 'ccc.exe'
$OutDir   = 'dst'
$Version  = '1.0.1'

# 公共工具: ccc.exe 解析 / .env 读写 / menu tag / kdbx cred 路径 / settings json 合并等。
# 三个 PS1 脚本同名副本已统一搬到这里, 见 ccc_common.ps1。
. (Join-Path $PSScriptRoot 'ccc_common.ps1')

function Build-Ccc {
    Push-Location $RepoRoot
    try {
        $cacheRoot = Join-Path $env:USERPROFILE 'AppData/Local/go-build'
        if (-not (Test-Path $cacheRoot)) { New-Item -ItemType Directory -Force -Path $cacheRoot | Out-Null }
        $env:GOCACHE = $cacheRoot
        $env:GOMODCACHE = (Join-Path $env:USERPROFILE 'go/pkg/mod')

        if (-not (Test-Path $OutDir)) { New-Item -ItemType Directory -Force -Path $OutDir | Out-Null }
        $outExe = Join-Path $OutDir $ExeName
        Write-Host "[build] go build -trimpath -ldflags -s -w -X main.version=$Version -o $outExe ./src"
        go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $outExe ./src
        if ($LASTEXITCODE -ne 0) { throw "go build failed (exit $LASTEXITCODE)" }
        Write-Host "[ok] built $outExe version $Version" -ForegroundColor Green
    } finally {
        Pop-Location
    }
}

function Install-Ccc {
    $exe = Get-CccExe
    $envPath = Get-EnvFilePath -CccExe $exe
    $credPath = Get-KdbxCredPath -CccExe $exe -EnvPath $envPath

    # 首次部署 .env 缺失:cred store 依赖 CCC_KDBX_PATH(Go 从 <exeDir>/.env 读),
    # 而 .env 正常由后面的 `ccc install` 生成 —— 顺序反了会先炸在 cred store。
    # 这里在 cred store 之前补一个默认 .env: 写 Go 同款默认值 ..\kdbx\apikey.kdbx
    # (相对 <exeDir>=dst/, 与 defaultInstallKdbxPath / defaultDotEnvContent 字节一致),
    # 仅当 .env 缺失时写, 已有 .env 一律不动(用户编辑优先, 与 ensureDefaultDotEnvAt 同语义)。
    if (-not (Test-Path -LiteralPath $envPath)) {
        $defaultKdbx = '..\kdbx\apikey.kdbx'
        [System.IO.File]::WriteAllText($envPath, "CCC_KDBX_PATH=$defaultKdbx`n", [System.Text.UTF8Encoding]::new($false))
        Write-Host "[ok] 已生成默认 .env: $envPath (CCC_KDBX_PATH=$defaultKdbx)" -ForegroundColor Green
    }

    # 种籽策略(2026-10-26 裁定,覆盖语义):
    #   kdbx 编辑源在场 → **无论 .cred 是否已存在**都跑 `cred sync`:输一次密码,
    #   把 masterPw + providers 快照从 kdbx 全量灌进 .cred(闸位 public 原样保留);
    #   缺席 → 仅当 .cred 缺失时 `cred store` 两次输入一致补 masterPw(没有数据源,
    #   已有 .cred 不动)。
    $kdbxPath = Get-KdbxPath
    $kdbxThere = $kdbxPath -and (Test-Path -LiteralPath $kdbxPath)
    $credThere = $credPath -and (Test-Path -LiteralPath $credPath)
    $seedCmd = $null
    if ($kdbxThere) { $seedCmd = 'sync' }
    elseif (-not $credThere) { $seedCmd = 'store' }

    if ($seedCmd) {
        if ($kdbxThere) {
            Write-Host "[info] 检测到 kdbx 编辑源: $kdbxPath → 覆盖式灌入 masterPw + providers(闸位保留)" -ForegroundColor Yellow
        } else {
            Write-Host "[info] .cred 不存在且 kdbx 编辑源缺席 → 只写 masterPw(两次输入一致)" -ForegroundColor Yellow
        }
        Write-Host ""
        Write-Host "=============================================" -ForegroundColor Cyan
        Write-Host " seeding credential file ($seedCmd)" -ForegroundColor Green
        Write-Host " enter kdbx master password to save." -ForegroundColor Green
        Write-Host "=============================================" -ForegroundColor Cyan
        Write-Host ""

        # 不重定向 stderr: 交互式密码输入 — Go 端 readPasswordInteractive 把 prompt 与
        # 每个字符的 '*' 全写 stderr (src/kdbx.go:readPasswordInteractive);被 2> 吞了
        # 用户看到的就是"无反应"。错误信息 Go 端会直接打到宿主 stderr,这里靠 $rc 判成败。
        # 注: 此处不套 try/catch/finally — PowerShell 规定 try 必须配 catch 或 finally,
        #     而本块抛错由菜单层 `try { Install-Ccc } catch { ... }` 统一处理。
        & $exe cred $seedCmd
        $rc = $LASTEXITCODE

        if ($rc -ne 0) {
            Write-Host "[fail] ccc cred $seedCmd exit=$rc" -ForegroundColor Red
            throw "cred $seedCmd failed (exit $rc)"
        }

        if (Test-Path -LiteralPath $credPath) {
            Write-Host "[ok] .cred 已写入: $credPath" -ForegroundColor Green
        } else {
            Write-Host "[warn] cred $seedCmd 执行成功但文件未创建: $credPath" -ForegroundColor Yellow
        }

        # 首次安装落盘初始 TRUE flag(默认上锁):readPublicMachine 对缺失文件本就
        # fail-closed 为 TRUE, init 把"缺失即 TRUE"落成显式 sealed 记录, 状态机从
        # 安装点起自洽。机主要用私人机功能(切明文/export)走菜单 4 输密码 off。
        # init 幂等 —— 闸位已存在则不碰(sync 覆盖也不会动 public,双重保险)。
        $r = Invoke-Ccc -Command publicMachine, init -RedirectStderr
        if ($r.Rc -ne 0) {
            Write-Host "[warn] ccc publicMachine init exit=$($r.Rc)(闸门仍 fail-closed 为 TRUE, 可重跑菜单 2 或菜单 4 手动处理)" -ForegroundColor Yellow
            if ($r.ErrText) { Write-Host "       $($r.ErrText)" -ForegroundColor DarkYellow }
        } elseif (-not $credThere) {
            if ($r.ErrText) { Write-Host "       $($r.ErrText)" -ForegroundColor DarkGray }
            Write-Host "[ok] 公共机闸门已初始化为 TRUE(默认上锁; 菜单 4 输密码可解锁私人机)" -ForegroundColor Green
        }
        Write-Host ""
    } else {
        Write-Host "[info] kdbx 编辑源缺席且 .cred 已存在, 跳过种籽: $credPath" -ForegroundColor DarkGray
    }

    # 执行 ccc install
    Write-Host "[install] $exe install"
    & $exe install
    if ($LASTEXITCODE -ne 0) { throw "ccc install failed (exit $LASTEXITCODE)" }
    Write-Host '[ok] install 完成 (symlink ~/.local/bin + HKCU Path)' -ForegroundColor Green

    # 首次安装补默认模型: 从 kdbx 挑首个 profile 写进 .env CURRENT_MODEL。
    # 与上方补默认 .env 同语义 — 仅当 .env 尚无 CURRENT_MODEL 时才写, 已有值一律不动
    # (用户配置优先)。profile 列表走 `ccc list`, 取首个 [kdbx] 行; 复用
    # ccc_common.ps1 现成 Set-CurrentModel (与 ccc.exe T3 行为等价)。
    # kdbx 缺失 / list 失败 / 无 profile -> 仅提示, 不阻断 install (菜单 3 可手动选)。
    $existing = Get-CurrentModel -Path $envPath
    if ($existing) {
        Write-Host "[info] .env 已有 CURRENT_MODEL, 保持不动: $existing" -ForegroundColor DarkGray
    } else {
        $r = Invoke-Ccc -Command list -RedirectStderr
        $first = $null
        if ($r.Rc -eq 0) {
            foreach ($line in $r.Out) {
                # 只取第一个 token 当 profile 名(后面几列是展示元数据)
                if ($line -match '^\[kdbx\]\s+(\S+)') { $first = $Matches[1]; break }
            }
        }
        if ($first) {
            Set-CurrentModel -Path $envPath -Profile $first
            Write-Host "[ok] 已写入默认模型 .env: CURRENT_MODEL=$first" -ForegroundColor Green
        } else {
            Write-Host '[info] kdbx 暂无可选 profile, 跳过默认模型填充 (菜单 3 可手动切明文/选模型)' -ForegroundColor Yellow
            if ($r.Rc -ne 0 -and $r.ErrText) { Write-Host "       $($r.ErrText)" -ForegroundColor DarkYellow }
        }
    }
}

# .cred 凭据信封粉碎(绕过回收站):走 `ccc cred delete` —— Go 侧 pulverize
# (3 遍随机覆写 + fsync + 随机改名 + 删除, src/pulverize.go),
# 并**连带清除旧格式残留**(.kdbx_master.cred / .public_machine.flag):
# 旧 blob 是 DPAPI LOCAL_MACHINE 口径,同机任何用户可解,不清等于没删。
# 文件被占用 → Go 侧报错 → PowerShell 警告即可,不接兜底,留给用户手动关占用。
function Remove-KdbxCred {
    $exe = Get-CccExe
    $envPath = Get-EnvFilePath -CccExe $exe
    $credPath = Get-KdbxCredPath -CccExe $exe -EnvPath $envPath
    if (-not $credPath) { return }

    Write-Host ('[target] {0}' -f $credPath) -ForegroundColor Cyan
    if (Test-Path -LiteralPath $credPath) {
        $size = (Get-Item -LiteralPath $credPath).Length
        Write-Host "  当前: 存在 (size=$size bytes, mode=0600)"
    } else {
        Write-Host '  当前: 不存在 — 仍会清理旧格式残留(若有)' -ForegroundColor DarkGray
    }

    Write-Host '[step] ccc cred delete' -ForegroundColor White
    $r = Invoke-Ccc -Command cred, delete -RedirectStderr
    if ($r.Rc -ne 0) {
        Write-Host "[warn] ccc cred delete exit=$($r.Rc)" -ForegroundColor Yellow
        if ($r.ErrText) { Write-Host "       $($r.ErrText)" -ForegroundColor DarkYellow }
        Write-Host "       (失败仅提示, 不再自动兜底; 关掉占用进程后手动重跑菜单 5)" -ForegroundColor Yellow
        return
    }
    if ($r.Out) { Write-Host "       $($r.Out -join "`n       ")" -ForegroundColor DarkGray }
    Write-Host '[ok] 已彻底粉碎 (随机覆写 3 遍 + 改名 + 删除)' -ForegroundColor Green
}

# apikey.kdbx 磁盘粉碎(绕过回收站):路径与 Go 端 kdbxPath() 同源
# (从 .env CCC_KDBX_PATH 读, 相对路径锚 <exeDir>;见 ccc_common.ps1:Get-KdbxPath)。
# y/n 二次确认: 仅接收 y / n (大小写不敏感);空回车或其它输入均视为拒绝,
# 默认 = 拒绝, 防止连按回车误删。
# 调 `ccc filePulverizer` (src/pulverize.go: 3 遍随机覆写 + fsync + 随机改名 + 删除),
# 与菜单 5 同源, 不接兜底, 失败仅提示。
# 危险: 跑完 apikey.kdbx + .cred (若有) 双毁, kdbx 主密码也无法再读出。

# y/n 二次确认 helper: 仅接收 y / n (大小写不敏感), 其它输入提示重输。
# 与脚本其它 Read-Host 默认 (空回车 = 默认值) 不同 — 这里"默认 = 拒绝",
# 防止用户连按回车误删。返回值 $true = 确认, $false = 取消。
function Confirm-KdbxDelete {
    while ($true) {
        $ans = (Read-Host '[confirm] 真要粉碎该 kdbx? 输入 y 确认, n 取消').Trim()
        switch ($ans.ToLower()) {
            'y'   { return $true }
            'yes' { return $true }
            'n'   { return $false }
            'no'  { return $false }
            default {
                Write-Host "       (输入不合法, 只接受 y / n)" -ForegroundColor DarkYellow
            }
        }
    }
}

function Invoke-Remove-Kdbx {
    $kdbxPath = Get-KdbxPath
    if (-not $kdbxPath) {
        Write-Host '[fail] 未配置 CCC_KDBX_PATH, 无法定位 kdbx 文件' -ForegroundColor Red
        return
    }

    Write-Host ('[target] {0}' -f $kdbxPath) -ForegroundColor Cyan
    if (-not (Test-Path -LiteralPath $kdbxPath)) {
        Write-Host '  当前: 不存在 — 无需任何操作' -ForegroundColor DarkGray
        return
    }
    $size = (Get-Item -LiteralPath $kdbxPath).Length
    Write-Host "  当前: 存在 (size=$size bytes)"
    Write-Host '  后果: apikey.kdbx 与 .cred 销毁后, kdbx 主密码无法恢复。' -ForegroundColor DarkYellow

    if (-not (Confirm-KdbxDelete)) {
        Write-Host '[cancel] 已取消' -ForegroundColor DarkGray
        return
    }

    Write-Host ('[step] ccc filePulverizer "{0}"' -f $kdbxPath) -ForegroundColor White
    $r = Invoke-Ccc -Command filePulverizer, $kdbxPath -RedirectStderr
    if ($r.Rc -ne 0) {
        Write-Host "[warn] ccc filePulverizer exit=$($r.Rc)" -ForegroundColor Yellow
        if ($r.ErrText) { Write-Host "       $($r.ErrText)" -ForegroundColor DarkYellow }
        Write-Host "       (失败仅提示, 不再自动兜底; 关掉占用进程后手动重跑菜单 6)" -ForegroundColor Yellow
        return
    }
    if ($r.Out) { Write-Host "       $($r.Out -join "`n       ")" -ForegroundColor DarkGray }
    Write-Host '[ok] 已彻底粉碎 (随机覆写 3 遍 + 改名 + 删除)' -ForegroundColor Green
}

function Toggle-RightMenu {
    # 1) 查当前模式: `ccc setMode` (无参) 唯一查询口。
    $r = Invoke-Ccc -Command setMode -RedirectStderr
    if ($r.Rc -ne 0) {
        Write-Host "[fail] ccc setMode (查询) exit $($r.Rc)" -ForegroundColor Red
        return
    }
    $cur = "$($r.Out)".Trim()

    # 2) 算 next: ENCRYPTED → SHOWSETTINGS;SHOWSETTINGS / NONE → ENCRYPTED
    #    方向由 ccc setMode SHOWSETTINGS/ENCRYPTED 内部状态机自决, PS1 不再二次推断。
    $next = if ($cur -eq 'ENCRYPTED') { 'SHOWSETTINGS' } else { 'ENCRYPTED' }

    Write-Host ("[toggle] {0} -> {1}" -f $cur, $next) -ForegroundColor Cyan

    # 3) 调一次 `ccc setMode <next>`: 菜单 + settings.json 生命周期全部由 Go 状态机处理。
    $r = Invoke-Ccc -Command setMode, $next -RedirectStderr
    if ($r.Rc -eq 3) {
        # SHOWSETTINGS 且模型不可解析 (无 .env CURRENT_MODEL 且 kdbx 无 profile)
        # 兜底: 拉起交互菜单让用户选 profile, 选完再 Sync-RightMenuPlain 一次。
        Write-Host '[warn] 模型不可解析, 落到 ccc_settings_local.ps1 菜单' -ForegroundColor Yellow
        $localScript = Join-Path $RepoRoot 'scripts\ccc_settings_local.ps1'
        if (Test-Path -LiteralPath $localScript) {
            & pwsh -NoProfile -ExecutionPolicy Bypass -File $localScript
        } else {
            Write-Host "[fail] $localScript 不存在, 请手动从 kdbx 选 profile" -ForegroundColor Red
        }
        return
    }
    if ($r.Rc -eq 4) {
        # 公共机闸门拒绝 (Phase 2): 只挡 SHOWSETTINGS, ENCRYPTED 方向不会触发 exit 4.
        # 此处不刷通用 fail, 明确告诉用户去菜单 4 关闸门。
        Write-Host '[fail] 公共机模式禁止切明文（菜单 4 可关闭）' -ForegroundColor Red
        return
    }
    if ($r.Rc -ne 0) {
        Write-Host "[fail] ccc setMode $next exit $($r.Rc)" -ForegroundColor Red
        if ($r.ErrText) { Write-Host $r.ErrText -ForegroundColor DarkYellow }
        return
    }
    if ($r.ErrText) { Write-Host $r.ErrText -ForegroundColor DarkGray }
    if ($r.Out) { Write-Host ("       " + ($r.Out -join "`n       ")) -ForegroundColor DarkGray }
}

# kdbx 首个 profile 探测 / 从 profile 写 settings.json: 已全部收进 `ccc setMode` 状态机。
# Toggle-RightMenu 不再各自拼字面量, 也不再需要这两个 helper, 故删除。

# 切换公共机闸门 (Phase 2):
#   - 状态查询: `ccc publicMachine status` -> stdout TRUE / FALSE (失败视作"未知")
#   - 当前 TRUE  -> `ccc publicMachine off` (交互式输 kdbx 主密码, 验证通过才写 FALSE)
#   - 当前 FALSE -> `ccc publicMachine on`  (强制联动 setMode ENCRYPTED 再写 TRUE)
# 闸门只挡 SHOWSETTINGS; 本函数只走 Go 命令, 不做闸门判断, 全部让 Go 端决定。
function Toggle-PublicMachine {
    # 1) 查当前状态: 唯一查询口是 `ccc publicMachine status`.
    $r = Invoke-Ccc -Command publicMachine, status -RedirectStderr
    if ($r.Rc -ne 0) {
        Write-Host "[fail] ccc publicMachine status exit $($r.Rc)" -ForegroundColor Red
        return
    }
    $cur = "$($r.Out)".Trim()

    # 2) 当前 TRUE -> off (输密码); 当前 FALSE -> on (强制转加密).
    if ($cur -eq 'TRUE') {
        Write-Host '[toggle] public machine TRUE -> FALSE (将交互式输 kdbx 主密码)' -ForegroundColor Cyan
        # 不重定向 stderr (Invoke-Ccc 不带 -RedirectStderr): Go 端 readPasswordInteractive
        # 把 prompt 与每个字符的 '*' 全写 stderr (同 Install-Ccc 的 cred store 注释 /
        # 整改建议 §5 PS1 清单). 被 2> 吞了用户看到的就是"无反应"。
        # 错误信息 Go 端会直接打到宿主 stderr, 这里靠 Rc 判成败。
        $rc = (Invoke-Ccc -Command publicMachine, off).Rc
    } else {
        Write-Host '[toggle] public machine FALSE -> TRUE (强制转加密)' -ForegroundColor Cyan
        $r = Invoke-Ccc -Command publicMachine, on -RedirectStderr
        $rc = $r.Rc
        # 摘要 Go 端的输出 (含 "已转加密" 等说明 + 任何错误信息).
        if ($r.ErrText) { Write-Host $r.ErrText -ForegroundColor DarkGray }
        if ($r.Out) { Write-Host ("       " + ($r.Out -join "`n       ")) -ForegroundColor DarkGray }
    }

    if ($rc -ne 0) {
        Write-Host "[fail] ccc publicMachine exit $rc" -ForegroundColor Red
        return
    }
}

# 生成 ccc_list.md: 把当前 kdbx 里所有可用 profile 列成 `ccc <profile> -p` 清单。
# 数据源 = `ccc list --tsv` (name \t model \t price \t context), 与 Go 端 list 同源,
# 脚本自己不读 kdbx。输出到脚本所在目录 (scripts/ccc_list.md), 覆盖写,
# UTF-8 无 BOM + LF; profile 为空时只提示不建文件。
function Generate-CccList {
    $r = Invoke-Ccc -Command list, '--tsv' -RedirectStderr
    if ($r.Rc -ne 0) {
        Write-Host "[fail] ccc list --tsv exit $($r.Rc)" -ForegroundColor Red
        if ($r.ErrText) { Write-Host "       $($r.ErrText)" -ForegroundColor DarkYellow }
        return
    }

    $names = @()
    foreach ($line in $r.Out) {
        if (-not $line) { continue }
        $line = "$line".TrimEnd()
        if ($line -eq '') { continue }
        $name = ($line -split "`t")[0].Trim()
        if ($name) { $names += $name }
    }

    if ($names.Count -eq 0) {
        Write-Host '[warn] kdbx 无可用 profile, 未生成 ccc_list.md' -ForegroundColor Yellow
        if ($r.ErrText) { Write-Host "       $($r.ErrText)" -ForegroundColor DarkYellow }
        return
    }

    $out = Join-Path $PSScriptRoot 'ccc_list.md'
    $lines = @('# ccc agent 命令示例', '', '```')
    foreach ($n in $names) { $lines += ('ccc {0} -p' -f $n) }
    $lines += @('```', '')

    # UTF-8 无 BOM + LF 换行 (仓库约定)
    [System.IO.File]::WriteAllText($out, ($lines -join "`n"), [System.Text.UTF8Encoding]::new($false))

    Write-Host "[ok] 已生成 $out ($($names.Count) 个 profile)" -ForegroundColor Green
    foreach ($n in $names) { Write-Host ('     ccc {0} -p' -f $n) -ForegroundColor DarkGray }
}

# 菜单动作后的暂停提示 —— 统一中文,替代 Pause 的英文 "Press Enter to continue..."。
function Wait-BackToMenu {
    [void](Read-Host "`n按 Enter 返回菜单")
}

while ($true) {
    try { Clear-Host } catch { }   # 管道/重定向下无控制台, 忽略 (对齐 ccc_switch.ps1:261)
    Write-Host '---- ccc 管理菜单 ----' -ForegroundColor Cyan
    Write-Host ''
    Write-Host '  1. 构建编译                           [dst/ccc.exe]'
    Write-Host '  2. 安装注册                           [symlink + PATH + .env + 凭据]'
    Write-Host
    # 菜单行 3 模式标签: 唯一查询口是 `ccc setMode` (无参) — 替代已被删除的 Get-MenuTag。
    # 失败 / NONE 视为未安装, 与整改建议 §3.1 状态机契约对齐。
    $menuTagLabel = '未安装'
    try {
        $r = Invoke-Ccc -Command setMode -RedirectStderr
        switch ("$($r.Out)".Trim()) {
            'ENCRYPTED'    { $menuTagLabel = '加密 (ccc last)' }
            'SHOWSETTINGS' { $menuTagLabel = '明文 (claude)' }
        }
    } catch {
        $menuTagLabel = '未安装'
    }
    Write-Host "  3. 切换右键 Claude 入口               [当前：$menuTagLabel]"
    # 公共机状态查询: `ccc publicMachine status` 唯一查询口, stdout TRUE/FALSE.
    # 失败 (含 PATH) 一律 -> 未知 (与菜单行展示契约对齐)。
    $pmStatusLabel = '未知'
    try {
        $r = Invoke-Ccc -Command publicMachine, status -RedirectStderr
        $pmStatusRaw = "$($r.Out)".Trim()
        if ($r.Rc -eq 0 -and ($pmStatusRaw -eq 'TRUE' -or $pmStatusRaw -eq 'FALSE')) {
            $pmStatusLabel = $pmStatusRaw
        }
    } catch {
        $pmStatusLabel = '未知'
    }
    Write-Host "  4. 切换公共机闸门                     [当前：$pmStatusLabel]"
    Write-Host
    Write-Host "  5. 删除 kdbx 凭据                     [文件：$(Get-CredTag)]"
    Write-Host "  6. 删除 apikey.kdbx                   [文件：$(Get-KdbxTag)]"
    Write-Host "  7. 生成 ccc_list.md                   [scripts/ccc_list.md]"
    Write-Host ''
    Write-Host '  0. 退出'
    Write-Host '---------------------' -ForegroundColor Cyan
    Write-Host ''
    switch (Read-Host '请选择') {
        '1' {
            try { Build-Ccc } catch { Write-Host "[fail] $($_.Exception.Message)" -ForegroundColor Red }
            Wait-BackToMenu
        }
        '2' {
            try { Install-Ccc } catch { Write-Host "[fail] $($_.Exception.Message)" -ForegroundColor Red }
            Wait-BackToMenu
        }
        '3' { Toggle-RightMenu; Wait-BackToMenu }
        '4' { Toggle-PublicMachine; Wait-BackToMenu }
        '5' { Remove-KdbxCred; Wait-BackToMenu }
        '6' { Invoke-Remove-Kdbx; Wait-BackToMenu }
        '7' { Generate-CccList; Wait-BackToMenu }
        '0' { exit 0 }
    }
}
