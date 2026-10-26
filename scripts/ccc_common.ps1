# ccc_common.ps1 — ccc 三个 PS1 脚本的公共工具库
#
# 内容:
#   - ccc.exe 路径解析 / 路径兜底 (Get-CccExe / Get-EnvFilePath / Get-KdbxCredPath / Get-CredTag)
#   - .env CURRENT_MODEL 读 / 写 (Get-CurrentModel / Set-CurrentModel)
#   - ccc list -> profile 名列表 (含 kdbx 凭据缺失时自动 cred store 重试一次) (Get-CccProfiles)
#   - ccc list --tsv -> profile 元数据 name->@{model;price;context} (Get-CccProfileMeta)
#   - settings.json 深合并 / 写盘 (Merge-JsonObject / Write-SettingsJson)
#   - 异常 / stderr 安全读取 (Fail-And-Exit / Read-StderrFile)
#   - 菜单 UI 排版 (Get-MenuRule / Get-DisplayWidth / Get-MenuRows / Write-MenuHeader /
#     Write-MenuRows / Write-MenuFooter / Read-MenuChoice)
#     —— switch / settings_local / settings_lan 三个菜单共用同一套渲染, 保证观感一致
#
# 用法: 三脚本头部各加一行
#   . (Join-Path $PSScriptRoot 'ccc_common.ps1')
#
# 路径约定: 本文件内的所有默认路径基于 $PSScriptRoot (= scripts/ 自身);
#   不得引用调用脚本的局部变量。
# 重要: $PSScriptRoot 在函数体内是 dot-source 调用方作用域求值,不是
#   绑定到本文件所在目录。当前成立是因为所有调用方与 common 同在 scripts/
#   目录;若调用方跨目录 dot-source 此文件(如 . 绝对路径/foo/bar/ccc_common.ps1),
#   $PSScriptRoot 会静默指向调用方,所有 <scripts/> 默认路径随即指错。

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# ---------- 失败: 打印错误 -> 暂停 -> 退出 (避免双击运行时窗口闪退看不见错误) ----------
# settings_local L27 原样搬运; 双 Pause 防护 (UserInteractive 才尝试, 异常吞掉).
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

# ---------- stderr 临时文件的安全读取 ----------
# 0 字节文件 Get-Content -Raw 返回 $null, 直接 .Trim() 会炸; 这里短路返回 ''.
# 所有 `2> $errFile` 模式都过这个函数 (manager L414 原样搬运).
function Read-StderrFile {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return '' }
    $content = Get-Content -LiteralPath $Path -Raw -Encoding UTF8 -ErrorAction SilentlyContinue
    if ($null -eq $content) { return '' }
    return $content.Trim()
}

# ---------- ccc.exe 统一调用口 ----------
# 收敛「stderr 重定向到临时文件 → finally 删文件」的重复样板 (manager 里原有 5 处)。
# 返回 @{ Out = stdout 行; Rc = $LASTEXITCODE; ErrText = 清理后的 stderr 文本 }。
# -RedirectStderr 才重定向; 不带时保留宿主 stderr — 交互式密码输入
# (cred store / publicMachine off) 的 prompt 与 '*' 全写 stderr, 被 2> 吞了
# 用户看到的就是"无反应"; 那种场景调用方靠 Rc 判成败。
# -Exe: 显式指定 ccc.exe 路径(调用脚本自己解析出来的)。空 = 走 Get-CccExe
# 默认解析(= <scripts>\..\dst\ccc.exe)。不带它时调用方的 -CccExe 参数覆盖
# 会被忽略(历史限制,见 ccc_settings_local.ps1 的说明注释)。
function Invoke-Ccc {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string[]]$Command,
        [switch]$RedirectStderr,
        [string]$Exe = ''
    )
    $exe = if ($Exe -ne '') { $Exe } else { Get-CccExe }
    $errFile = $null
    if ($RedirectStderr) {
        $errFile = Join-Path $env:TEMP ("ccc_err_{0}.txt" -f [guid]::NewGuid().ToString('N'))
    }
    try {
        if ($RedirectStderr) { $out = & $exe @Command 2> $errFile }
        else                 { $out = & $exe @Command }
        $rc = $LASTEXITCODE
        $errText = ''
        if ($RedirectStderr) { $errText = Read-StderrFile -Path $errFile }
        return @{ Out = $out; Rc = $rc; ErrText = $errText }
    } finally {
        if ($errFile) { Remove-Item -LiteralPath $errFile -Force -ErrorAction SilentlyContinue }
    }
}

