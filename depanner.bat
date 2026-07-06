@echo off
setlocal

set "ROOT=%~dp0"
set "CLIENT=%ROOT%client"
set "VENV=%CLIENT%\.venv"

where python >nul 2>nul
if errorlevel 1 (
    echo ERREUR : Python n'est pas installe, ou pas accessible dans le PATH.
    echo Installe Python puis relance ce raccourci.
    pause
    exit /b 1
)

if not exist "%VENV%\Scripts\python.exe" (
    echo Premiere utilisation : preparation de l'environnement client...
    python -m venv "%VENV%"
    if errorlevel 1 (
        echo ERREUR : la creation de l'environnement virtuel Python a echoue.
        pause
        exit /b 1
    )

    "%VENV%\Scripts\python.exe" -m pip install --quiet --upgrade pip
    "%VENV%\Scripts\python.exe" -m pip install --quiet -r "%CLIENT%\requirements.txt"
    if errorlevel 1 (
        echo ERREUR : l'installation des dependances Python a echoue.
        pause
        exit /b 1
    )
)

where claude >nul 2>nul
if errorlevel 1 (
    echo ERREUR : la commande "claude" est introuvable dans le PATH.
    echo Installe Claude Code puis relance ce raccourci.
    pause
    exit /b 1
)

cd /d "%ROOT%"
echo Environnement pret. Lancement de Claude Code...
claude
