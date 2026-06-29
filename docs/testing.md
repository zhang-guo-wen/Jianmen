# Testing Strategy

This project uses two test layers. Keep them separate so fast checks stay reliable and integration checks prove real protocol behavior.

## Unit and lightweight package tests

Run by default:

```powershell
go test -count=1 ./...
```

Rules:

- Use `t.TempDir()` or `:memory:` SQLite only.
- Never read or write `data/bastion.db`, `data/`, `logs/`, or a developer's local runtime files.
- Seed data inside each test and close database handles with `db.DB().Close()`.
- Do not use real admin passwords, production credentials, or local config files.
- Use `-count=1` when validating changes so Go's test cache cannot hide pollution.

These tests cover store behavior, metadata migration fallback, RBAC checks, parsers, observers, recording artifacts, and admin handlers without external services.

## Docker integration tests

Run explicitly with the `integration` build tag:

```powershell
go test -count=1 -tags=integration ./internal/integration
```

Or run all packages plus integration tests:

```powershell
go test -count=1 -tags=integration ./...
```

The integration suite starts simulated upstream services in Docker:

- OpenSSH target container for SSH proxy and SSH audit artifacts.
- MySQL target containers for database gateway and SQL audit artifacts. By default this runs `mysql:5.7` and `mysql:8.0`.
- PostgreSQL target container for database gateway and SQL audit artifacts.

If Docker is unavailable, integration tests are skipped by default. To make missing Docker a hard failure in CI:

```powershell
$env:JIANMEN_REQUIRE_DOCKER = "1"
go test -count=1 -tags=integration ./internal/integration
```

On Windows, if `docker` is not available in PowerShell but WSL has Docker, the integration tests automatically call `wsl.exe -e docker ...`.

To override the MySQL image matrix:

```powershell
$env:JIANMEN_MYSQL_IMAGES = "mysql:5.7,mysql:8.0"
go test -count=1 -tags=integration -run TestDatabaseGatewayMySQLAgainstDocker ./internal/integration
```

The MySQL integration test creates a temporary metadata database, starts each MySQL image, connects through the Jianmen database gateway, executes `SELECT 42 AS audit_probe`, and asserts that the SQL text is present in `replay/db/*/queries.jsonl`.

## Convenience script

```powershell
.\scripts\test.ps1
.\scripts\test.ps1 -Frontend
.\scripts\test.ps1 -Integration
.\scripts\test.ps1 -Integration -RequireDocker
```

## Current integration constraints

- PostgreSQL integration uses trust auth in the simulated upstream container. The gateway still validates the bastion user password before dialing upstream.
- PostgreSQL integration uses a database name matching the upstream user because the current gateway only forwards the upstream user in its StartupMessage. Preserving the client's requested database should be covered by a future regression test.
- MySQL client passwords are sent as MySQL protocol challenge responses, so validating the bastion user's bcrypt password on the MySQL gateway path needs a separate protocol design. Do not treat the current MySQL integration test as proof of user-password verification.