# ---------- ccc.exe 路径解析 ----------
# 默认: <PSScriptRoot>\..\dst\ccc.exe (= 仓库内 dst/ccc.exe).
# 覆盖: -CccExe <path> 显式指定 (存在则解析后返回).
# -Require: 既找不到显式路径也找不到 dst/ 时 Fail-And-Exit 1;
#           不带 -Require 时 fallback 到 'ccc' (走 PATH).
# 调用方:
#   - manager 调默认 (无 -Require, 走 PATH 兜底)
#   - settings_local / ccc_switch 调 `-CccExe $CccExe -Require`
function Get-CccExe {
    [CmdletBinding()]
    param(
        [string]$CccExe,
        [switch]$Require
    )

    # 1) 显式覆盖优先: -CccExe 非空且存在 -> 解析后返回
    if ($CccExe -ne '' -and $null -ne $CccExe) {
        if (Test-Path -LiteralPath $CccExe) {
            return (Resolve-Path -LiteralPath $CccExe).Path
        }
        if ($Require) {
            Write-Host "ccc.exe not found at -CccExe '$CccExe'" -ForegroundColor Red
            Fail-And-Exit 1
        }
        return 'ccc'   # 非 Require: fallback 走 PATH
    }

    # 2) 默认: <PSScriptRoot>\..\dst\ccc.exe (= 仓库 dst/ccc.exe)
    $candidate = Join-Path $PSScriptRoot '..\dst\ccc.exe'
    $resolved = Resolve-Path -LiteralPath $candidate -ErrorAction SilentlyContinue
    if ($resolved) { return $resolved.Path }

    if ($Require) {
        Write-Host "ccc.exe not found at $candidate" -ForegroundColor Red
        Write-Host "build it first:  pwsh ./build.ps1" -ForegroundColor Red
        Fail-And-Exit 1
    }
    return 'ccc'   # 走 PATH (install 后 ~/.local/bin 在 PATH 里)
}

# ---------- ccc list -> profile 名列表 ----------
# 跑 ccc list, 解析 stdout 的 [kdbx] <name> 行.
# 若 stderr 含 "cred store"(kdbx 凭据缺失), 自动拉起 ccc cred store(交互式)后重试一次.
# 凭据 store 仍失败 -> Fail-And-Exit. (从 ccc_switch L66 / settings_local L65 搬运).
# -CccExe: 调用方透传它的 $CccExe 形参 (settings_local / ccc_switch 的 param() 里来的);
#          留空走 Get-CccExe 的 <PSScriptRoot>\..\dst\ccc.exe 探测 + Require(默认不存在就退出).
function Get-CccProfiles {
    [CmdletBinding()]
    param([string]$CccExe)

    $exe = Get-CccExe -CccExe $CccExe -Require

    $errFile = Join-Path $env:TEMP ("ccc_list_err_{0}.txt" -f [guid]::NewGuid().ToString('N'))

    try {
        $stdout = & $exe list 2> $errFile
        $rc = $LASTEXITCODE
        $errText = if (Test-Path -LiteralPath $errFile) { (Get-Content -LiteralPath $errFile -Raw -Encoding UTF8) } else { '' }

        if ($rc -ne 0 -or ($errText -match 'cred store')) {
            # 凭据缺失 -> 交互式 store, 重试一次
            Write-Host ""
            Write-Host "=============================================" -ForegroundColor Cyan
            Write-Host " creating credential file" -ForegroundColor Green
            Write-Host " enter kdbx master password to save." -ForegroundColor Green
            Write-Host "=============================================" -ForegroundColor Cyan
            Write-Host ""
            & $exe cred store
            if ($LASTEXITCODE -ne 0) {
                Fail-And-Exit $LASTEXITCODE
            }
            Write-Host ""
            Write-Host "Setup OK. Retrying ccc list..." -ForegroundColor Yellow
            $stdout = & $exe list
            $rc = $LASTEXITCODE
        }

        if ($rc -ne 0) {
            Write-Host "ccc list exit=$rc" -ForegroundColor Red
            Fail-And-Exit $rc
        }
    } finally {
        Remove-Item -LiteralPath $errFile -Force -ErrorAction SilentlyContinue
    }

    $names = New-Object System.Collections.ArrayList
    foreach ($line in ($stdout -split "`n")) {
        # ccc list 的行是 "[kdbx] <name> <model> <price> <context>";
        # 只取第一个 token 当 profile 名(后面几列是展示元数据)。
        if ($line -match '^\[kdbx\]\s+(\S+)') {
            [void]$names.Add($Matches[1])
        }
    }
    return ,$names
}

