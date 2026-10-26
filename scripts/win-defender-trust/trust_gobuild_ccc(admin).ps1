#Requires -RunAsAdministrator
# 把 ccc 构建输出目录与 Go 编译临时目录加入 Defender 排除。
# 必须以管理员运行(普通权限 Add-MpPreference 静默无效)。

$dst = Resolve-Path (Join-Path $PSScriptRoot '..')
Add-MpPreference -ExclusionPath $dst.Path
Add-MpPreference -ExclusionPath "$env:TEMP\go-build*"

Write-Host "已加入排除:"
Write-Host "  $($dst.Path)"
Write-Host "  $env:TEMP\go-build*"
Get-MpPreference | Select-Object -ExpandProperty ExclusionPath
Read-Host "`n按回车退出"
