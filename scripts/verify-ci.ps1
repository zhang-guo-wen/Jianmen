$required = @(
  'npm run typecheck',
  'npm run build',
  'go build ./...',
  'go test ./... -count=1',
  'go vet ./...'
)
$content = Get-Content '.github/workflows/ci.yml' -Raw
foreach ($command in $required) {
  if (-not $content.Contains($command)) {
    throw "missing CI command: $command"
  }
}
