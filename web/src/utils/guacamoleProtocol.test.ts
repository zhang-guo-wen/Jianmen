import assert from 'node:assert/strict';
import test from 'node:test';

import {
  encodeGuacamoleInstruction,
  UnicodeGuacamoleParser,
} from './guacamoleProtocol';

test('encodes Guacamole lengths as Unicode codepoints', () => {
  assert.equal(
    encodeGuacamoleInstruction(['clipboard', 1, 'text/😀']),
    '9.clipboard,1.1,6.text/😀;',
  );
});

test('encodes Guacamole boolean parameters as protocol integers', () => {
  assert.equal(
    encodeGuacamoleInstruction(['key', 97, true]),
    '3.key,2.97,1.1;',
  );
  assert.equal(
    encodeGuacamoleInstruction(['key', 97, false]),
    '3.key,2.97,1.0;',
  );
});

test('parses supplementary Unicode split across packets', () => {
  const parser = new UnicodeGuacamoleParser();
  const instructions: Array<[string, string[]]> = [];
  parser.oninstruction = (opcode, parameters) => {
    instructions.push([opcode, parameters]);
  };

  const emoji = '😀';
  parser.receive(`4.name,1.${emoji[0]}`);
  parser.receive(`${emoji[1]};4.sync,1.1;`);

  assert.deepEqual(instructions, [
    ['name', ['😀']],
    ['sync', ['1']],
  ]);
});

test('rejects malformed and oversized instructions', () => {
  const malformed = new UnicodeGuacamoleParser();
  assert.throws(() => malformed.receive('x.name;'), /length/);

  const oversized = new UnicodeGuacamoleParser();
  assert.throws(
    () => oversized.receive(`${(1 << 20) + 1}.`),
    /exceeds/,
  );
});

test('bounds the whole instruction across already-parsed elements', () => {
  const parser = new UnicodeGuacamoleParser();
  const element = `1048576.${'x'.repeat(1 << 20)},`;

  for (let index = 0; index < 7; index += 1) {
    parser.receive(element);
  }
  assert.throws(() => parser.receive(element), /instruction exceeds/);
});
