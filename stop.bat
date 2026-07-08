@echo off
REM ===========================================================================
REM KNOTT - Windows Platform Shutdown
REM ===========================================================================

echo.
echo Stopping KNOTT services...
echo.

taskkill /F /IM workflow-registry.exe   >nul 2>&1
taskkill /F /IM execution-engine.exe    >nul 2>&1
taskkill /F /IM human-task-service.exe  >nul 2>&1
taskkill /F /IM agent-integration.exe   >nul 2>&1

REM Stop only the AI engine window started by start.bat (title "KNOTT-AI"),
REM not unrelated Python processes.
taskkill /F /FI "WINDOWTITLE eq KNOTT-AI*" >nul 2>&1

echo [OK] All services stopped
echo.