# ---------- ccc list --tsv -> profile 元数据 (name -> @{Model;Price;Context}) ----------
# 跑 `ccc list --tsv`, 每行 "name<TAB>model<TAB>price<TAB>context"(缺值留空),
# 按 tab 切成 name 键的哈希表. 供 ccc_switch 菜单在名字后展示 【context】【price】.
# 与 Get-CccProfiles 的关系: 那个只取名字(走默认表格 + [kdbx] 前缀), 这个取整行元数据
# (走 --tsv, 无 [kdbx] 前缀、不做空格对齐). price 里带空格("0.05, 0.5"), 按空格切会碎,
# 所以 --tsv 用 tab 分隔, 这里按 tab 精确切.
# 降级: 退出码非 0 / 无输出 -> 返回空哈希表(菜单只显示名字, 不阻断选模型).
# 凭据缺失的交互式 cred store 由 Get-CccProfiles 先行触发, 本函数不重复.
function Get-CccProfileMeta {
    [CmdletBinding()]
    param([string]$CccExe)

    $exe = Get-CccExe -CccExe $CccExe -Require
    $meta = @{}

    $errFile = Join-Path $env:TEMP ("ccc_meta_err_{0}.txt" -f [guid]::NewGuid().ToString('N'))
    try {
        $stdout = & $exe list --tsv 2> $errFile
        $rc = $LASTEXITCODE
        if ($rc -ne 0) { return $meta }
    } finally {
        Remove-Item -LiteralPath $errFile -Force -ErrorAction SilentlyContinue
    }

    foreach ($line in ($stdout -split "`n")) {
        if ($line -notmatch '\S') { continue }
        $parts = $line -split "`t"
        if ($parts.Count -lt 1 -or $parts[0] -eq '') { continue }
        $name    = $parts[0]
        $model   = if ($parts.Count -ge 2) { $parts[1] } else { '' }
        $price   = if ($parts.Count -ge 3) { $parts[2] } else { '' }
        $context = if ($parts.Count -ge 4) { $parts[3] } else { '' }
        $meta[$name] = @{ Model = $model; Price = $price; Context = $context }
    }
    return $meta
}

# ---------- .env 路径 ----------
# -CccExe 存在 -> exe 所在目录/.env (Go 端 loadDotEnv 的 <exeDir>/.env 同义).
# 否则 -> <PSScriptRoot>\..\dst\.env (= 仓库 dst/.env). 合并 manager L256 语义.
function Get-EnvFilePath {
    param([string]$CccExe)
    if ($CccExe -and (Test-Path -LiteralPath $CccExe)) {
        $dir = Split-Path -Parent (Resolve-Path -LiteralPath $CccExe).Path
        return (Join-Path $dir '.env')
    }
    return (Join-Path $PSScriptRoot '..\dst\.env')
}

