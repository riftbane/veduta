# Veduta installer for Windows, in PowerShell:
#
#   irm https://raw.githubusercontent.com/riftbane/veduta/main/install.ps1 | iex
#
# Puts veduta.exe in %LOCALAPPDATA%\Programs\veduta and that folder on your PATH, with no
# administrator rights. A Lua game needs nothing more; a Go game (veduta init --go) needs Go
# 1.25 or newer from https://go.dev/dl/. Running it again installs the requested version in
# place. Set these before running it to choose:
#
#   $env:VEDUTA_VERSION = "v2.0.0"   a version (default: the newest of the channel)
#   $env:VEDUTA_CHANNEL = "beta"     stable (default) or beta, which includes release candidates
#   $env:VEDUTA_HOME    = "D:\veduta" another folder
& {
	$ErrorActionPreference = 'Stop'
	$ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is many times slower with it
	[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

	$repo = 'riftbane/veduta'
	$version = $env:VEDUTA_VERSION
	$channel = if ($env:VEDUTA_CHANNEL) { $env:VEDUTA_CHANNEL } else { 'stable' }
	$dir = if ($env:VEDUTA_HOME) { $env:VEDUTA_HOME } else { Join-Path $env:LOCALAPPDATA 'Programs\veduta' }
	if ($channel -ne 'stable' -and $channel -ne 'beta') {
		throw "install.ps1: unknown channel $channel (want stable or beta)"
	}

	# A token already in the environment only raises the GitHub API's rate limit, which shared
	# addresses such as CI runners hit; the releases are public. It goes to the API only.
	$headers = @{ 'User-Agent' = 'veduta-install' }
	$token = if ($env:GITHUB_TOKEN) { $env:GITHUB_TOKEN } else { $env:GH_TOKEN }
	if ($token) { $headers['Authorization'] = "Bearer $token" }

	if (-not $version) {
		if ($channel -eq 'stable') {
			$version = (Invoke-RestMethod -Headers $headers "https://api.github.com/repos/$repo/releases/latest").tag_name
		} else {
			# The newest by version, not by date: major, minor and patch as numbers, a release
			# ahead of its own candidates, and the candidates' numbers compared as numbers.
			$best = $null
			$bestKey = $null
			# Assigned first: Windows PowerShell passes the whole list on as one object.
			$releases = Invoke-RestMethod -Headers $headers "https://api.github.com/repos/$repo/releases?per_page=30"
			foreach ($r in $releases) {
				if ($r.tag_name -notmatch '^v(\d+)\.(\d+)\.(\d+)(?:-(.+))?$') { continue }
				# Taken before the suffix is looked at: every -match there replaces $Matches.
				$key = '{0:D10}{1:D10}{2:D10}' -f [long]$Matches[1], [long]$Matches[2], [long]$Matches[3]
				$pre = '~'
				if ($Matches[4]) {
					$pre = ($Matches[4].Split('.') | ForEach-Object { if ($_ -match '^\d+$') { '{0:D10}' -f [long]$_ } else { $_ } }) -join '.'
				}
				$key += $pre
				if ($null -eq $bestKey -or [string]::CompareOrdinal($key, $bestKey) -gt 0) {
					$best = $r.tag_name
					$bestKey = $key
				}
			}
			$version = $best
		}
		if (-not $version) { throw "install.ps1: could not find the newest $channel release; set `$env:VEDUTA_VERSION" }
	}
	if ($version -notmatch '^v') { $version = "v$version" }

	$archive = "veduta_${version}_windows_amd64.zip"
	$base = "https://github.com/$repo/releases/download/$version"
	$tmp = Join-Path ([IO.Path]::GetTempPath()) ("veduta-" + [Guid]::NewGuid())
	New-Item -ItemType Directory -Path $tmp | Out-Null
	try {
		Write-Host "Downloading veduta $version"
		Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile (Join-Path $tmp $archive)
		Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
		$want = $null
		foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
			$parts = $line -split '\s+'
			if ($parts.Count -ge 2 -and $parts[1] -eq $archive) { $want = $parts[0].ToLowerInvariant() }
		}
		if (-not $want) { throw "install.ps1: checksums.txt has no entry for $archive" }
		$got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $archive)).Hash.ToLowerInvariant()
		if ($got -ne $want) { throw "install.ps1: checksum mismatch for $archive (got $got, want $want); nothing was installed" }
		Write-Host "Checksum OK ($got)"
		Expand-Archive -Force -Path (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'x')

		New-Item -ItemType Directory -Force -Path $dir | Out-Null
		$exe = Join-Path $dir 'veduta.exe'
		# A running veduta.exe cannot be overwritten, but it can be renamed out of the way.
		if (Test-Path $exe) {
			Remove-Item -Force -ErrorAction SilentlyContinue "$exe.old"
			Move-Item -Force $exe "$exe.old"
		}
		Copy-Item (Join-Path $tmp 'x\veduta.exe') $exe
		Remove-Item -Force -ErrorAction SilentlyContinue "$exe.old"
		Write-Host "Installed $exe"
	} finally {
		Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tmp
	}

	$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
	$entries = @(if ($userPath) { $userPath.Split(';') | Where-Object { $_ } })
	if (-not ($entries | Where-Object { $_.TrimEnd('\') -ieq $dir.TrimEnd('\') })) {
		[Environment]::SetEnvironmentVariable('Path', (($entries + $dir) -join ';'), 'User')
		Write-Host "Added $dir to your PATH (new terminals see it)"
	}
	if (-not (($env:Path.Split(';')) | Where-Object { $_.TrimEnd('\') -ieq $dir.TrimEnd('\') })) {
		$env:Path = "$env:Path;$dir"
	}

	& $exe version
	if (Get-Command go -ErrorAction SilentlyContinue) {
		Write-Host "Go: $(go env GOVERSION)"
	} else {
		Write-Host "Lua games need nothing else. For a Go game (veduta init --go), install Go 1.25 or newer: https://go.dev/dl/"
	}
	Write-Host ''
	Write-Host 'Next: veduta init mygame; cd mygame; veduta sim'
}
