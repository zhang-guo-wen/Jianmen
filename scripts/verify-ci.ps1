$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$ciWorkflowPath = Join-Path $repoRoot '.github/workflows/ci.yml'
$releaseWorkflowPath = Join-Path $repoRoot '.github/workflows/release.yml'

$required = @(
  'npm run typecheck',
  'npm run build',
  'go build ./...',
  'go test ./... -count=1',
  'go vet ./...',
  'FuzzMySQLPacketFrames',
  'FuzzReadPostgresStartupMessage',
  'FuzzReadPostgresTypedMessage',
  'FuzzRedisRESPFrameLength',
  'FuzzRedisObserverClientFrames',
  'FuzzRedisAuthenticationCommandParser',
  'JIANMEN_REQUIRE_DOCKER: "1"'
)
$content = Get-Content -LiteralPath $ciWorkflowPath -Raw
foreach ($command in $required) {
  if (-not $content.Contains($command)) {
    throw "missing CI command: $command"
  }
}

function Get-WorkflowJobBlock {
  param(
    [Parameter(Mandatory)]
    [string]$Content,

    [Parameter(Mandatory)]
    [string]$Name
  )

  $escapedName = [Regex]::Escape($Name)
  $pattern = "(?ms)^  ${escapedName}:[^\r\n]*\r?\n.*?(?=^  [A-Za-z0-9_-]+:[^\r\n]*\r?\n|\z)"
  $match = [Regex]::Match($Content, $pattern)
  if (-not $match.Success) {
    throw "missing release workflow job: $Name"
  }

  return $match.Value
}

$releaseContent = Get-Content -LiteralPath $releaseWorkflowPath -Raw
$packageJob = Get-WorkflowJobBlock -Content $releaseContent -Name 'package'
$releaseJob = Get-WorkflowJobBlock -Content $releaseContent -Name 'release'

$requiredPackageContract = @(
  'needs: quality-gates',
  './scripts/package-release.sh',
  'uses: actions/upload-artifact@v4',
  'name: release-packages',
  'path: dist/release'
)
foreach ($expected in $requiredPackageContract) {
  if (-not $packageJob.Contains($expected)) {
    throw "package job is missing contract: $expected"
  }
}

$requiredReleaseContract = @(
  'needs: package',
  'uses: actions/download-artifact@v4',
  'name: release-packages',
  'path: dist/release',
  'gh release'
)
foreach ($expected in $requiredReleaseContract) {
  if (-not $releaseJob.Contains($expected)) {
    throw "release job is missing contract: $expected"
  }
}

$releaseBuildCommands = @(
  './scripts/package-release.sh',
  'npm run',
  'go build',
  'go test',
  'go vet'
)
foreach ($command in $releaseBuildCommands) {
  if ($releaseJob.Contains($command)) {
    throw "release job must only publish artifacts; found build command: $command"
  }
}
