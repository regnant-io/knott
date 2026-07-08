@echo off
REM ═══════════════════════════════════════════════════════════════════════════
REM KNOTT — Windows Platform Launcher
REM   Usage:  start.bat            (build if needed, then run)
REM           start.bat rebuild    (force a clean rebuild of all Go services)
REM ═══════════════════════════════════════════════════════════════════════════

setlocal enabledelayedexpansion

echo.
echo ==============================================================
echo    KNOTT - Sovereign Workflow Platform
echo ==============================================================
echo.

REM --- Load environment ------------------------------------------------------
if exist .env (
    echo [*] Loading .env configuration
    for /f "usebackq eol=# tokens=1,* delims==" %%a in (".env") do (
        set "%%a=%%b"
    )
) else (
    echo [!] No .env file found - copying from .env.example
    copy .env.example .env >nul
    echo     Edit .env to configure your AI provider, then run start.bat again
    pause
    exit /b 1
)

if not exist data mkdir data
if not exist logs mkdir logs
if not exist bin  mkdir bin
echo [*] Directories ready
echo.

REM --- Optional forced rebuild -----------------------------------------------
if /i "%1"=="rebuild" (
    echo [*] Rebuild requested - removing existing binaries
    del /q bin\*.exe >nul 2>&1
)

REM --- Build Go services -----------------------------------------------------
where go >nul 2>&1
if %errorlevel% equ 0 (
    call :build workflow-registry
    call :build execution-engine
    call :build human-task-service
    call :build agent-integration
) else (
    echo [!] Go not found - assuming pre-built binaries exist in bin\
)
echo.

REM --- Check Python (AI engine has NO pip dependencies) ----------------------
where python >nul 2>&1
if %errorlevel% equ 0 (
    echo [*] Python found - AI Decision Engine ready ^(stdlib only^)
) else (
    echo [!] Python not found - AI Decision Engine will not start
)
echo.

echo ==============================================================
echo    STARTING SERVICES
echo ==============================================================
echo.

REM --- Stop any previous instances -------------------------------------------
taskkill /F /IM workflow-registry.exe   >nul 2>&1
taskkill /F /IM execution-engine.exe    >nul 2>&1
taskkill /F /IM human-task-service.exe  >nul 2>&1
taskkill /F /IM agent-integration.exe   >nul 2>&1
timeout /t 2 /nobreak >nul

REM Default ports / discovery (overridable from .env)
if "%REGISTRY_PORT%"==""        set REGISTRY_PORT=8001
if "%ENGINE_PORT%"==""          set ENGINE_PORT=8002
if "%AI_PORT%"==""              set AI_PORT=8003
if "%TASK_PORT%"==""            set TASK_PORT=8004
if "%AGENT_PORT%"==""           set AGENT_PORT=8005
if "%REGISTRY_URL%"==""         set REGISTRY_URL=http://localhost:%REGISTRY_PORT%
if "%AI_DECISION_URL%"==""      set AI_DECISION_URL=http://localhost:%AI_PORT%
if "%HUMAN_TASK_URL%"==""       set HUMAN_TASK_URL=http://localhost:%TASK_PORT%
if "%AGENT_URL%"==""            set AGENT_URL=http://localhost:%AGENT_PORT%
if "%EXECUTION_ENGINE_URL%"=="" set EXECUTION_ENGINE_URL=http://localhost:%ENGINE_PORT%
set FRONTEND_PATH=.\apps\designer\dist

REM --- Port preflight ---------------------------------------------------------
REM Detect ports already held by another process (commonly a leftover Docker
REM stack). Hitting this silently was a real debugging trap, so we warn loudly.
set PORT_CONFLICT=0
for %%P in (%REGISTRY_PORT% %ENGINE_PORT% %AI_PORT% %TASK_PORT% %AGENT_PORT%) do (
    netstat -ano | findstr /R /C:":%%P .*LISTENING" >nul 2>&1 && (
        echo [!] Port %%P is already in use by another process.
        set PORT_CONFLICT=1
    )
)
if "%PORT_CONFLICT%"=="1" (
    echo.
    echo [!] One or more KNOTT ports are occupied. Common cause: a previous
    echo     Docker stack is still running. Check with:  docker ps
    echo     and stop it with:                            docker compose down
    echo     Or change the *_PORT values in .env to free ports.
    echo.
    choice /C YN /M "Continue starting anyway"
    if errorlevel 2 (
        echo Aborted. Free the ports and re-run start.bat.
        endlocal
        exit /b 1
    )
)

