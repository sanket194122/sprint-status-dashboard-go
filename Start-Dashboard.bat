@echo off
title Sprint Status Dashboard
cd /d "%~dp0"

echo.
echo   Sprint Status Dashboard
echo   =======================
echo.

if defined AWS_ACCESS_KEY_ID (echo   AI: Bedrock) else if defined GITHUB_TOKEN (echo   AI: GitHub Models) else if defined GH_MODELS_TOKEN (echo   AI: GitHub Models) else (echo   AI: Disabled)
if defined TEAMS_WEBHOOK_URL (echo   Teams: Enabled) else (echo   Teams: Disabled)
echo.

if not exist "sprint-dashboard.exe" (
    echo   Building...
    go build -o sprint-dashboard.exe .
    if errorlevel 1 (echo BUILD FAILED & pause & exit /b 1)
    echo   Build complete.
    echo.
)

timeout /t 1 /nobreak >/dev/null
start http://localhost:8501
sprint-dashboard.exe
pause
