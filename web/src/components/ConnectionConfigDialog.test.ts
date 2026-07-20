import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { buildConnectionCommands, type ConnectionCommandInput } from '../utils/connectionConfigCommands.ts';

test('connection commands include both SSH and SFTP when both are allowed', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: true,
    allowSftp: true,
    connectionInfo: {
      host: '10.0.0.1',
      port: 2222,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 2);
  assert.equal(commands[0]?.label.includes('SSH'), true);
  assert.equal(commands[1]?.label.includes('SFTP'), true);
  assert.equal(commands[0]?.value, 'ssh admin@10.0.0.1 -p 2222');
  assert.equal(commands[1]?.value, 'sftp -P 2222 admin@10.0.0.1');
});

test('connection commands include only SSH when SFTP is denied', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: true,
    allowSftp: false,
    connectionInfo: {
      host: '10.0.0.1',
      port: 22,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 1);
  assert.equal(commands[0]?.label.includes('SSH'), true);
  assert.equal(commands.some(item => item.label.includes('SFTP')), false);
});

test('connection commands include only SFTP when SSH is denied', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: false,
    allowSftp: true,
    connectionInfo: {
      host: '10.0.0.1',
      port: 22,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 1);
  assert.equal(commands[0]?.label.includes('SFTP'), true);
  assert.equal(commands.some(item => item.label.includes('SSH')), false);
});

test('connection commands hidden when both SSH and SFTP are denied', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: false,
    allowSftp: false,
    connectionInfo: {
      host: '10.0.0.1',
      port: 22,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 0);
});

test('ConnectionConfigDialog props use Vue-normalized casing', () => {
  const componentPath = new URL('../components/ConnectionConfigDialog.vue', import.meta.url);
  const componentSource = readFileSync(componentPath, 'utf8');

  assert.ok(/allowSsh\?: boolean/.test(componentSource));
  assert.ok(/allowSftp\?: boolean/.test(componentSource));
  assert.equal(/allowSSH/.test(componentSource), false);
  assert.equal(/allowSFTP/.test(componentSource), false);

  assert.ok(/allowSsh/.test(componentSource));
  assert.ok(/allowSftp/.test(componentSource));

  assert.equal(/:\s*allowSSH/.test(componentSource), false);
  assert.equal(/:\s*allowSFTP/.test(componentSource), false);
});

test('ConnectionConfigDialog call sites keep kebab-case attrs', () => {
  const callers = [
    { file: new URL('../views/DatabaseView.vue', import.meta.url), expected: [':allow-ssh='] },
    { file: new URL('../views/HostsView.vue', import.meta.url), expected: [':allow-ssh=', ':allow-sftp='] },
  ];

  for (const caller of callers) {
    const viewSource = readFileSync(caller.file, 'utf8');

    assert.equal(/:allowSSH/.test(viewSource), false);
    assert.equal(/:allowSFTP/.test(viewSource), false);
    assert.ok(/<ConnectionConfigDialog/.test(viewSource));
    for (const attr of caller.expected) {
      assert.ok(viewSource.includes(attr), `missing ${attr} in ${caller.file}`);
    }
    assert.ok(/:allow-(ssh|sftp)=/.test(viewSource), `missing kebab prop in ${caller.file}`);
  }
});
