@echo off
setlocal enabledelayedexpansion

:: Servidor remoto (antes era WSL local, agora é acesso via SSH)
set "SSH_HOST=jhoorodr@192.168.0.108"
set "REMOTE_PROJECT_DIR=/home/jhoorodr/projetos/BarfiManga"
set "REMOTE_BIN=./bin/barfimanga"

:: Share SMB [homeserver] do servidor aponta para /home/jhoorodr (ver smb.conf)
set "SMB_PREFIX=//100.124.18.121/homeserver"
set "REMOTE_HOME=/home/jhoorodr"

echo ========================================
echo           BARFIMANGA (SSH)
echo ========================================

:: Verifica se o usuario arrastou uma pasta para o .bat
set "WIN_PATH=%~1"

if "%WIN_PATH%"=="" (
    echo [i] Iniciando modo interativo...
    ssh -t %SSH_HOST% "cd '%REMOTE_PROJECT_DIR%' && %REMOTE_BIN%"
) else (
    echo [i] Pasta detectada: %WIN_PATH%
    echo [i] Convertendo caminho do share SMB para o servidor...

    :: \\100.124.18.121\homeserver\Mangas\X -> /home/jhoorodr/Mangas/X
    :: Z:\Mangas\X (drive mapeado pro mesmo share) -> /home/jhoorodr/Mangas/X
    set "SLASH_PATH=%WIN_PATH:\=/%"
    set "LINUX_PATH=!SLASH_PATH:%SMB_PREFIX%=%REMOTE_HOME%!"
    set "LINUX_PATH=!LINUX_PATH:Z:=%REMOTE_HOME%!"

    if "!LINUX_PATH!"=="!SLASH_PATH!" (
        echo [!] AVISO: pasta fora do share \\100.124.18.121\homeserver ou do drive Z: - o servidor pode nao enxergar esse caminho.
    )

    echo [i] Iniciando upload de: !LINUX_PATH!
    ssh -t %SSH_HOST% "cd '%REMOTE_PROJECT_DIR%' && %REMOTE_BIN% --dir '!LINUX_PATH!'"
)

echo.
echo [OK] Processo finalizado.
pause
