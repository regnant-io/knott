@echo off
REM Copyright 2026 Regnant
REM SPDX-License-Identifier: Apache-2.0
REM
REM Build and run KNOTT from a checkout: the console, then the single knott
REM binary that hosts every service. Extra arguments go to "knott serve".
setlocal
cd /d "%~dp0"

if not exist apps\designer\dist\index.html (
  echo Building the console...
  call npm --prefix apps\designer ci --no-audit --no-fund || exit /b 1
  call npm --prefix apps\designer run build || exit /b 1
)
if exist internal\ui\dist\assets rmdir /s /q internal\ui\dist\assets
xcopy /e /y /q apps\designer\dist\* internal\ui\dist\ >nul

echo Building knott...
go build -o bin\knott.exe .\cmd\knott || exit /b 1
bin\knott.exe serve --open %*
