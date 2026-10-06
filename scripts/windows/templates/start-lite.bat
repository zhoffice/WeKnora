@echo off
rem WeKnora Lite launcher: loads .env.lite into the environment, then starts the server.
setlocal EnableExtensions
cd /d "%~dp0"

if not exist ".env.lite" (
    copy /y ".env.lite.example" ".env.lite" >nul
    echo Created .env.lite from .env.lite.example. Edit it to change models or ports.
)

rem KEY=VALUE per line; lines starting with # are comments.
for /f "usebackq eol=# tokens=1,* delims==" %%A in (".env.lite") do (
    set "%%A=%%B"
)

echo WeKnora Lite is starting: http://localhost:8080
"%~dp0WeKnora-lite.exe"
set "EXIT_CODE=%ERRORLEVEL%"
if not "%EXIT_CODE%"=="0" pause
exit /b %EXIT_CODE%
