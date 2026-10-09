# Search for 'bin' directories under script root and add selected one to User PATH
# Also link/remove exe(s) found under script root as symlinks in ~/local/bin

$ErrorActionPreference = 'Stop'

function Show-Menu {
    Clear-Host
    Write-Host "======================================" -ForegroundColor Cyan
    Write-Host "         Add to PATH" -ForegroundColor Cyan
    Write-Host "======================================" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  [1] Add bin directory to User PATH"
    Write-Host "  [2] Add current directory to User PATH"
    Write-Host "  [3] Link exe(s) to ~/local/bin"
    Write-Host "  [4] Remove exe link(s) from ~/local/bin"
    Write-Host "  [0] Exit"
    Write-Host ""
}

# Resolve the ~/local/bin directory (created on demand by the link item)
function Get-LocalBinDir {
    return (Join-Path $env:USERPROFILE 'local\bin')
}

# Scan for exe files under a root, return full paths (depth-limited to skip deep trees)
function Get-ExeCandidates {
    param([string]$Root)
    return @(Get-ChildItem -Path $Root -File -Filter "*.exe" -Recurse -Depth 2 -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty FullName)
}

# Show a numbered list, return the chosen full path (auto-select when only one item)
function Select-FromList {
    param([string[]]$Items, [string]$Prompt)

    if ($Items.Count -eq 1) {
        Write-Host "Found: $($Items[0])" -ForegroundColor Cyan
        return $Items[0]
    }

    Write-Host "`nMultiple candidates found:" -ForegroundColor Cyan
    for ($i = 0; $i -lt $Items.Count; $i++) {
        Write-Host "  [$i] $($Items[$i])"
    }
    Write-Host ""
    $sel = Read-Host $Prompt
    if ($sel -notmatch '^\d+$' -or [int]$sel -lt 0 -or [int]$sel -ge $Items.Count) {
        Write-Host "Invalid selection: $sel" -ForegroundColor Red
        Pause
        exit 1
    }
    return $Items[[int]$sel]
}

# Append a directory to the User PATH; reports whether it was added
function Add-PathEntry {
    param([string]$Dir)

    $currentPath = [Environment]::GetEnvironmentVariable("PATH", 'User')
    $entries = @()
    if ($currentPath) {
        $entries = $currentPath -split ';' | ForEach-Object { $_.Trim() } | Where-Object { $_ }
    }

    # Case-insensitive duplicate check
    $alreadyThere = $entries | Where-Object { $_ -ieq $Dir }
    if ($alreadyThere) {
        Write-Host "`nThe path is already present in the User PATH." -ForegroundColor Yellow
        return $false
    }

    $newPath = if ($entries.Count -gt 0) {
        ($entries -join ';') + ';' + $Dir
    } else {
        $Dir
    }

    try {
        [Environment]::SetEnvironmentVariable("PATH", $newPath, 'User')
        Write-Host "`nSuccessfully added to User PATH:" -ForegroundColor Green
        Write-Host "  $Dir" -ForegroundColor Green
        Write-Host "`nNote: Restart your terminal or run the following for this session:" -ForegroundColor Yellow
        Write-Host "  `$env:PATH += `";$Dir`"" -ForegroundColor Yellow
        return $true
    } catch {
        Write-Host "`nFailed to update User PATH!" -ForegroundColor Red
        Write-Host $_.Exception.Message -ForegroundColor Red
        Pause
        exit 1
    }
}

# Create a symlink <LocalBin>\<name>.exe -> <ExePath>, overwriting an existing symlink
function New-ExeLink {
    param([string]$ExePath, [string]$LocalBin)

    $linkPath = Join-Path $LocalBin ([System.IO.Path]::GetFileName($ExePath))
    $existing = Get-Item -LiteralPath $linkPath -Force -ErrorAction SilentlyContinue

    if ($existing) {
        if ($existing.LinkType -ne 'SymbolicLink') {
            Write-Host "  SKIP $linkPath - a real file/dir is there, not a symlink" -ForegroundColor Yellow
            return $false
        }
        if ($existing.Target -eq $ExePath) {
            Write-Host "  OK   $linkPath -> $ExePath (already linked)" -ForegroundColor Green
            return $true
        }
        Remove-Item -LiteralPath $linkPath -Force
    }

    try {
        New-Item -ItemType SymbolicLink -Path $linkPath -Target $ExePath | Out-Null
        Write-Host "  NEW  $linkPath -> $ExePath" -ForegroundColor Green
        return $true
    } catch {
        Write-Host "  FAIL $linkPath - $($_.Exception.Message)" -ForegroundColor Red
        Write-Host "       Symlinks need Developer Mode or an elevated shell." -ForegroundColor Yellow
        return $false
    }
}

