@echo off
REM Copyright 2026 Regnant
REM SPDX-License-Identifier: Apache-2.0
REM
REM Stop a KNOTT started with start.bat. In-flight runs resume on the next start.
taskkill /IM knott.exe >nul 2>&1 && echo KNOTT stopping || echo KNOTT is not running
