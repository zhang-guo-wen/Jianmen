# 云数据库资源导入未来规划

日期：2026-07-19

状态：规划中，当前未实现

## 1. 背景

Jianmen 当前的数据库实例和数据库账号由管理员在管理页面中手动创建。随着纳管资源数量增加，
逐个录入云数据库实例的名称、地址、端口、引擎和地域信息会产生较高的维护成本，也容易出现重复
录入、地址过期和状态不一致。

未来计划接入阿里云、腾讯云、华为云和 AWS 等云厂商的资源查询 API，让管理员能够从云账号中
发现数据库实例，预览后选择性导入 Jianmen。

本规划只描述未来方向和边界，不代表当前版本已经支持云数据库导入。

## 2. 建设目标

- 通过云厂商只读 API 发现数据库实例。
- 支持按云账号、地域、数据库类型和标签筛选资源。
- 导入前展示预览和冲突信息，由管理员确认后写入。
- 将云实例映射为 Jianmen 的数据库实例容器。
- 继续以 `database_account` 作为连接、授权、审计和有效期控制的最小资源。
- 支持重复导入时按云资源标识更新，避免产生重复实例。
- 为后续手动同步和定时同步预留统一的多云适配层。
- 云资源变化后能够刷新动态数据库代理配置。

## 3. 非目标

第一阶段不计划实现以下能力：

- 不通过云 API 修改或删除云数据库实例。
- 不自动导入无法安全取得凭据的数据库账号。
- 不把云账号的 AccessKey 当作数据库登录凭据。
- 不在未确认的情况下自动全量写入云账号中的所有资源。
- 不因为云端资源暂时不可见而自动删除 Jianmen 中的数据。
- 不在第一阶段实现定时同步、跨账号聚合和云密钥自动轮换。
- 不导入备份、监控指标、账单、慢日志等非纳管必需数据。

## 4. 核心资源边界

云数据库实例仍然只是容器，不能成为最终授权对象。

```text
云账号
  └── 云数据库实例
        └── Jianmen database_instance
              └── Jianmen database_account
                    ├── RBAC
                    ├── 连接控制
                    ├── 有效期与禁用状态
                    └── 审计
```

云 API 通常能够返回实例名称、云资源 ID、引擎、版本、地域、状态、网络地址和端口，但通常不会
返回数据库账号密码。即使部分厂商能够查询账号名称，也只能将其作为待补充凭据的账号草稿，不能
直接作为可连接账号启用。

## 5. 规划中的用户流程

数据库管理页面未来增加“从云厂商导入”入口：

1. 管理员选择云厂商。
2. 新建或选择已经保存的云账号凭据。
3. 测试云 API 访问权限。
4. 选择一个或多个地域。
5. 系统分页拉取数据库实例。
6. 管理员按类型、状态、标签和关键字筛选。
7. 管理员勾选需要导入的实例。
8. 系统展示字段映射、网络地址和重复资源冲突。
9. 管理员确认导入。
10. 导入完成后，为实例新增数据库账号或补充已发现账号的凭据。
11. 测试连接成功后启用账号及动态代理。

导入必须采用“发现 → 预览 → 选择 → 确认”的流程，不能在测试云凭据时直接写入资源。

## 6. 云厂商接入顺序

### 第一阶段：阿里云 RDS

优先支持：

- RDS MySQL
- RDS PostgreSQL
- RDS SQL Server
- RDS MariaDB

计划使用只读 RAM 用户或 RAM 角色，通过 `DescribeRegions`、`DescribeDBInstances`、
`DescribeDBInstanceAttribute` 和 `DescribeDBInstanceNetInfo` 等 API 获取实例信息。

第一阶段只导入实例，不自动导入数据库账号密码。

### 第二阶段：腾讯云、华为云和 AWS

在统一 Provider 接口稳定后，依次补充：

- 腾讯云 CDB、PostgreSQL 等关系型数据库。
- 华为云 RDS。
- AWS RDS 和 Aurora。

每个 Provider 必须输出统一资源结构，厂商特有字段保存在扩展元数据中，不能渗透到数据库实例
的通用业务逻辑。

### 第三阶段：扩展云数据库类型

根据 Jianmen 已有代理协议和用户需求，再评估：

- 阿里云 PolarDB
- 腾讯云 TDSQL
- AWS Aurora
- 云 Redis
- 其他兼容 MySQL、PostgreSQL 或 Redis 协议的托管数据库

协议兼容不代表可以直接接入。新增类型前必须验证地址模型、认证方式、账号模型、主从节点和代理
端点差异。

## 7. 规划中的数据模型

建议新增独立的云账号实体，不把云凭据直接放入数据库实例：

```text
cloud_account
- id
- name
- provider
- auth_type
- access_key_id
- encrypted_secret
- role_identifier
- default_regions
- enabled
- last_sync_at
- last_sync_error
- created_at
- updated_at
```

