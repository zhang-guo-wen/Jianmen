import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

const source = readFileSync('internal/integration/database_storage_migration_integration_test.go', 'utf8');
const start = source.indexOf('func TestStorageMigrationExpandsDatabaseAuditQueryPayload');
const end = source.indexOf('legacyMigrations := loadMigrationVersionSet(t, db)', start);
const setupBlock = source.slice(start, end);

test('database audit payload migration fixture rebuilds current schema, reseeds migration records, and keeps only 190006 pending', () => {
  assert.doesNotMatch(
    setupBlock,
    /seedAppliedMigrations\([\s\S]*auditDBQueryLargePayloadMigrationVersion[\s\S]*databaseGatewayModeMigrationVersion[\s\S]*databaseTLSDefaultMigrationVersion[\s\S]*storage\.Migrate\(db\)/,
  );
  assert.match(setupBlock, /createCurrentSchemaWithOnlyMigrationPending\(t, db, auditDBQueryLargePayloadMigrationVersion\)/);
  assert.match(setupBlock, /seedAllCurrentMigrationsExcept\(t, db, auditDBQueryLargePayloadMigrationVersion\)/);
});