$searchRoot = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }

Show-Menu
$choice = Read-Host "Enter selection"

$addCurrentDir = $false
$linkMode = $false
$unlinkMode = $false

switch ($choice) {
    '0' {
        Write-Host "Exiting." -ForegroundColor Gray
        exit 0
    }
    '1' {
    }
    '2' {
        $addCurrentDir = $true
    }
    '3' {
        $linkMode = $true
    }
    '4' {
        $unlinkMode = $true
    }
    default {
        Write-Host "Invalid selection: $choice" -ForegroundColor Red
        Pause
        exit 1
    }
}

# --- Link / unlink exe symlinks in ~/local/bin ---
if ($linkMode -or $unlinkMode) {
    $localBin = Get-LocalBinDir

    if ($linkMode) {
        Write-Host "`nSearching for exe files under: $searchRoot" -ForegroundColor Cyan
        $exes = Get-ExeCandidates -Root $searchRoot

        if ($exes.Count -eq 0) {
            Write-Host "Error: No exe file found under $searchRoot" -ForegroundColor Red
            Pause
            exit 1
        }

        Write-Host "`nLink target directory: $localBin" -ForegroundColor Cyan
        if (-not (Test-Path -LiteralPath $localBin)) {
            New-Item -ItemType Directory -Path $localBin -Force | Out-Null
            Write-Host "Created directory: $localBin" -ForegroundColor Green
        }

        $done = 0
        foreach ($exe in $exes) {
            if (New-ExeLink -ExePath $exe -LocalBin $localBin) { $done++ }
        }

        Write-Host "`n$done / $($exes.Count) link(s) ready in $localBin" -ForegroundColor Green
        # A link dir that is not on PATH is useless - make it reachable
        Add-PathEntry -Dir $localBin | Out-Null
    } else {
        Write-Host "`nLink directory: $localBin" -ForegroundColor Cyan
        if (-not (Test-Path -LiteralPath $localBin)) {
            Write-Host "Error: $localBin does not exist." -ForegroundColor Red
            Pause
            exit 1
        }

        # Only offer links whose target lives under this script root
        $links = @(Get-ChildItem -Path $localBin -File -Force -ErrorAction SilentlyContinue |
            Where-Object { $_.LinkType -eq 'SymbolicLink' -and $_.Target -and "$($_.Target)".StartsWith($searchRoot, [StringComparison]::OrdinalIgnoreCase) } |
            Select-Object -ExpandProperty FullName)

        if ($links.Count -eq 0) {
            Write-Host "Nothing to remove: no symlink in $localBin points under $searchRoot" -ForegroundColor Yellow
            Pause
            exit 0
        }

        Write-Host "`nSymlinks pointing under this directory:" -ForegroundColor Cyan
        foreach ($link in $links) {
            Write-Host "  $link -> $((Get-Item -LiteralPath $link -Force).Target)" -ForegroundColor DarkGray
        }
        $ans = Read-Host "`nRemove these $($links.Count) link(s)? [y/N]"
        if ($ans -notmatch '^(y|yes)$') {
            Write-Host "Aborted, nothing removed." -ForegroundColor Gray
            Pause
            exit 0
        }

        $removed = 0
        foreach ($link in $links) {
            try {
                Remove-Item -LiteralPath $link -Force
                Write-Host "  RM   $link" -ForegroundColor Green
                $removed++
            } catch {
                Write-Host "  FAIL $link - $($_.Exception.Message)" -ForegroundColor Red
            }
        }
        Write-Host "`n$removed / $($links.Count) link(s) removed" -ForegroundColor Green
    }

    Pause
    exit 0
}

# --- Add to PATH ---
if ($addCurrentDir) {
    $targetPath = (Get-Location).Path
    Write-Host "`nCurrent directory: $targetPath" -ForegroundColor Cyan
} else {
    Write-Host "`nSearching for 'bin' directories under: $searchRoot" -ForegroundColor Cyan
    $binDirs = @(Get-ChildItem -Path $searchRoot -Directory -Filter "bin" -Recurse -ErrorAction SilentlyContinue | Select-Object -ExpandProperty FullName)

    if ($binDirs.Count -eq 0) {
        Write-Host "Error: No 'bin' directory found under $searchRoot" -ForegroundColor Red
        Pause
        exit 1
    }

    $targetPath = Select-FromList -Items $binDirs -Prompt "Enter selection number"
}

Add-PathEntry -Dir $targetPath | Out-Null
Pause
