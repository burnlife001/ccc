# build.ps1 — build dst/ccc.exe from src/(不安装,构建即用)
#
# 用法：在仓库根目录执行 `pwsh ./build.ps1`
# 行为：
#   go build -ldflags "-s -w -X main.version=0.1.0" -o dst/ccc.exe ./src
#   剥离符号表、压小体积、注入语义化版本号（覆盖 paths.go 里的默认 0.1.0-dev）
#
# 单 exe 约 2.5 MB,直接运行 dst/ccc.exe 即可,不复制、不改 PATH。
# 该脚本不引入依赖、不做签名,符合 docs/go架构规范.md §7 的 std-only 原则。

$ErrorActionPreference = 'Stop'

$RepoRoot = $PSScriptRoot
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

    Write-Host "[ok] built $outExe version $Version"
} finally {
    Pop-Location
}
