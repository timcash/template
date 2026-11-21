#!/usr/bin/env pwsh
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$bin = Join-Path $root "bin\template.exe"

if (Test-Path $bin) {
    & $bin @args
    exit $LASTEXITCODE
}

Push-Location $root
try {
    & go run . @args
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