# ---------- 当前 CURRENT_MODEL 值 ----------
# 容错:
#   - 跳过注释行 (^#.*CURRENT_MODEL=...)
#   - 行内出现 "...CURRENT_MODEL=aCURRENT_MODEL=b" 时, 取最后一个有效值
#     (历史 bug 曾在同一行内反复拼接, 此处兼容之)
# 算法来自 ccc_switch L130: 先剥行内残留拼接, 再判这行是否形如 "CURRENT_MODEL=xxx".
# 坑(原样保留): 不能用 'CURRENT_MODEL=([^\s]+)' 正则 —— 它从左起匹配, 会把
#   "...aCURRENT_MODEL=b" 整段(含内嵌的 CURRENT_MODEL=)吞成一个值, 得到垃圾名.
#   所以走 LastIndexOf. (ccc_switch L141-142 注释原话).
function Get-CurrentModel {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    $clean = New-Object System.Collections.Generic.List[string]
    foreach ($line in (Get-Content -LiteralPath $Path)) {
        if ($line -match '^CURRENT_MODEL\s*=\s*(.+?)\s*$') {
            $clean.Add($Matches[1])
            continue
        }
        $idx = $line.LastIndexOf('CURRENT_MODEL=')
        if ($idx -ge 0) {
            $clean.Add($line.Substring($idx + 'CURRENT_MODEL='.Length).Trim())
        }
    }
    if ($clean.Count -gt 0) { return $clean[$clean.Count - 1] }
    return $null
}

# ---------- 写 CURRENT_MODEL=<Profile> 到 env ----------
# 等同 ccc.exe T3 行为:
#   1) 删除所有独占一行的 CURRENT_MODEL=xxx
#   2) 把每行内残留拼接的 CURRENT_MODEL=xxx 全部剥离 (只去点, 不破坏同一行其它键)
#   3) 干净的 CURRENT_MODEL=<Profile> 单独占一行, 写在文件末尾, 以 \n 收尾
# 算法来自 ccc_switch L158: 用 [regex]::Replace 全局替换一次性吃净所有残留
# (包括紧邻的粘接符 '#'). 老的 LastIndexOf 单趟只剥最后一处会留下
# "...#CURRENT_MODEL=旧值" 残根, 实测 CCC_KDBX_PATH 仍带尾时 kdbx 路径查不到.
# 例: "apikey.kdbx#CURRENT_MODEL=aCURRENT_MODEL=b" -> "apikey.kdbx"
# 例: "flashCURRENT_MODEL=580ai"               -> "flash"
function Set-CurrentModel {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Profile)

    $clean = New-Object System.Collections.Generic.List[string]
    if (Test-Path -LiteralPath $Path) {
        foreach ($line in (Get-Content -LiteralPath $Path)) {
            # 1) 整行就是 CURRENT_MODEL=... -> 丢弃
            if ($line -match '^\s*CURRENT_MODEL\s*=') { continue }

            # 2) 行内残留拼接 ("xCURRENT_MODEL=y" / "path#CURRENT_MODEL=y"):
            #    "[^\s#]*" 贪婪吃到行尾/空白/'#' 为止, 一次清完所有残留.
            $stripped = [regex]::Replace($line, '#?CURRENT_MODEL=[^\s#]*', '')
            if ($stripped -ne $line) { $line = $stripped.TrimEnd() }
            $clean.Add($line)
        }
    }

    $clean.Add("CURRENT_MODEL=$Profile")

    $parent = Split-Path -Parent $Path
    if ($parent -and -not (Test-Path -LiteralPath $parent)) {
        New-Item -Path $parent -ItemType Directory -Force | Out-Null
    }
    $body = ($clean -join "`n") + "`n"
    [System.IO.File]::WriteAllText($Path, $body, [System.Text.UTF8Encoding]::new($false))
}

# ---------- .cred 凭据信封路径 ----------
# 3in1: 运行时凭据合并为单个 <ccc.exe 所在目录>/.cred 信封
# (user-scope DPAPI 密封的 JSON: 主密码 + 公共机闸位 + 厂商快照; Go 端 credPath 同源).
# 旧格式 (.kdbx_master.cred + .public_machine.flag) 只在惰性迁移时被读一次,
# 迁移成功即删 —— 清理由 `ccc cred delete` 连带完成, 本函数只定位新信封.
# -EnvPath 保留仅作签名兼容 (Get-CredTag 调用点不需改), 不再参与路径解析.
# -CccExe 存在 -> exe 所在目录
# 否则       -> <PSScriptRoot>\..\dst\ (= 仓库 dst/ 兜底, 与 Get-CccExe 的 dst/ 探测对齐)
# 末尾 GetFullPath 归一化 (去掉 .. 段) —— 与 Get-CccExe 的 Resolve-Path 输出一致,
# 避免 `scripts\..\dst\...` 这种未规范化形式泄漏给调用方.
# 文件暂不存在也行 —— 路径本身合法, 调用方自行 Test-Path 判定标签.
function Get-KdbxCredPath {
    param(
        [string]$CccExe,
        [string]$EnvPath
    )
    $exeDir = $null
    if ($CccExe -and (Test-Path -LiteralPath $CccExe)) {
        $exeDir = Split-Path -Parent (Resolve-Path -LiteralPath $CccExe).Path
    } else {
        $exeDir = Join-Path $PSScriptRoot '..\dst'   # 兜底: 仓库 dst/ (对齐 Get-CccExe 默认探测)
    }
    $credPath = Join-Path $exeDir '.cred'
    return [System.IO.Path]::GetFullPath($credPath)
}

