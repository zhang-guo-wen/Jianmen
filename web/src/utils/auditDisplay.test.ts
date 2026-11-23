import assert from 'node:assert/strict';
import test from 'node:test';

import {
  auditFailureDetail,
  connectionOutcomePresentation,
  formatAuditTimestamp,
  loginOutcomePresentation,
  loginReasonForDisplay,
  operationResultPresentation,
  parseOperationAuditMetadata,
  queryResultPresentation,
} from './auditDisplay';

test('unknown and pending audit outcomes are never presented as failures', () => {
  assert.deepEqual(loginOutcomePresentation('pending'), { code: 'pending', tag: 'warning' });
  assert.deepEqual(loginOutcomePresentation('new-value'), { code: 'unknown', tag: 'info' });
  assert.deepEqual(operationResultPresentation({
    detail: '{"phase":"intent","result":"pending"}',
  }), { code: 'pending', tag: 'warning' });
  assert.deepEqual(operationResultPresentation({ detail: '{}' }), { code: 'unknown', tag: 'info' });
  assert.deepEqual(queryResultPresentation('recorded'), { code: 'recorded', tag: 'info' });
  assert.deepEqual(queryResultPresentation(''), { code: 'unknown', tag: 'info' });
});

test('operation metadata prefers structured fields and supports legacy detail JSON', () => {
  assert.deepEqual(parseOperationAuditMetadata({
    phase: 'result',
    result: 'success',
    request_id: 'structured',
    status_code: 204,
    detail: '{"phase":"intent","result":"pending","request_id":"legacy","status":500}',
  }), {
    phase: 'result',
    result: 'success',
    requestId: 'structured',
    statusCode: 204,
    errorMessage: '',
    method: '',
    path: '',
  });
  assert.deepEqual(parseOperationAuditMetadata({
    detail: '{"phase":"result","result":"failure","request_id":"req-1","status":503}',
  }), {
    phase: 'result',
    result: 'failure',
    requestId: 'req-1',
    statusCode: 503,
    errorMessage: '',
    method: '',
    path: '',
  });
});

test('failed operations expose the recorded reason and request path', () => {
  assert.deepEqual(parseOperationAuditMetadata({
    detail: '{"phase":"result","result":"failure","status":502,"error_message":"上游连接超时","method":"post","path":"/api/targets/test-connection"}',
  }), {
    phase: 'result',
    result: 'failure',
    requestId: '',
    statusCode: 502,
    errorMessage: '上游连接超时',
    method: 'POST',
    path: '/api/targets/test-connection',
  });
});

test('connection and query outcomes retain their distinct meanings', () => {
  assert.equal(connectionOutcomePresentation('', 'active').code, 'active');
  assert.equal(connectionOutcomePresentation('terminated').code, 'terminated');
  assert.equal(connectionOutcomePresentation('denied').tag, 'danger');
  assert.equal(queryResultPresentation('policy_denied').code, 'denied');
  assert.equal(queryResultPresentation('error').code, 'failure');
});

test('legacy linkage metadata is not shown as a login reason', () => {
  assert.equal(loginReasonForDisplay('intent_id=abc;bad_password'), 'bad_password');
  assert.equal(loginReasonForDisplay('intent_id=abc'), '');
  assert.equal(loginReasonForDisplay('account_disabled'), 'account_disabled');
  assert.equal(auditFailureDetail('timeout', '连接超时'), 'timeout：连接超时');
});

test('audit timestamps use the fixed second-level display shape', () => {
  assert.equal(formatAuditTimestamp('2026-07-24T03:36:18'), '2026-07-24 03:36:18');
  assert.equal(formatAuditTimestamp('not-a-time', '无'), '无');
});
