export type AuditTagType = 'success' | 'warning' | 'danger' | 'info' | 'primary';

export type AuditResultCode =
  | 'success'
  | 'failure'
  | 'blocked'
  | 'denied'
  | 'terminated'
  | 'active'
  | 'connecting'
  | 'pending'
  | 'recorded'
  | 'unknown';

export interface AuditResultPresentation {
  code: AuditResultCode;
  tag: AuditTagType;
}

type OperationResultSource = {
  detail?: string;
  phase?: string;
  result?: string;
  request_id?: string;
  status_code?: number;
};

export interface OperationAuditMetadata {
  phase: string;
  result: string;
  requestId: string;
  statusCode?: number;
}

function normalized(value: unknown): string {
  return String(value ?? '').trim().toLowerCase();
}

function presentation(code: AuditResultCode, tag: AuditTagType): AuditResultPresentation {
  return { code, tag };
}

export function connectionOutcomePresentation(
  outcome: unknown,
  state?: unknown,
): AuditResultPresentation {
  const value = normalized(outcome) || normalized(state);
  switch (value) {
    case 'success':
    case 'succeeded':
    case 'completed':
      return presentation('success', 'success');
    case 'failure':
    case 'failed':
    case 'error':
      return presentation('failure', 'danger');
    case 'blocked':
      return presentation('blocked', 'warning');
    case 'denied':
    case 'policy_denied':
      return presentation('denied', 'danger');
    case 'terminated':
    case 'cancelled':
    case 'canceled':
      return presentation('terminated', 'warning');
    case 'active':
      return presentation('active', 'success');
    case 'connecting':
      return presentation('connecting', 'info');
    case 'pending':
      return presentation('pending', 'warning');
    default:
      return presentation('unknown', 'info');
  }
}

export function loginOutcomePresentation(outcome: unknown): AuditResultPresentation {
  switch (normalized(outcome)) {
    case 'success':
    case 'succeeded':
      return presentation('success', 'success');
    case 'failure':
    case 'failed':
    case 'error':
      return presentation('failure', 'danger');
    case 'blocked':
      return presentation('blocked', 'warning');
    case 'pending':
      return presentation('pending', 'warning');
    default:
      return presentation('unknown', 'info');
  }
}

export function parseOperationAuditMetadata(row: OperationResultSource): OperationAuditMetadata {
  let detail: Record<string, unknown> = {};
  if (row.detail) {
    try {
      const parsed = JSON.parse(row.detail) as unknown;
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        detail = parsed as Record<string, unknown>;
      }
    } catch {
      detail = {};
    }
  }
  const rawStatus = row.status_code ?? detail.status;
  const statusCode = typeof rawStatus === 'number'
    ? rawStatus
    : (typeof rawStatus === 'string' && rawStatus.trim() ? Number(rawStatus) : undefined);
  return {
    phase: String(row.phase ?? detail.phase ?? '').trim().toLowerCase(),
    result: String(row.result ?? detail.result ?? '').trim().toLowerCase(),
    requestId: String(row.request_id ?? detail.request_id ?? '').trim(),
    statusCode: Number.isFinite(statusCode) ? statusCode : undefined,
  };
}

export function operationResultPresentation(row: OperationResultSource): AuditResultPresentation {
  const metadata = parseOperationAuditMetadata(row);
  switch (metadata.result) {
    case 'success':
    case 'succeeded':
      return presentation('success', 'success');
    case 'failure':
    case 'failed':
    case 'error':
      return presentation('failure', 'danger');
    case 'blocked':
      return presentation('blocked', 'warning');
    case 'denied':
    case 'policy_denied':
      return presentation('denied', 'danger');
    case 'pending':
      return presentation('pending', 'warning');
    default:
      return metadata.phase === 'intent'
        ? presentation('pending', 'warning')
        : presentation('unknown', 'info');
  }
}

export function queryResultPresentation(status: unknown): AuditResultPresentation {
  switch (normalized(status)) {
    case 'success':
    case 'succeeded':
      return presentation('success', 'success');
    case 'failure':
    case 'failed':
    case 'error':
      return presentation('failure', 'danger');
    case 'denied':
    case 'policy_denied':
      return presentation('denied', 'danger');
    case 'pending':
    case 'running':
      return presentation('pending', 'warning');
    case 'recorded':
      return presentation('recorded', 'info');
    default:
      return presentation('unknown', 'info');
  }
}

export function auditFailureDetail(code: unknown, message: unknown): string {
  const normalizedCode = String(code ?? '').trim();
  const normalizedMessage = String(message ?? '').trim();
  if (normalizedCode && normalizedMessage) return `${normalizedCode}：${normalizedMessage}`;
  return normalizedMessage || normalizedCode;
}

export function loginReasonForDisplay(reason: unknown): string {
  const value = String(reason ?? '').trim();
  if (!value.startsWith('intent_id=')) return value;
  const separator = value.indexOf(';');
  return separator >= 0 ? value.slice(separator + 1).trim() : '';
}

export function formatAuditTimestamp(value: unknown, empty = '-'): string {
  let date: Date | undefined;
  if (typeof value === 'number' && Number.isFinite(value)) {
    date = new Date(value);
  } else if (typeof value === 'string' && value.trim()) {
    const milliseconds = Date.parse(value);
    if (!Number.isNaN(milliseconds)) date = new Date(milliseconds);
  }
  if (!date || Number.isNaN(date.getTime())) return empty;

  const parts = new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date);
  const values = Object.fromEntries(parts.map(part => [part.type, part.value]));
  return `${values.year}-${values.month}-${values.day} ${values.hour}:${values.minute}:${values.second}`;
}