REM --- Workflow Registry ------------------------------------------------------
set REGISTRY_DB=.\data\workflows.db
start "KW-Registry" /MIN bin\workflow-registry.exe >> logs\workflow-registry.log 2>&1
echo [*] Workflow Registry      -^> http://localhost:%REGISTRY_PORT%

REM --- AI Decision Engine -----------------------------------------------------
where python >nul 2>&1
if %errorlevel% equ 0 (
    set PORT=%AI_PORT%
    if "%AI_CONFIG_PATH%"=="" set AI_CONFIG_PATH=.\data\ai-config.json
    start "KNOTT-AI" /MIN python services\ai-decision-engine\main.py >> logs\ai-decision-engine.log 2>&1
    echo [*] AI Decision Engine     -^> http://localhost:%AI_PORT%
    set PORT=
) else (
    echo [!] AI Decision Engine     -^> SKIPPED ^(Python not available^)
)

REM --- Human Task Service -----------------------------------------------------
set TASK_DB=.\data\tasks.db
start "KW-Tasks" /MIN bin\human-task-service.exe >> logs\human-task-service.log 2>&1
echo [*] Human Task Service     -^> http://localhost:%TASK_PORT%

REM --- Agent Integration ------------------------------------------------------
set AGENT_DB=.\data\agents.db
start "KW-Agents" /MIN bin\agent-integration.exe >> logs\agent-integration.log 2>&1
echo [*] Agent Integration      -^> http://localhost:%AGENT_PORT%

REM --- Execution Engine (serves the UI + proxies the others) ------------------
set ENGINE_DB=.\data\runs.db
start "KW-Engine" /MIN bin\execution-engine.exe >> logs\execution-engine.log 2>&1
echo [*] Execution Engine       -^> http://localhost:%ENGINE_PORT%

echo.
echo [*] Waiting for services to start...
timeout /t 4 /nobreak >nul

echo.
echo ==============================================================
echo    HEALTH STATUS
echo ==============================================================
echo.
curl -s http://localhost:%REGISTRY_PORT%/api/v1/health >nul 2>&1 && echo [OK]  Workflow Registry  || echo [DOWN] Workflow Registry
curl -s http://localhost:%ENGINE_PORT%/api/v1/health   >nul 2>&1 && echo [OK]  Execution Engine   || echo [DOWN] Execution Engine
curl -s http://localhost:%AI_PORT%/internal/v1/health  >nul 2>&1 && echo [OK]  AI Decision Engine || echo [DOWN] AI Decision Engine
curl -s http://localhost:%TASK_PORT%/api/v1/health     >nul 2>&1 && echo [OK]  Human Task Service || echo [DOWN] Human Task Service
curl -s http://localhost:%AGENT_PORT%/api/v1/health    >nul 2>&1 && echo [OK]  Agent Integration  || echo [DOWN] Agent Integration

echo.
echo ==============================================================
echo.
echo    KNOTT IS READY
echo.
echo    Open:  http://localhost:%ENGINE_PORT%
echo    Logs:  .\logs\     Data:  .\data\     Stop:  stop.bat
echo.
echo ==============================================================
echo.
endlocal
exit /b 0

REM ─── build helper: :build <service-name> ───────────────────────────────────
:build
if not exist bin\%1.exe (
    echo [build] %1 ...
    pushd services\%1
    go build -o ..\..\bin\%1.exe .
    if errorlevel 1 (
        echo [ERROR] build failed for %1
    ) else (
        echo [build] %1 OK
    )
    popd
)
exit /b 0
