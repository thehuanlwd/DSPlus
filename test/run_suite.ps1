# 1. Stop existing DSPlus process to release file lock and port
Write-Host "[1/5] Stopping existing DSPlus process..." -ForegroundColor Yellow
$processes = Get-Process -Name "DSPlus" -ErrorAction SilentlyContinue
if ($processes) {
    $processes | Stop-Process -Force
    Start-Sleep -Milliseconds 500
    Write-Host "DSPlus process terminated successfully." -ForegroundColor Green
} else {
    Write-Host "No active DSPlus process found." -ForegroundColor Gray
}

# 2. Build project
Write-Host "[2/5] Building latest DSPlus code..." -ForegroundColor Yellow
go build -o DSPlus.exe .
if ($LASTEXITCODE -ne 0) {
    Write-Host "Failed to build DSPlus!" -ForegroundColor Red
    exit $LASTEXITCODE
}
Write-Host "DSPlus.exe built successfully." -ForegroundColor Green

# 3. Launch proxy process in background
Write-Host "[3/5] Starting DSPlus service in background..." -ForegroundColor Yellow
Start-Process -FilePath ".\DSPlus.exe" -NoNewWindow
if ($LASTEXITCODE -ne 0) {
    Write-Host "Failed to start DSPlus service!" -ForegroundColor Red
    exit $LASTEXITCODE
}

# 4. Wait for service to bind port
Write-Host "[4/5] Waiting for port to be ready..." -ForegroundColor Yellow
Start-Sleep -Seconds 2

# 5. Run automated test runner suite
Write-Host "[5/5] Executing automated test suite..." -ForegroundColor Yellow
go run test/test_runner.go

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Automated compatibility test suite completed." -ForegroundColor Green
Write-Host "Notice: Proxy service is now running in background." -ForegroundColor Green
Write-Host "==========================================" -ForegroundColor Cyan