数据库实例增加云来源字段：

```text
database_instance
- source_type
- cloud_account_id
- cloud_provider
- cloud_region
- cloud_resource_id
- cloud_resource_type
- cloud_metadata_json
- last_synced_at
```

建议建立唯一约束：

```text
(cloud_account_id, cloud_region, cloud_resource_id)
```

该约束用于保证重复导入执行幂等更新，而不是创建重复实例。

若未来支持发现账号，数据库账号需要增加凭据状态：

```text
credential_status
- missing
- configured
- invalid
```

`missing` 状态的账号默认禁用，不能参与 RBAC 授权和连接。

## 8. 多云适配架构

计划在业务层定义由使用方拥有的统一接口：

```go
type Provider interface {
	TestConnection(ctx context.Context) error
	ListRegions(ctx context.Context) ([]Region, error)
	ListDatabaseInstances(ctx context.Context, region string, cursor string) (InstancePage, error)
}
```

建议目录：

```text
internal/cloud/
├── provider.go
├── aliyun/
├── tencent/
├── huawei/
└── aws/
```

Provider 输出的统一实例结构至少包含：

```text
- resource_id
- name
- region
- engine
- engine_version
- status
- endpoints
- network_type
- tags
- provider_metadata
```

云 SDK 只能存在于对应厂商适配包中。HTTP handler 只负责请求解析和响应格式化，导入、去重、
冲突处理和状态转换放在 service，数据库访问通过 store 接口完成。

## 9. 地址选择规则

云数据库可能同时返回内网地址、公网地址、只读地址、集群地址和代理地址。导入时不能简单选择
第一个地址。

计划规则：

- 显示所有可用端点及其网络类型。
- 默认优先选择 Jianmen 服务端可达的私网地址。
- 无法判断可达性时由管理员手动选择。
- 导入前执行可选的 TCP 连通性测试。
- 地址变化时保留变更记录并提示管理员。
- 端点切换后刷新动态代理配置。
- 一个云实例存在多个实际连接入口时，评估创建多个 Jianmen 数据库实例，而不是将多个地址塞入
  一个连接字段。

## 10. 凭据和账号策略

计划支持三种逐步演进的方式：

### 方式一：只导入实例

导入实例元数据后，由管理员在 Jianmen 中手动创建数据库账号。这是第一阶段默认方式。

### 方式二：发现账号名称

如果云厂商 API 支持查询账号列表，则导入账号名称、类型和状态，但不导入密码。账号以
`credential_status=missing` 保存且保持禁用，管理员补充密码并测试成功后才能启用。

### 方式三：关联云密钥服务

后续评估通过云密钥服务按需读取数据库凭据，例如 AWS Secrets Manager。该方式必须满足：

- Jianmen 不持久化云密钥服务返回的明文密码，或明确采用现有加密字段存储。
- 读取行为具备独立权限、审计和失败处理。
- 明文不得写入日志、同步错误或 API 响应。
- 密钥轮换后能够刷新连接能力。
- 禁止在列表和详情接口中回显密钥内容。

## 11. 安全要求

- 云账号默认只授予实例查询所需的最小只读权限。
- 优先支持临时凭证或角色扮演，长期 AccessKey 作为兼容方案。
- AccessKey Secret、Token 和数据库密码必须使用加密字段保存。
- API 响应、审计详情和错误日志不得包含 Secret、Token 或数据库密码。
- 云凭据的新增、编辑、删除、测试和同步都必须写入审计日志。
- 测试连接只能验证权限，不得产生云资源变更。
- 云账号禁用后停止发现和同步，但不自动禁用已导入的数据库账号。
- 前端不得直接调用云厂商 API，所有请求由 Jianmen 后端发起。
- SSRF 防护、代理设置、请求超时、重试上限和 SDK 日志脱敏必须统一处理。

## 12. 同步和冲突策略

第一阶段只提供手动导入。后续手动同步和定时同步应遵循：

- 云资源 ID 是匹配主键，名称和地址不能作为唯一标识。
- 名称、版本、状态、地址和标签等云端字段可以更新。
- Jianmen 中的账号、分组、备注、RBAC 和审计配置不能被云同步覆盖。
- 云端资源不存在时标记为“云端已不存在”，不自动删除。
- 云 API 暂时失败时保留最后一次成功数据并展示同步错误。
- 云实例禁用、过期或删除时阻止新增连接前，必须区分真实状态和同步失败。
- 所有批量导入和同步操作必须支持幂等重试。
- 代理刷新失败时保留数据库记录，显示“已导入但代理未生效”，并允许重试。

## 13. RBAC 与审计

未来至少增加以下管理权限：

