import assert from 'node:assert/strict';
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