# ---------- menu 4 用 [Cred: 就绪/不存在/未知错误] 标签 ----------
# 复用 Get-EnvFilePath + Get-KdbxCredPath, 不重复 CCC_KDBX_PATH 解析.
# 返回 '就绪' / '不存在' / '未知错误' (manager L403 原样搬运).
function Get-CredTag {
    $exe = Get-CccExe
    $envPath = Get-EnvFilePath -CccExe $exe
    $credPath = Get-KdbxCredPath -CccExe $exe -EnvPath $envPath
    if (-not $credPath) { return '未知错误' }
    if (Test-Path -LiteralPath $credPath) { return '就绪' }
    return '不存在'
}

# ---------- apikey.kdbx 路径 (与 Go 端 kdbxPath() 同源) ----------
# 算法与 src/kdbx.go:kdbxPath() 完全对齐:
#   1) 从 <exeDir>/.env 读 CCC_KDBX_PATH (= Go 端 loadDotEnv 注入 os.Getenv).
#   2) 若相对路径 -> 锚定 <exeDir> (而非进程 CWD), 与 Go 端 filepath.Join(exeDir(), p) 同义.
# 容错: 文件不存在 / 没值 -> 返回 $null (菜单 6 标签据此判"未知错误").
# 行内残留拼接 (历史 bug) 此处不动: 同 <env>/.env 一般是干净的 KV 一行一条, 当前无痕.
function Get-KdbxPath {
    $exe = Get-CccExe
    $envPath = Get-EnvFilePath -CccExe $exe

    $raw = $null
    if (Test-Path -LiteralPath $envPath) {
        foreach ($line in (Get-Content -LiteralPath $envPath)) {
            # 跳过注释 / 空行
            if ($line -match '^\s*#') { continue }
            if ($line -match '^\s*$') { continue }
            if ($line -match '^\s*CCC_KDBX_PATH\s*=\s*(.+?)\s*$') {
                $raw = $Matches[1]
                continue
            }
            $idx = $line.LastIndexOf('CCC_KDBX_PATH=')
            if ($idx -ge 0) {
                $raw = $line.Substring($idx + 'CCC_KDBX_PATH='.Length).Trim()
            }
        }
    }
    if (-not $raw) { return $null }

    # 相对 -> 锚 exeDir (与 Go 端 filepath.IsAbs 判断 + Join(exeDir, p) 同义)
    if (-not [System.IO.Path]::IsPathRooted($raw)) {
        $exeDir = $null
        if ($exe -and (Test-Path -LiteralPath $exe)) {
            $exeDir = Split-Path -Parent (Resolve-Path -LiteralPath $exe).Path
        } else {
            $exeDir = Join-Path $PSScriptRoot '..\dst'   # 兜底: 仓库 dst/
        }
        $raw = Join-Path $exeDir $raw
    }
    return [System.IO.Path]::GetFullPath($raw)
}

# ---------- menu 6 用 [Kdbx: 就绪/不存在/未知错误] 标签 ----------
# 复用 Get-KdbxPath: 路径解不出 -> 未知错误;解出 -> 按 Test-Path 标就绪/不存在.
# 与 Get-CredTag 同形, 后续若再加 tag 类菜单可抽公共 helper, 现两处不动免 diff 扩散.
function Get-KdbxTag {
    $kdbxPath = Get-KdbxPath
    if (-not $kdbxPath) { return '未知错误' }
    if (Test-Path -LiteralPath $kdbxPath) { return '就绪' }
    return '不存在'
}

