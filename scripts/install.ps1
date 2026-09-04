<#
.SYNOPSIS
    Installs noodge.

.DESCRIPTION
    Downloads the latest release, verifies its SHA-256 against the published
    checksums, and puts it on your PATH.

    Run it with:

        irm https://raw.githubusercontent.com/wimhaanstra/noodge/main/scripts/install.ps1 | iex

    A script piped into iex cannot take parameters, so it is configured with
    environment variables instead:

        $env:NOODGE_VERSION     = 'v1.2.3'   # default: the latest release
        $env:NOODGE_INSTALL_DIR = 'C:\tools' # default: %LOCALAPPDATA%\Programs\noodge\bin
        $env:NOODGE_NO_PATH     = '1'        # do not touch PATH
#>

#Requires -Version 5.1

$ErrorActionPreference = 'Stop'
# Without this, Invoke-WebRequest on Windows PowerShell 5.1 spends most of the
# download redrawing a progress bar, which is dramatically slower.
$ProgressPreference = 'SilentlyContinue'

$Repo = 'wimhaanstra/noodge'

function Write-Step { param([string]$Message) Write-Host "==> $Message" }
function Write-Note { param([string]$Message) Write-Host "    $Message" -ForegroundColor DarkGray }

# Windows PowerShell 5.1 still defaults to TLS 1.0 on some machines, which
# GitHub refuses outright. PowerShell 7 negotiates properly and ignores this.
try {
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {
    # Not fatal: on PowerShell 7 the type behaves differently and TLS is
    # already negotiated correctly.
}

function Get-Architecture {
    # Not $env:PROCESSOR_ARCHITECTURE: under WOW64 a 32-bit PowerShell on an
    # arm64 machine reports x86, and the installer would fetch the wrong build.
    try {
        switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
            'X64'   { return 'amd64' }
            'Arm64' { return 'arm64' }
            default { throw "noodge has no build for $_" }
        }
    } catch [System.Management.Automation.RuntimeException] {
        throw
    } catch {
        # Very old .NET without RuntimeInformation.
        if ($env:PROCESSOR_ARCHITEW6432 -eq 'ARM64' -or $env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { return 'arm64' }
        return 'amd64'
    }
}

function Get-LatestVersion {
    $url = "https://api.github.com/repos/$Repo/releases/latest"
    try {
        return (Invoke-RestMethod -Uri $url -Headers @{ 'User-Agent' = 'noodge-installer' }).tag_name
    } catch {
        throw "could not work out the latest version from $url : $($_.Exception.Message)"
    }
}

function Test-Checksum {
    param(
        [string]$Path,
        [string]$Expected
    )

    $actual = (Get-FileHash -Path $Path -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $Expected.ToLower()) {
        throw "checksum mismatch for $(Split-Path $Path -Leaf)`n  expected $Expected`n  got      $actual"
    }
}

function Add-ToUserPath {
    param([string]$Directory)

    # setx is the obvious tool and the wrong one: it silently truncates PATH at
    # 1024 characters, which quietly destroys a long PATH.
    #
    # [Environment]::SetEnvironmentVariable is the next obvious tool and is
    # also wrong: it reads PATH with variables already expanded and writes the
    # result back as a plain string, so entries like %JAVA_HOME%\bin are
    # replaced by whatever they happened to point at, permanently.
    #
    # Reading the raw value and writing it back as an ExpandString is the only
    # approach that leaves an existing PATH exactly as it was.
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    try {
        $current = $key.GetValue(
            'Path', '',
            [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)

        $entries = @($current -split ';' | Where-Object { $_ -ne '' })
        if ($entries -contains $Directory) {
            Write-Note 'already on PATH'
            return $false
        }

        $updated = (@($entries) + $Directory) -join ';'
        $key.SetValue('Path', $updated, [Microsoft.Win32.RegistryValueKind]::ExpandString)
    } finally {
        $key.Dispose()
    }

    Send-SettingChange
    return $true
}

function Send-SettingChange {
    # Writing the registry updates PATH for new processes but not for anything
    # already running, including Explorer, so a freshly opened terminal would
    # still not see it until the next sign-in.
    if (-not ('NoodgeNative' -as [type])) {
        Add-Type -Namespace '' -Name 'NoodgeNative' -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(
    IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam,
    uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
    }

    $HWND_BROADCAST = [IntPtr]0xffff
    $WM_SETTINGCHANGE = 0x1A
    $SMTO_ABORTIFHUNG = 0x2

    $result = [UIntPtr]::Zero
    [void][NoodgeNative]::SendMessageTimeout(
        $HWND_BROADCAST, $WM_SETTINGCHANGE, [UIntPtr]::Zero, 'Environment',
        $SMTO_ABORTIFHUNG, 5000, [ref]$result)
}

function Install-Noodge {
    $arch = Get-Architecture

    $version = if ($env:NOODGE_VERSION) { $env:NOODGE_VERSION } else { Get-LatestVersion }
    if (-not $version.StartsWith('v')) { $version = "v$version" }
    $bare = $version.TrimStart('v')

    $installDir = if ($env:NOODGE_INSTALL_DIR) {
        $env:NOODGE_INSTALL_DIR
    } else {
        Join-Path $env:LOCALAPPDATA 'Programs\noodge\bin'
    }

    $archive = "noodge_${bare}_windows_${arch}.zip"
    $baseUrl = "https://github.com/$Repo/releases/download/$version"

    Write-Step "Installing noodge $version ($arch)"

    $work = Join-Path ([System.IO.Path]::GetTempPath()) ("noodge-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $work | Out-Null

    try {
        $archivePath = Join-Path $work $archive

        Write-Step "Downloading $archive"
        Invoke-WebRequest -Uri "$baseUrl/$archive" -OutFile $archivePath

        # Unsigned downloads over a pipe-to-shell installer have no other
        # integrity check, so this one is not optional.
        Write-Step 'Verifying checksum'
        $sumsPath = Join-Path $work 'checksums.txt'
        Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $sumsPath

        $line = Get-Content $sumsPath | Where-Object { $_ -match [regex]::Escape($archive) } | Select-Object -First 1
        if (-not $line) { throw "checksums.txt has no entry for $archive" }
        Test-Checksum -Path $archivePath -Expected ($line -split '\s+')[0]

        Write-Step "Installing to $installDir"
        Expand-Archive -Path $archivePath -DestinationPath $work -Force
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null

        $target = Join-Path $installDir 'noodge.exe'
        if (Test-Path $target) {
            # Windows will not overwrite a running executable but will let it
            # be renamed, so an upgrade while noodge is open still works.
            $old = "$target.old"
            if (Test-Path $old) { Remove-Item $old -Force -ErrorAction SilentlyContinue }
            Move-Item -Path $target -Destination $old -Force -ErrorAction SilentlyContinue
        }
        Copy-Item -Path (Join-Path $work 'noodge.exe') -Destination $target -Force

        if ($env:NOODGE_NO_PATH -ne '1') {
            Write-Step 'Adding to PATH'
            if (Add-ToUserPath -Directory $installDir) {
                Write-Note 'open a new terminal for this to take effect'
            }
        }

        Write-Host ''
        Write-Step 'Done'
        & $target version

        # A PowerShell function or alias called noodge, or another binary
        # earlier on PATH, would silently shadow what was just installed.
        $found = Get-Command noodge -ErrorAction SilentlyContinue
        if ($found -and $found.Source -and $found.Source -ne $target) {
            Write-Host ''
            Write-Warning "'noodge' currently resolves to $($found.Source), not the copy just installed."
        }

        Write-Host ''
        Write-Note 'Next: run  noodge init  in a project, then  noodge'
        Write-Note 'Tab completion:  noodge completion install pwsh'
    } finally {
        Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
    }
}

Install-Noodge
