@echo off
echo Building IIS Log Compressor...
echo.

REM Download dependencies
echo Downloading dependencies...
go mod tidy

REM Build for Windows
echo Building executable...
go build -o iis-log-compressor.exe main.go

if %ERRORLEVEL% EQU 0 (
    echo.
    echo Build successful!
    echo Executable created: iis-log-compressor.exe
    echo.
    echo To run the application:
    echo 1. Make sure config.json is in the same directory
    echo 2. Run: iis-log-compressor.exe
    echo.
) else (
    echo.
    echo Build failed!
    echo Please check for errors above.
)

pause
