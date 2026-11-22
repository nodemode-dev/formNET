@echo off
setlocal
cd /d "%~dp0"
where npm >nul 2>nul || (echo Node 22 or newer is required: https://nodejs.org/ & exit /b 1)
if exist package-lock.json goto ci
call npm install --no-audit --no-fund || exit /b 1
exit /b 0
:ci
call npm ci --no-audit --no-fund || exit /b 1
exit /b 0
