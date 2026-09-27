import assert from 'node:assert/strict';
import test from 'node:test';

import { TABLE_COLUMNS, TABLE_COLUMN_WIDTHS } from './tableColumns';

test('semantic table columns use the shared width scale', () => {
  assert.deepEqual(TABLE_COLUMN_WIDTHS, {
    address: 200,
    url: 240,
    status: 88,
    number: 104,
    note: 200,
    time: 176,
    group: 144,
    identifier: 136,
    actionsCompact: 96,
    actions: 160,
    actionsWide: 224,
    actionsExtraWide: 280,
  });
});

test('data columns align left and action columns stay fixed on the right', () => {
  for (const key of ['address', 'url', 'status', 'number', 'note', 'time', 'group', 'identifier'] as const) {
    assert.equal(TABLE_COLUMNS[key].align, 'left');
    assert.equal(TABLE_COLUMNS[key].headerAlign, 'left');
  }

  for (const key of ['actionsCompact', 'actions', 'actionsWide', 'actionsExtraWide'] as const) {
    assert.equal(TABLE_COLUMNS[key].align, 'right');
    assert.equal(TABLE_COLUMNS[key].headerAlign, 'right');
    assert.equal(TABLE_COLUMNS[key].fixed, 'right');
  }
});

test('variable text columns expose overflow content while stable columns stay fixed', () => {
  assert.equal(TABLE_COLUMNS.note.minWidth, TABLE_COLUMN_WIDTHS.note);
  assert.equal(TABLE_COLUMNS.url.minWidth, TABLE_COLUMN_WIDTHS.url);
  assert.equal(TABLE_COLUMNS.address.width, TABLE_COLUMN_WIDTHS.address);
  assert.equal(TABLE_COLUMNS.time.width, TABLE_COLUMN_WIDTHS.time);
  assert.equal(TABLE_COLUMNS.group.width, TABLE_COLUMN_WIDTHS.group);
  assert.equal(TABLE_COLUMNS.identifier.width, TABLE_COLUMN_WIDTHS.identifier);
  assert.equal(TABLE_COLUMNS.note.showOverflowTooltip, true);
});