# ---------- 递归深合并 JSON ----------
# 先复制 target 全部键值, 再用 source 强制覆盖同名键 (同层递归合并对象).
# settings_local L191 原样搬运.
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

# ---------- 写 settings.json ----------
# 默认: 直接把 SourceJson 转 JSON 覆盖写到 Path (mkdir -p 同 manager 旧版).
# -Merge: 若 Path 存在, 读出 -> 与 SourceJson 深合并 -> 写盘 (整覆盖, 其它键原样保留).
# 写盘: ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 -NoNewline, 与
# manager L347 / settings_local L284 现行为完全一致. ccc_settings_lan 的非 -Merge
# 路径用法 (远端原子写) 不在本函数职责内, 不动.
function Write-SettingsJson {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]$SourceJson,
        [Parameter(Mandatory)][string]$Path,
        [switch]$Merge
    )

    $finalJson = $SourceJson
    if ($Merge -and (Test-Path -LiteralPath $Path)) {
        try {
            $targetJson = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
            $finalJson = Merge-JsonObject -Source $SourceJson -Target $targetJson
        } catch {
            $finalJson = $SourceJson   # 读旧文件失败 -> 退化为覆盖
        }
    }

    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path -LiteralPath $dir)) {
        New-Item -Path $dir -ItemType Directory -Force | Out-Null
    }
    $finalJson | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $Path -Encoding UTF8 -NoNewline
}

# ============== 菜单 UI 排版 (switch / settings_local / settings_lan 共用) ==============
# 统一观感: 56 列分隔线 + 头部信息块(label/value) + profile 列表(尾注【ctx】【price】
# 按列对齐) + 0. exit。原来这套排版只在 ccc_switch.ps1 里, 现三脚本共用一份实现。

# ---------- 分隔线 ----------
# 三个菜单原本各自写死不同长度 (56/42/53), 统一到这里。
function Get-MenuRule { return ('=' * 56) }