```text
cloud_account:list
cloud_account:create
cloud_account:update
cloud_account:delete
cloud_account:test
cloud_database:discover
cloud_database:import
cloud_database:sync
```

云实例导入后，实际连接授权仍然绑定 `database_account`，不能直接把云账号或
`database_instance` 当作最终连接资源。

审计事件至少记录：

- 云账号创建、修改、禁用和删除。
- 云凭据测试成功或失败。
- 资源发现的厂商、地域、数量和耗时。
- 导入、跳过、更新和冲突数量。
- 单个实例的来源云资源 ID。
- 手动同步和定时同步结果。
- 动态代理刷新结果。

审计中只记录凭据标识，不记录凭据值。

## 14. 分阶段实施计划

### 里程碑 0：设计确认

- [ ] 确认云账号和云来源字段的数据模型。
- [ ] 确认数据库实例多端点映射规则。
- [ ] 确认凭据加密和脱敏方案。
- [ ] 确认 RBAC 动作和审计事件。
- [ ] 为 Provider 接口编写契约测试。

### 里程碑 1：阿里云手动导入

- [ ] 新增云账号 CRUD 和凭据测试。
- [ ] 实现阿里云 RDS Provider。
- [ ] 支持地域选择、分页发现和筛选。
- [ ] 实现导入预览、冲突提示和批量确认。
- [ ] 实现按云资源 ID 幂等写入。
- [ ] 导入后刷新动态数据库代理。
- [ ] 暂不导入数据库账号。

### 里程碑 2：实例手动同步

- [ ] 支持单实例和单云账号手动同步。
- [ ] 支持端点、状态、版本和标签更新。
- [ ] 支持云端不存在标记。
- [ ] 支持同步结果审计和失败重试。

### 里程碑 3：多云 Provider

- [ ] 接入腾讯云。
- [ ] 接入华为云。
- [ ] 接入 AWS。
- [ ] 建立所有 Provider 的统一契约测试和错误分类。

### 里程碑 4：账号发现与密钥服务

- [ ] 评估各厂商账号查询能力。
- [ ] 支持无凭据账号草稿。
- [ ] 评估并接入云密钥服务。
- [ ] 支持凭据轮换和连接刷新。

### 里程碑 5：定时同步

- [ ] 增加可配置同步周期。
- [ ] 增加并发限制、退避重试和限流处理。
- [ ] 增加同步任务状态与告警。
- [ ] 增加大规模账号和多地域性能测试。

## 15. 测试与验收规划

实现阶段至少覆盖：

- Provider 分页、限流、超时、重试和凭据失效。
- 云厂商字段到统一实例结构的映射。
- 同一云资源重复导入不产生重复记录。
- 同名但不同云资源 ID 的实例可以分别导入。
- 多端点选择和地址冲突。
- 云端资源消失时不自动删除本地数据。
- 云账号禁用后不能继续发现和同步。
- 无凭据账号不能参与连接。
- API、日志和审计无 Secret、Token、密码泄露。
- 导入、数据库写入和代理刷新部分失败时的可恢复性。
- `database_account` 的 RBAC、禁用状态和有效期在连接路径实际生效。

实现完成后按仓库规范执行：

```powershell
npm --prefix web run typecheck
npm --prefix web run build
go build ./...
go test ./... -count=1
```

涉及真实云 API 的测试使用独立测试账号，并通过集成测试配置显式启用，默认单元测试不得依赖
外部云服务。

## 16. 实施前置条件

开始开发前应满足：

- 当前数据库实例、数据库账号和动态代理生命周期稳定。
- 已有加密字段能够安全保存云凭据。
- RBAC 可以覆盖云账号管理和导入动作。
- 审计系统可以记录批量操作结果。
- 已准备权限受限的云测试账号。
- 已确认第一阶段只支持阿里云 RDS 实例导入。

开发时必须使用独立 `git worktree` 分支，不直接修改 `dev`。合并回 `dev` 前必须先合并或
rebase 最新 `dev`，解决冲突并完成前后端规定的编译与测试。

## 17. 官方资料

- 阿里云 RDS `DescribeDBInstances`：
  <https://help.aliyun.com/zh/rds/developer-reference/api-rds-2014-08-15-describedbinstances>
- 阿里云 `AliyunRDSReadOnlyAccess`：
  <https://help.aliyun.com/zh/ram/developer-reference/aliyunrdsreadonlyaccess>
- 腾讯云 PostgreSQL API 概览：
  <https://cloud.tencent.com/document/product/409/16761>
- AWS RDS `DescribeDBInstances`：
  <https://docs.aws.amazon.com/AmazonRDS/latest/APIReference/API_DescribeDBInstances.html>
- AWS RDS 与 Secrets Manager：
  <https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-secrets-manager.html>

具体 API、权限和 SDK 版本在正式实施前必须重新核对官方最新文档。
