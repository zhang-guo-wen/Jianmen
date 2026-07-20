import type { DBGatewayConfig } from '@/api/client';

export function databaseProtocolLabel(protocol?: string): string {
  const labels: Record<string, string> = {
    mysql: 'MySQL',
    postgres: 'PG',
    postgresql: 'PG',
    redis: 'Redis',
  };
  const normalized = String(protocol || 'mysql').trim().toLowerCase();
  return labels[normalized] || normalized.toUpperCase();
}

export function databaseGatewayConnectionError(
  gateway: DBGatewayConfig | null | undefined,
  protocol: string,
): string | null {
  const label = databaseProtocolLabel(protocol);
  if (!gateway?.enabled) return `${label} 数据库网关未启用`;
  if (gateway.connectable) return null;
  if (gateway.unavailable_reason === 'tls_identity_missing') {
    return `统一数据库入口已启用，但 ${label} 连接所需的 TLS 证书尚未就绪`;
  }
  return `${label} 数据库网关暂不可用`;
}
