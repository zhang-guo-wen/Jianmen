import type {
  TargetConnectionTestPayload,
  TargetPayload,
} from "@/api/client";

/**
 * 构造连接测试 payload。
 *
 * 新增账号尚未保存时，buildAccountPayload 会生成一个临时 ID；若把它发给后端，
 * test-connection 会按该 ID 从存储加载账号（账号不存在则报 target not found）。
 * 新增模式必须去掉 ID，后端才会走「按 host_id 解析父主机后直测」分支
 * （参考 internal/service/host_management_connection.go ResolveConnectionTest）。
 */
export function buildConnectionTestPayload(
  payload: TargetPayload,
  isNewAccount: boolean,
): TargetConnectionTestPayload {
  if (!isNewAccount) return payload;
  return { ...payload, id: "" };
}
