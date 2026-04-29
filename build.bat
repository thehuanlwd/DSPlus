@echo off
set CGO_ENABLED=1
echo Building DSPlus...
go build -ldflags="-H windowsgui -s -w" -o DSPlus.exe .
if %ERRORLEVEL% == 0 (
  echo Build successful: DSPlus.exe
  dir DSPlus.exe
) else (
  echo Build FAILED.
  echo.
  echo If CGO/webview failed, try building without webview:
  echo   set CGO_ENABLED=0
  echo   go build -o DSPlus.exe .
)
pause