# ---------- 显示宽度 (控制台列数) ----------
# 【】是东亚全角字符, 占 2 列; ASCII 占 1 列。
# 用字符数算对齐会错位(【1M】与【131K】字符数差 2, 显示宽度也差 2,
# 但【】【】这类组合必须按列宽算, 否则含全角的尾注会偏)。
function Get-DisplayWidth {
    param([string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return 0 }
    $w = 0
    foreach ($ch in $Text.ToCharArray()) {
        $c = [int][char]$ch
        if (($c -ge 0x1100 -and $c -le 0x115F) -or
            ($c -ge 0x2E80 -and $c -le 0xA4CF) -or
            ($c -ge 0xAC00 -and $c -le 0xD7A3) -or
            ($c -ge 0xF900 -and $c -le 0xFAFF) -or
            ($c -ge 0xFE30 -and $c -le 0xFE6F) -or
            ($c -ge 0xFF00 -and $c -le 0xFF60) -or
            ($c -ge 0xFFE0 -and $c -le 0xFFE6)) { $w += 2 } else { $w += 1 }
    }
    return $w
}

# ---------- 菜单行: 名字 + 尾注【ctx】【price】 ----------
# 尾注固定从第 45 列起(名字字段 PadRight 到 42 + 两个空格), 所有行的第一个【对齐。
# 名字太长(前缀+名字 > 42 列)时尾注只右移、不截断。
# 第二个【(price 的【)也要求对齐: ctx 标签宽度不一(【1M】/【131K】/【262K】),
# 所以先算出全部行的 ctx 标签最大显示宽度, 短的补空格,
# 第二个【便统一落在「较靠后的行」那一列。
# 纯 ctx 无 price 的行、以及非 openrouter 无尾注的行, 同样按此列位补齐。
function Get-MenuRows {
    param($Entries, [hashtable]$Meta)

    # 1) 每行的 ctx 标签 + 其最大显示宽度
    $ctxTags = @{}
    $maxCtxWidth = 0
    for ($i = 0; $i -lt $Entries.Count; $i++) {
        $name = $Entries[$i]
        $ctx = ''
        if ($Meta -and $Meta.ContainsKey($name)) {
            $ctx = $Meta[$name].Context
            # context 只取 '/' 前的部分(1M/944K → 1M)
            if ($ctx -match '^([^/]+)/') { $ctx = $Matches[1] }
        }
        $tag = ''
        if ($ctx) { $tag = "【$ctx】" }
        $ctxTags[$name] = $tag
        $w = Get-DisplayWidth $tag
        if ($w -gt $maxCtxWidth) { $maxCtxWidth = $w }
    }

    # 2) 逐行拼装
    $rows = New-Object System.Collections.ArrayList
    for ($i = 0; $i -lt $Entries.Count; $i++) {
        $name = $Entries[$i]
        $prefix = "  {0}. " -f ($i + 1)
        # 尾注统一从第 45 列开始(名字字段 PadRight 到 42 + 2 空格);名字超长时仅右移、不截断
        $nameField = ($prefix + $name).PadRight(42)

        $ctxTag = $ctxTags[$name]
        $priceTag = ''
        if ($Meta -and $Meta.ContainsKey($name)) {
            $price = $Meta[$name].Price
            if ($price) { $priceTag = "【$price】" }
        }

        $tail = ''
        if ($ctxTag -or $priceTag) {
            $pad = ' ' * [Math]::Max(0, $maxCtxWidth - (Get-DisplayWidth $ctxTag))
            $tail = "  $ctxTag$pad$priceTag"
        } else {
            $nameField = $nameField.TrimEnd()   # 无尾注的行不留行尾空格
        }

        [void]$rows.Add(@{ Name = $name; Line = $nameField; Tail = $tail })
    }
    return ,$rows
}

# ---------- 头部信息块 ----------
# 空行 + 标题(Cyan) + 分隔线 + 逐行 "  label    value"(DarkGray) + 可选提示行(Yellow) + 分隔线。
# -Info 用 [ordered]@{} 保证插入顺序; 键不带冒号, 本函数补。
# -Notice 非空时在信息块下方单起一行黄色提示(如 lan 的 DRY-RUN)。
function Write-MenuHeader {
    param(
        [Parameter(Mandatory)][string]$Title,
        [Parameter(Mandatory)]$Info,
        [string]$Notice = ''
    )
    Write-Host ""
    Write-Host $Title -ForegroundColor Cyan
    Write-Host (Get-MenuRule) -ForegroundColor Cyan
    foreach ($k in $Info.Keys) {
        Write-Host ("  {0,-10}{1}" -f ($k + ':'), $Info[$k]) -ForegroundColor DarkGray
    }
    if ($Notice -ne '') {
        Write-Host ("  {0,-10}{1}" -f ('mode:'), $Notice) -ForegroundColor Yellow
    }
    Write-Host (Get-MenuRule) -ForegroundColor Cyan
}

# ---------- profile 列表区 ----------
# 名字 + 尾注(DarkGray) + [current](Green), 行尾补色后由 Write-Host 换行。
function Write-MenuRows {
    param($Entries, [string]$Current, [hashtable]$Meta)

    $rows = Get-MenuRows -Entries $Entries -Meta $Meta
    for ($i = 0; $i -lt $rows.Count; $i++) {
        $name = $rows[$i].Name
        Write-Host $rows[$i].Line -NoNewline
        if ($rows[$i].Tail) { Write-Host $rows[$i].Tail -NoNewline -ForegroundColor DarkGray }
        if ($name -eq $Current) {
            Write-Host "  [current]" -ForegroundColor Green -NoNewline
        }
        Write-Host ""
    }
}

# ---------- 底部: 分隔线 + 0. exit ----------
function Write-MenuFooter {
    Write-Host (Get-MenuRule) -ForegroundColor Cyan
    Write-Host "  0. Exit"
    Write-Host ""
}

# ---------- 读取选择 ----------
# 返回选中的 profile 名; 0 / 非法输入返回 $null(调用方一律当退出处理, 三脚本语义一致)。
function Read-MenuChoice {
    param($Entries)
    $choice = Read-Host "Select profile (1-$($Entries.Count)) or 0 to exit"
    if ($choice -eq "0") { return $null }
    if ($choice -match "^\d+$") {
        $idx = [int]$choice
        if ($idx -ge 1 -and $idx -le $Entries.Count) { return $Entries[$idx - 1] }
    }
    Write-Host "Invalid choice" -ForegroundColor Red
    return $null
}
