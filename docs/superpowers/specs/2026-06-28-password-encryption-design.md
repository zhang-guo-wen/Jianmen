# 密码加密存储设计方案

日期：2026-06-28

## 概述

将所有敏感凭证（SSH 密码、私钥、私钥口令、数据库密码）统一使用 AES-256-GCM 可逆加密存储。用户登录密码从 SHA-256 升级为 bcrypt。首次启动时自动生成加密密钥并引导创建管理员。

## 第一部分：加密架构与密钥管理

### 1.1 密钥生成与加载

```
服务启动 → 检测 data/encryption.key 是否存在
    ├── 不存在 → 生成 32 字节随机密钥(crypto/rand)
    │          → 写入 data/encryption.key(权限 0600)
    │          → 标记系统为"初始化模式"
    │          → 引导管理员完成首次设置
    └── 已存在 → 读取密钥 → 正常启动
```

### 1.2 加密算法

- **算法**: AES-256-GCM（认证加密）
- **密钥**: 32 字节（从 data/encryption.key 读取）
- **Nonce**: 每次加密随机生成 12 字节
- **存储格式**: `Base64(Nonce(12B) + Ciphertext + Tag(16B))`
- **Go 标准库**: `crypto/aes` + `crypto/cipher.NewGCM`

### 1.3 加密范围

| 字段 | 所在模型 | 加密方式 | 说明 |
|------|----------|----------|------|
| `HostAccount.Password` | HostAccount | AES-256-GCM | SSH 密码 |
| `HostAccount.PrivateKeyPEM` | HostAccount | AES-256-GCM | SSH 私钥内容 |
| `HostAccount.Passphrase` | HostAccount | AES-256-GCM | 私钥解锁口令 |
| `DatabaseAccount.UpstreamPassword` | DatabaseAccount | AES-256-GCM | 数据库连接密码 |
| `User.PasswordHash` | User | bcrypt | 管理员/用户登录密码（不可逆） |
| `TemporaryCredential.SecretHash` | TemporaryCredential | SHA-256（保持） | 临时凭证，有效期短 |

### 1.4 EncryptedField 类型

在 `internal/model/` 下新增 `encrypted.go`，定义 `EncryptedField` 结构体：

- 实现 `sql.Scanner` — 从数据库读取时自动解密
- 实现 `driver.Valuer` — 写入数据库时自动加密
- 实现 `json.Marshaler` — JSON 序列化不出现在响应中
- 实现 `json.Unmarshaler` — JSON 反序列化接收明文

对调用方暴露 `NewEncryptedField(plaintext)` 构造、`GetPlaintext()` 取值、`SetPlaintext()` 赋值。
加解密过程完全透明。

**零值处理：** `EncryptedField` 零值表示空字符串，`Valuer()` 返回 `nil`（数据库存 NULL），
`Scanner()` 遇到 NULL 或空字符串时设置空明文。确保空密码和未设置密码行为一致。

## 第二部分：首次设置向导

### 2.1 初始化模式

初始化模式判定：数据库中不存在任何 User 记录 → 系统未初始化。

初始化模式下，API 只开放三个端点：

| 端点 | 用途 |
|------|------|
| `GET /api/init/status` | 返回 `{ initialized: false }` |
| `POST /api/init/setup` | 创建超级管理员，参数: username, password, email |
| `GET /api/init/encryption-key` | 返回加密密钥文本。仅初始化期间可调用一次，调用后标记密钥已获取，后续返回 403 |

### 2.2 管理员创建流程

1. 浏览器访问堡垒机 → 自动检测初始化状态 → 跳转 `/setup`
2. 填写：用户名、登录密码、确认密码、邮箱
3. 提交 → API 用 bcrypt 哈希密码 → 创建管理员 User 记录
4. 返回成功 + `GET /api/init/encryption-key` 获取密钥
5. 前端展示密钥文本 + 复制按钮 + "密钥仅显示一次" 警告
6. 管理员保存密钥后点击"我已保存，前往登录"
7. 系统退出初始化模式 → 跳转登录页

### 2.3 密钥备份查看

- 后端日志输出：`加密密钥文件路径: data/encryption.key，请妥善保管此文件`
- `GET /api/init/encryption-key` 仅在初始化期间可用一次
- 管理后台后续可增加"查看密钥"功能（需验证管理员密码），不在本次范围

## 第三部分：数据模型变更

### 3.1 模型字段类型变更

```go
// HostAccount
type HostAccount struct {
    Password      model.EncryptedField `gorm:"type:text" json:"-"`
    PrivateKeyPEM model.EncryptedField `gorm:"type:text" json:"-"`
    Passphrase    model.EncryptedField `gorm:"type:text" json:"-"`
}

// DatabaseAccount
type DatabaseAccount struct {
    UpstreamPassword model.EncryptedField `gorm:"type:text" json:"-"`
}

// User — 新增 email 字段，密码字段长度不变，哈希算法变更
type User struct {
    Email        string `gorm:"size:255" json:"email,omitempty"`
    PasswordHash string `gorm:"size:255" json:"-"` // bcrypt hash
}
```

