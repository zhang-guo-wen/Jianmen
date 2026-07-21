---
name: run-jianmen
description: Use when asked to run, launch, start, restart, test, or smoke-test the Jianmen bastion application
---

# Run Jianmen

## Mandatory runtime policy

Jianmen local runtime is container-only. Always run it from the repository root with `start.ps1`.

Never start any of these local processes:

- `bin/bastion-core.exe`
- `go run ./cmd/bastion-core`
- `npm run dev`
- a standalone Vite process

Do not fall back to native Go/Vite when Docker is unavailable. Report the Docker error instead.
Windows without `docker.exe` is not sufficient evidence that Docker is unavailable: `start.ps1`
automatically discovers a Docker Engine inside WSL.

If working in a git worktree, do not start the service there. Merge the verified change back to
the main workspace, then run the main workspace script.

## Start and restart

Full artifact build, image build, container recreation, and health verification:

```powershell
.\start.ps1
```

Reuse the existing image and recreate the container:

```powershell
.\start.ps1 -SkipBuild
```

Use the certificate-backed HTTPS configuration:

```powershell
.\start.ps1 -EnableTLS
```

The script must exit with an error if neither Windows Docker nor WSL Docker is available. It must
not start native services. The expected persistent service container name is `jianmen`; local
startup uses one `docker run` service container and does not use Docker Compose.

## Verification

Run these checks after startup:

```powershell
wsl.exe -d Debian -e docker ps --filter 'name=^/jianmen$'
(Invoke-WebRequest 'http://127.0.0.1:47100/api/init/status' -UseBasicParsing).StatusCode
Get-CimInstance Win32_Process |
  Where-Object {
    $_.Name -in @('bastion-core.exe', 'node.exe') -and
    $_.CommandLine -like '*Jianmen*'
  }
```

Expected results:

- container `jianmen` is `healthy`;
- the init-status request returns HTTP 200;
- no Jianmen native backend or Vite process is present;
- port `47101` is unused because the frontend is embedded in the Go binary.

If localhost access fails while the container is healthy, check whether a native process had
occupied the ports when WSL started. Stop that process, restart WSL if its localhost forwarding
did not recover, then rerun `.\start.ps1 -SkipBuild`.

## Diagnostics

```powershell
wsl.exe -d Debian -e docker logs --tail 120 jianmen
wsl.exe -d Debian -e docker inspect jianmen --format '{{json .State.Health}}'
```

When a system-wide HTTP proxy interferes with localhost checks, use PowerShell
`Invoke-WebRequest`, or use `curl --noproxy '*'`.
