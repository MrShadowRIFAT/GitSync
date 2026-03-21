@echo off
echo Installing dependencies for build...
pip install -r requirements.txt

echo.
echo Building GitSync...
rem Use --add-data "ui;ui" to bundle the frontend dashboard correctly inside the executable
pyinstaller --onefile --noconsole --name GitSync --icon "ui\assets\logo.ico" --add-data "ui;ui" --add-data "backend\secrets.json;backend" main.py

echo.
echo Build complete! Your file is located at:
echo dist\GitSync.exe
pause