### 3.2 数据库字段类型

- 原来 `varchar(512)` 改为 `text`，因为加密+Base64 后长度会增加
- 不使用 `size` 限制，让 GORM 使用 `text` 类型

### 3.3 配置文件调整

`config.json` 中的 `users` 数组清空（不再从配置文件创建用户）。
管理员统一通过首次设置向导创建。

```json
{
  "users": []
}
```

### 3.4 启动初始化顺序

```
main() 启动顺序：
1. 加载 config.json
2. crypto.Init(dataDir)             // 生成或加载 encryption.key，设置包级密钥
3. 初始化数据库连接
4. 检查管理员是否存在 → 确定初始化模式
5. 启动 HTTP 服务器
```

`crypto.Init()` 必须在数据库操作之前调用，确保 `EncryptedField` 的 `Scanner/Valuer` 可用。

### 3.5 旧数据兼容

不做兼容。部署时删除旧数据库文件，重新开始。

## 第四部分：API 变更

### 4.1 新增端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/init/status` | 查询初始化状态 |
| POST | `/api/init/setup` | 创建超级管理员 |
| GET | `/api/init/encryption-key` | 获取加密密钥（一次性） |

### 4.2 现有端点

无需变更。请求/响应的 DTO 保持不变，加密在模型层透明完成。

### 4.3 登录端点变更

`POST /api/auth/login` — 密码验证从 SHA-256 比对升级为 bcrypt.CompareHashAndPassword。

## 第五部分：前端变更

### 5.1 新增页面

`web/src/views/SetupView.vue` — 首次设置向导页面：

- 步骤1：创建管理员表单（用户名、邮箱、密码、确认密码）
- 步骤2：展示加密密钥 + 复制按钮 + 警告提示
- 步骤3：点击"我已保存"后跳转登录页

### 5.2 路由变更

- 新增路由: `{ path: '/setup', component: SetupView, meta: { public: true } }`
- 路由守卫: 应用启动时检查 `GET /api/init/status`，若 `initialized: false` 则重定向到 `/setup`

### 5.3 现有页面

| 页面 | 变更 |
|------|------|
| `HostsView.vue` | 无变更 — 加密对前端透明 |
| `DatabaseView.vue` | 无变更 — 加密对前端透明 |
| `LoginView.vue` | 添加初始化检测，未初始化时跳转 `/setup` |

### 5.4 Loader 函数

新增 `web/src/api/init.ts`，提供 `getInitStatus()`、`setupAdmin()`、`getEncryptionKey()` 三个 API 调用。

## 第六部分：后端代码结构

### 6.1 新增/修改文件

```
internal/
├── model/
│   ├── encrypted.go          # 新增: EncryptedField 类型
│   ├── core.go                # 修改: 字段类型变更
│   └── session.go             # 修改: User.PasswordHash 注释
├── crypto/
│   └── crypto.go              # 新增: AES-256-GCM 加解密 + 密钥生成/加载
├── store/
│   └── dbstore.go             # 修改: 无需改动(driver.Valuer 自动加密)
├── server/
│   └── admin/
│       ├── server.go          # 修改: 添加初始化端点
│       └── init.go            # 新增: 初始化相关 handler
├── proxy/
│   └── ssh.go                 # 修改: 使用时调用 .GetPlaintext()
└── storage/
    └── bootstrap.go           # 修改: 去掉配置用户创建，改为仅初始化向导创建

web/src/
├── views/
│   └── SetupView.vue          # 新增: 设置向导页面
├── api/
│   └── init.ts                # 新增: 初始化 API 调用
└── router/
    └── index.ts               # 修改: 添加 /setup 路由 + 路由守卫
```

### 6.2 关键依赖

- Go 标准库 `crypto/aes`、`crypto/cipher`、`crypto/rand` — 加密核心
- Go 标准库 `crypto/sha256` — 临时凭证（保持）
- `golang.org/x/crypto/bcrypt` — 用户密码哈希（已有依赖）

不使用任何第三方加密库。全部使用 Go 标准库。

## 第七部分：测试要求

### 7.1 Go 单元测试

- `internal/crypto/crypto_test.go` — 加解密正确性、密钥生成、错误处理
- `internal/model/encrypted_test.go` — EncryptedField 的 Scanner/Valuer/Marshaler/Unmarshaler
- `internal/server/admin/init_test.go` — 初始化端点行为

### 7.2 集成验证

修改后必须通过：
- `go build ./...`
- `go test ./... -count=1`
- `npm run typecheck`
- `npm run build`
