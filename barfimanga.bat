@echo off
setlocal enabledelayedexpansion

:: Caminho do projeto no WSL
set "WSL_PROJECT_DIR=/home/jhonnatta/projetos/BarfiManga"
set "WSL_BIN=./bin/barfimanga"

echo ========================================
echo             BARFIMANGA (WSL)
echo ========================================

:: Verifica se o usuario arrastou uma pasta para o .bat
set "WIN_PATH=%~1"

if "%WIN_PATH%"=="" (
    echo [i] Iniciando modo interativo...
    wsl bash -c "cd '!WSL_PROJECT_DIR!' && !WSL_BIN!"
) else (
    echo [i] Pasta detectada: %WIN_PATH%
    echo [i] Convertendo caminho para WSL...
    
    :: Usa o wslpath para converter o caminho do Windows (C:\...) para Linux (/mnt/c/...)
    for /f "usebackq tokens=*" %%a in (`wsl wslpath '%WIN_PATH%'`) do set "LINUX_PATH=%%a"
    
    echo [i] Iniciando upload de: !LINUX_PATH!
    wsl bash -c "cd '!WSL_PROJECT_DIR!' && !WSL_BIN! --dir '!LINUX_PATH!'"
)

echo.
echo [OK] Processo finalizado.
pause
