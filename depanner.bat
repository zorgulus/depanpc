@echo off
setlocal

set "ROOT=%~dp0"
set "CLIENT=%ROOT%client"
set "VENV=%CLIENT%\.venv"

if not exist "%VENV%\Scripts\python.exe" (
    echo Premiere utilisation : preparation de l'environnement client...
    python -m venv "%VENV%"
    "%VENV%\Scripts\python.exe" -m pip install --quiet --upgrade pip
    "%VENV%\Scripts\python.exe" -m pip install --quiet -r "%CLIENT%\requirements.txt"
)

cd /d "%ROOT%"
echo Environnement pret. Lancement de Claude Code...
claude
