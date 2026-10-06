@echo off
rem Run scripts/windows/package-windows.sh with Git Bash from cmd / PowerShell.
rem   scripts\windows\package.cmd check
rem   scripts\windows\package.cmd lite
setlocal

set "GIT_BASH=%ProgramFiles%\Git\bin\bash.exe"
if exist "%GIT_BASH%" goto run

rem Not in the default location: derive it from git.exe (Git\cmd\git.exe -> Git\bin\bash.exe).
rem Do not fall back to a bare bash.exe on PATH, that is usually WSL's.
for /f "delims=" %%G in ('where git 2^>nul') do (
    if exist "%%~dpG..\bin\bash.exe" (
        set "GIT_BASH=%%~dpG..\bin\bash.exe"
        goto run
    )
)
echo Git Bash not found. Install Git for Windows: https://git-scm.com/download/win
exit /b 1

:run
"%GIT_BASH%" "%~dp0package-windows.sh" %*
exit /b %ERRORLEVEL%
