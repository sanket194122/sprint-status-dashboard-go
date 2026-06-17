@echo off
title Sprint Status Dashboard
cd /d "%~dp0"

echo.
echo   Sprint Status Dashboard (Go)
echo   ============================
echo.

if defined GITHUB_TOKEN (echo   AI: Enabled) else (echo   AI: Disabled)
if defined TEAMS_WEBHOOK_URL (echo   Teams: Enabled) else (echo   Teams: Disabled)
echo.

if not exist "sprint-dashboard.exe" (
    echo   Building binary...
    go build -o sprint-dashboard.exe .
    if errorlevel 1 (
        echo.
        echo   BUILD FAILED. Install Go from https://go.dev/dl/
        pause
        exit /b 1
    )
    echo   Build complete.
    echo.
)

timeout /t 1 /nobreak >nul
start http://localhost:8501
sprint-dashboard.exe
pause
