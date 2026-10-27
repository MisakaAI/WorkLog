param(
    [string]$AppDirectory = (Join-Path $PSScriptRoot '..\dist\win-unpacked')
)
$ErrorActionPreference = 'Stop'
# Run with Windows PowerShell 5.1+ on Windows x64. No Node/Go required.
$AppDirectory = (Resolve-Path $AppDirectory).Path
$data = Join-Path $env:LOCALAPPDATA ('WorklogDesktop-smoke-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $data | Out-Null
$supervisor = $null
$backendId = 0

function Assert-BackendStopped([int]$ProcessId) {
    for ($i = 0; $i -lt 50; $i++) {
        if (-not (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) { return }
        Start-Sleep -Milliseconds 100
    }
    throw "Backend process $ProcessId survived supervisor shutdown"
}

try {
    $token = ([guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N'))
    $conf = Join-Path $data 'backend.json'
    $json = @{
        addr = '127.0.0.1:0'; db_driver = 'sqlite'
        db_dsn = (Join-Path $data 'worklog.db')
        token_file = (Join-Path $data 'mcp-token'); write_token = $token
        static_dir = (Join-Path $AppDirectory 'resources\web')
        allowed_hosts = @('127.0.0.1'); allowed_origins = @()
    } | ConvertTo-Json
    [System.IO.File]::WriteAllText($conf, $json, (New-Object System.Text.UTF8Encoding $false))
    $backend = Join-Path $AppDirectory 'resources\backend\worklog.exe'
    $helper = Join-Path $AppDirectory 'resources\backend\worklog-supervisor.exe'
    for ($run = 0; $run -lt 2; $run++) {
        $info = New-Object System.Diagnostics.ProcessStartInfo
        $info.FileName = $helper
        $info.Arguments = '"{0}" "{1}"' -f $backend, $conf
        $info.WorkingDirectory = $data
        $info.UseShellExecute = $false
        $info.CreateNoWindow = $true
        $info.RedirectStandardInput = $true
        $info.RedirectStandardOutput = $true
        $info.RedirectStandardError = $true
        $supervisor = New-Object System.Diagnostics.Process
        $supervisor.StartInfo = $info
        if (-not $supervisor.Start()) { throw 'Cannot start supervisor' }
        $stderr = $supervisor.StandardError.ReadToEndAsync()
        $line = $supervisor.StandardOutput.ReadLineAsync()
        if (-not $line.Wait(90000)) { throw 'Listener notification timed out' }
        if (-not $line.Result) { throw 'Supervisor exited before reporting its port' }
        $message = $line.Result | ConvertFrom-Json
        if ($message.type -ne 'listening' -or $message.port -lt 1) { throw 'Invalid listener notification' }
        $backendId = [int]$message.pid
        $url = 'http://127.0.0.1:' + $message.port
        $healthy = $false
        for ($i = 0; $i -lt 40; $i++) {
            try {
                $healthy = (Invoke-RestMethod "$url/healthz" -TimeoutSec 3).ok -eq $true
            } catch { $healthy = $false }
            if ($healthy) { break }
            Start-Sleep -Milliseconds 250
        }
        if (-not $healthy) { throw 'Health check failed' }
        $listeners = @(Get-NetTCPConnection -State Listen -OwningProcess $backendId)
        if ($listeners.Count -ne 1 -or $listeners[0].LocalAddress -ne '127.0.0.1' -or $listeners[0].LocalPort -ne $message.port) {
            throw 'Backend is not exclusively listening on the reported IPv4 loopback port'
        }
        $page = Invoke-WebRequest "$url/" -UseBasicParsing
        if ($page.Content -notmatch 'id="app"') { throw 'Vue entry missing' }
        $runtimeConfig = Invoke-WebRequest "$url/config.js" -UseBasicParsing
        if ($runtimeConfig.Content -notmatch "apiBaseUrl: '/api'") { throw 'API is not same-origin' }
        $headers = @{ Origin = $url }
        if ($run -eq 0) {
            $headers['Idempotency-Key'] = [guid]::NewGuid().ToString()
            $null = Invoke-RestMethod "$url/api/todos" -Method Post -Headers $headers -ContentType 'application/json' -Body '{"title":"desktop-smoke"}'
        }
        $todos = Invoke-RestMethod "$url/api/todos" -Headers @{ Origin = $url }
        if (-not ($todos.todos | Where-Object { $_.title -eq 'desktop-smoke' })) { throw 'Data did not persist' }
        if ($run -eq 0) {
            $supervisor.StandardInput.Close() # normal Electron exit / parent pipe closure
        } else {
            $supervisor.Kill() # supervisor crash: the kernel must still clean the backend
        }
        if (-not $supervisor.WaitForExit(10000)) { throw 'Supervisor did not stop' }
        Assert-BackendStopped $backendId
        $backendId = 0
        $supervisor.Dispose()
        $supervisor = $null
        Write-Host "PASS run $run : health, Vue entry, same-origin API, SQLite persistence, loopback port, process cleanup"
    }
} finally {
    if ($supervisor) {
        if (-not $supervisor.HasExited) { $supervisor.Kill(); $supervisor.WaitForExit() }
        $supervisor.Dispose()
    }
    if ($backendId) { Assert-BackendStopped $backendId }
    Remove-Item -LiteralPath $data -Recurse -Force
}
