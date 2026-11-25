# GitHub hosts 自动修复
#
# 背景（2026-11-25）：github.com 会解析到多个 IP，其中部分在国内网络下不可达；
# 浏览器/curl 随机命中坏 IP 就表现为 ERR_CONNECTION_TIMED_OUT，且好 IP 会轮换。
# 本脚本定期探测候选 IP，若当前 hosts 指向的 IP 已失效，自动切到可用的那个。
#
# 安装（管理员 PowerShell，一次即可）：
#   schtasks /Create /TN "GitHubHostsHeal" /SC MINUTE /MO 10 /RL HIGHEST /F ^
#     /TR "powershell -NoProfile -ExecutionPolicy Bypass -File C:\dongxin\bss-ops-system\scripts\github-hosts-heal.ps1"
#
# 手动跑一次：右键“以管理员身份运行” PowerShell 后执行本文件。

$ErrorActionPreference = 'Stop'
$hostsPath = "$env:SystemRoot\System32\drivers\etc\hosts"
$probeURL  = 'https://github.com/wrf2071380081-design/bss-ops-system-main'

# 候选 IP：GitHub 的几组入口（东南亚 / 美西 / 澳）
$candidates = @('20.27.177.113', '140.82.112.3', '4.237.22.38', '20.205.243.166', '140.82.113.3')

function Test-IP([string]$ip) {
    try {
        $args = @('-s', '-o', 'NUL', '-m', '6', '--noproxy', '*',
                  '--resolve', "github.com:443:$ip", '-w', '%{http_code}', $probeURL)
        $code = & curl.exe @args 2>$null
        return ($code -eq '200')
    } catch { return $false }
}

function Get-CurrentPin {
    if (-not (Test-Path $hostsPath)) { return $null }
    $line = Get-Content $hostsPath | Where-Object { $_ -match '^\s*([0-9\.]+)\s+github\.com\s*$' } | Select-Object -First 1
    if (-not $line) { return $null }
    return ($line -split '\s+')[0]
}

$current = Get-CurrentPin
if ($current -and (Test-IP $current)) {
    Write-Host "[ok] $current 仍然可用，无需修改"
    exit 0
}

$good = $null
foreach ($ip in $candidates) {
    if (Test-IP $ip) { $good = $ip; break }
}
if (-not $good) {
    Write-Host "[fail] 所有候选 IP 都不可达，可能需要代理/VPN" -ForegroundColor Red
    exit 1
}

$content = Get-Content $hostsPath | Where-Object { $_ -notmatch 'github\.com' }
$content += "$good   github.com"
Set-Content -Path $hostsPath -Value $content -Encoding ASCII
& ipconfig /flushdns | Out-Null
Write-Host "[fixed] github.com -> $good"
