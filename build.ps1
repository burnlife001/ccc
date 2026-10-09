# build.ps1 — build dst/ccc.exe from src/ and install to D:/Programs/ccc/
#
# 用法：在仓库根目录执行 `pwsh ./build.ps1`
# 行为：
#   1) go build -ldflags "-s -w -X main.version=0.1.0" -o dst/ccc.exe ./src
#      剥离符号表、压小体积、注入语义化版本号（覆盖 paths.go 里的默认 0.1.0-dev）
#   2) 拷贝到 D:/Programs/ccc/ccc.exe
#   3) 如用户 PATH 不含该目录，自动追加到 User 级 PATH（重启 shell 生效）
#
# 该脚本不引入依赖、不做签名，符合 docs/go架构规范.md §7 的 std-only 原则。

$ErrorActionPreference = 'Stop'

$RepoRoot = $PSScriptRoot
$InstallDir = 'D:/Programs/ccc'
$ExeName = 'ccc.exe'
$OutDir = 'dst'
$Version = '0.1.0'

Push-Location $RepoRoot
try {
    # 部分环境下 LOCALAPPDATA 未设置会导致 go 找不到 build cache,提前显式指定。
    # 不存在则创建,避免污染用户环境。
    $cacheRoot = Join-Path $env:USERPROFILE 'AppData/Local/go-build'
    if (-not (Test-Path $cacheRoot)) { New-Item -ItemType Directory -Force -Path $cacheRoot | Out-Null }
    $env:GOCACHE = $cacheRoot
    $env:GOMODCACHE = (Join-Path $env:USERPROFILE 'go/pkg/mod')

    if (-not (Test-Path $OutDir)) { New-Item -ItemType Directory -Force -Path $OutDir | Out-Null }
    $outExe = Join-Path $OutDir $ExeName
    Write-Host "[build] go build -ldflags -s -w -X main.version=$Version -o $outExe ./src"
    go build -ldflags "-s -w -X main.version=$Version" -o $outExe ./src
    if ($LASTEXITCODE -ne 0) { throw "go build failed (exit $LASTEXITCODE)" }

    if (-not (Test-Path $InstallDir)) {
        Write-Host "[install] creating $InstallDir"
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    }
    Copy-Item -Force $outExe (Join-Path $InstallDir $ExeName)
    Write-Host "[install] copied to $InstallDir/$ExeName"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$InstallDir*") {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$InstallDir", 'User')
        Write-Host "[path] added $InstallDir to user PATH (restart shell to take effect)"
    } else {
        Write-Host "[path] $InstallDir already in user PATH"
    }

    Write-Host "[ok] built and installed version $Version"
} finally {
    Pop-Location
}
