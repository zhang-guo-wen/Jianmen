# 密码加密存储 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将所有敏感凭证（SSH 密码、私钥、私钥口令、数据库密码）统一使用 AES-256-GCM 可逆加密存储，用户登录密码从 SHA-256 升级为 bcrypt，首次启动自动生成加密密钥并引导创建管理员。

**Architecture:** 新增 `internal/crypto` 包管理 AES-256-GCM 加解密和密钥生命周期；新增 `model.EncryptedField` 类型实现 GORM Scanner/Valuer 透明加解密。对上层 store 和 proxy 层只需将 `string` 字段改为 `EncryptedField` 类型并用 `NewEncryptedField()`/`GetPlaintext()` 读写。前端新增 `/setup` 设置向导页，初始化模式下所有请求重定向到设置页。

**Tech Stack:** Go 标准库 crypto/aes + crypto/cipher + crypto/rand；golang.org/x/crypto/bcrypt；Vue 3 + Element Plus + TypeScript

## Global Constraints

- 开发使用 git worktree 隔离，不直接在项目目录修改
- 不兼容旧数据，部署时清空数据库文件重新开始
- config.json 的 users 数组清空，管理员统一通过设置向导创建
- 所有代码不引入第三方加密库，使用 Go 标准库
- 每次变更后必须通过: `go build ./...`、`go test ./... -count=1`、`npm run typecheck`、`npm run build`

---

### Task 1: 创建 crypto 包 — AES-256-GCM 加解密 + 密钥管理

**Files:**
- Create: `internal/crypto/crypto.go`
- Create: `internal/crypto/crypto_test.go`

**Interfaces:**
- Produces:
  - `func Init(dataDir string) (newKeyGenerated bool, err error)` — 加载或生成加密密钥
  - `func Encrypt(plaintext []byte) (string, error)` — AES-256-GCM 加密，返回 Base64 编码密文
  - `func Decrypt(encoded string) ([]byte, error)` — 解密，输入 Base64 编码密文
  - `func GetKey() []byte` — 返回当前主密钥（仅测试用）
  - `func KeyExists(dataDir string) bool` — 检查密钥文件是否存在

- [ ] **Step 1: 编写 crypto_test.go（TDD）**

```go
package crypto

import (
    "os"
    "path/filepath"
    "testing"
)

func TestEncryptDecrypt(t *testing.T) {
    // 使用临时密钥初始化
    tmpDir := t.TempDir()
    generated, err := Init(tmpDir)
    if err != nil {
        t.Fatalf("Init: %v", err)
    }
    if !generated {
        t.Fatal("expected new key to be generated")
    }

    plaintext := []byte("my-secret-password-123")
    encrypted, err := Encrypt(plaintext)
    if err != nil {
        t.Fatalf("Encrypt: %v", err)
    }
    if encrypted == "" {
        t.Fatal("encrypted is empty")
    }
    if encrypted == string(plaintext) {
        t.Fatal("encrypted equals plaintext")
    }

    decrypted, err := Decrypt(encrypted)
    if err != nil {
        t.Fatalf("Decrypt: %v", err)
    }
    if string(decrypted) != string(plaintext) {
        t.Fatalf("round-trip failed: got %q, want %q", decrypted, plaintext)
    }
}

func TestEncryptSamePlaintextDifferentCiphertext(t *testing.T) {
    tmpDir := t.TempDir()
    _, err := Init(tmpDir)
    if err != nil {
        t.Fatal(err)
    }

    plaintext := []byte("password")
    c1, _ := Encrypt(plaintext)
    c2, _ := Encrypt(plaintext)
    if c1 == c2 {
        t.Fatal("same plaintext produced identical ciphertext — nonce reuse?")
    }
}

func TestDecryptInvalidBase64(t *testing.T) {
    tmpDir := t.TempDir()
    _, _ = Init(tmpDir)

    _, err := Decrypt("not-valid-base64!!!")
    if err == nil {
        t.Fatal("expected error for invalid base64")
    }
}

func TestDecryptTamperedData(t *testing.T) {
    tmpDir := t.TempDir()
    _, _ = Init(tmpDir)

    encrypted, _ := Encrypt([]byte("secret"))
    tampered := encrypted[:len(encrypted)-2] + "AA"
    _, err := Decrypt(tampered)
    if err == nil {
        t.Fatal("expected error for tampered ciphertext (GCM auth failure)")
    }
}

func TestInitLoadsExistingKey(t *testing.T) {
    tmpDir := t.TempDir()
    keyPath := filepath.Join(tmpDir, "encryption.key")

    // 手动写入已知密钥
    if err := os.WriteFile(keyPath, []byte("0123456789abcdef0123456789abcdef"), 0600); err != nil {
        t.Fatal(err)
    }

    generated, err := Init(tmpDir)
    if err != nil {
        t.Fatalf("Init: %v", err)
    }
    if generated {
        t.Fatal("expected existing key to be loaded, not regenerated")
    }

    if len(GetKey()) != 32 {
        t.Fatalf("key length: got %d, want 32", len(GetKey()))
    }
}

func TestKeyExists(t *testing.T) {
    tmpDir := t.TempDir()
    if KeyExists(tmpDir) {
        t.Fatal("expected no key in empty dir")
    }
    if err := os.WriteFile(filepath.Join(tmpDir, "encryption.key"), make([]byte, 32), 0600); err != nil {
        t.Fatal(err)
    }
    if !KeyExists(tmpDir) {
        t.Fatal("expected key to exist")
    }
}

func TestEncryptDecryptEmptyPlaintext(t *testing.T) {
    tmpDir := t.TempDir()
    _, _ = Init(tmpDir)

    encrypted, err := Encrypt([]byte{})
    if err != nil {
        t.Fatalf("Encrypt empty: %v", err)
    }
    decrypted, err := Decrypt(encrypted)
    if err != nil {
        t.Fatalf("Decrypt empty: %v", err)
    }
    if len(decrypted) != 0 {
        t.Fatalf("empty round-trip: got %d bytes", len(decrypted))
    }
}

func TestEncryptDecryptBinaryData(t *testing.T) {
    tmpDir := t.TempDir()
    _, _ = Init(tmpDir)

    data := make([]byte, 256)
    for i := range data {
        data[i] = byte(i)
    }
    encrypted, _ := Encrypt(data)
    decrypted, _ := Decrypt(encrypted)
    if string(decrypted) != string(data) {
        t.Fatal("binary round-trip failed")
    }
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/crypto/ -v -count=1`
Expected: 编译失败（包不存在或函数未定义）

- [ ] **Step 3: 实现 crypto.go**

```go
package crypto

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

var masterKey []byte

// Init 从 dataDir/encryption.key 加载密钥，如果不存在则生成新密钥。
// 返回 true 表示生成了新密钥（系统进入初始化模式）。
func Init(dataDir string) (newKeyGenerated bool, err error) {
    keyPath := filepath.Join(dataDir, "encryption.key")

    // 先尝试读取已有密钥
    existing, readErr := os.ReadFile(keyPath)
    if readErr == nil {
        if len(existing) != 32 {
            return false, fmt.Errorf("encryption key must be 32 bytes, got %d", len(existing))
        }
        masterKey = make([]byte, 32)
        copy(masterKey, existing)
        return false, nil
    }

    if !errors.Is(readErr, os.ErrNotExist) {
        return false, fmt.Errorf("read encryption key: %w", readErr)
    }

    // 生成新密钥
    newKey := make([]byte, 32)
    if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
        return false, fmt.Errorf("generate encryption key: %w", err)
    }

    // 确保 dataDir 存在
    if err := os.MkdirAll(dataDir, 0700); err != nil {
        return false, fmt.Errorf("create data dir: %w", err)
    }

    if err := os.WriteFile(keyPath, newKey, 0600); err != nil {
        return false, fmt.Errorf("write encryption key: %w", err)
    }

    masterKey = make([]byte, 32)
    copy(masterKey, newKey)
    return true, nil
}

// KeyExists 检查密钥文件是否存在
func KeyExists(dataDir string) bool {
    _, err := os.Stat(filepath.Join(dataDir, "encryption.key"))
    return err == nil
}

// GetKey 返回当前主密钥（仅用于测试）
func GetKey() []byte {
    return masterKey
}

// Encrypt 使用 AES-256-GCM 加密明文，返回 Base64 编码的密文。
// 格式: Base64(Nonce(12字节) + Ciphertext + Tag(16字节))
func Encrypt(plaintext []byte) (string, error) {
    if len(masterKey) != 32 {
        return "", errors.New("crypto not initialized: master key is not set")
    }

    block, err := aes.NewCipher(masterKey)
    if err != nil {
        return "", fmt.Errorf("create AES cipher: %w", err)
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("create GCM: %w", err)
    }

    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", fmt.Errorf("generate nonce: %w", err)
    }

    // Seal 会追加密文和认证标签到 nonce 后面
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密 Base64 编码的密文，返回明文。
func Decrypt(encoded string) ([]byte, error) {
    if len(masterKey) != 32 {
        return nil, errors.New("crypto not initialized: master key is not set")
    }

    ciphertext, err := base64.StdEncoding.DecodeString(encoded)
    if err != nil {
        return nil, fmt.Errorf("decode base64: %w", err)
    }

    block, err := aes.NewCipher(masterKey)
    if err != nil {
        return nil, fmt.Errorf("create AES cipher: %w", err)
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("create GCM: %w", err)
    }

    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, fmt.Errorf("ciphertext too short: %d bytes, need at least %d", len(ciphertext), nonceSize)
    }

    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }

    return plaintext, nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/crypto/ -v -count=1`
Expected: 所有测试 PASS

- [ ] **Step 5: 确保编译通过**

Run: `go build ./internal/crypto/`
Expected: 无错误

- [ ] **Step 6: 提交**

```bash
git add internal/crypto/crypto.go internal/crypto/crypto_test.go
git commit -m "feat: add AES-256-GCM crypto package with key management"
```

---

### Task 2: 创建 EncryptedField 类型

**Files:**
- Create: `internal/model/encrypted.go`
- Create: `internal/model/encrypted_test.go`

**Interfaces:**
- Consumes: `crypto.Encrypt([]byte) (string, error)`, `crypto.Decrypt(string) ([]byte, error)`
- Produces:
  - `type EncryptedField struct` — 实现 `driver.Valuer`, `sql.Scanner`, `json.Marshaler`, `json.Unmarshaler`
  - `func NewEncryptedField(plaintext string) EncryptedField`
  - `func (e EncryptedField) GetPlaintext() string`
  - `func (e *EncryptedField) SetPlaintext(s string)`

- [ ] **Step 1: 编写 encrypted_test.go（TDD）**

```go
package model

import (
    "encoding/json"
    "testing"
)

func TestEncryptedField_Value_Scan_RoundTrip(t *testing.T) {
    original := NewEncryptedField("my-secret-password")
    val, err := original.Value()
    if err != nil {
        t.Fatalf("Value: %v", err)
    }
    if val == nil {
        t.Fatal("expected non-nil value for non-empty field")
    }

    var restored EncryptedField
    if err := restored.Scan(val); err != nil {
        t.Fatalf("Scan: %v", err)
    }
    if restored.GetPlaintext() != "my-secret-password" {
        t.Fatalf("round-trip: got %q", restored.GetPlaintext())
    }
}

func TestEncryptedField_EmptyValue_ReturnsNil(t *testing.T) {
    e := EncryptedField{}
    val, err := e.Value()
    if err != nil {
        t.Fatalf("Value: %v", err)
    }
    if val != nil {
        t.Fatal("expected nil for empty field")
    }
}

func TestEncryptedField_Scan_Nil_SetsEmpty(t *testing.T) {
    var e EncryptedField
    if err := e.Scan(nil); err != nil {
        t.Fatalf("Scan nil: %v", err)
    }
    if e.GetPlaintext() != "" {
        t.Fatalf("expected empty after scanning nil, got %q", e.GetPlaintext())
    }
}

func TestEncryptedField_Scan_EmptyBytes_SetsEmpty(t *testing.T) {
    var e EncryptedField
    if err := e.Scan([]byte{}); err != nil {
        t.Fatalf("Scan empty bytes: %v", err)
    }
    if e.GetPlaintext() != "" {
        t.Fatal("expected empty after scanning empty bytes")
    }
}

func TestEncryptedField_Scan_EmptyString_SetsEmpty(t *testing.T) {
    var e EncryptedField
    if err := e.Scan(""); err != nil {
        t.Fatalf("Scan empty string: %v", err)
    }
    if e.GetPlaintext() != "" {
        t.Fatal("expected empty after scanning empty string")
    }
}

func TestEncryptedField_Scan_InvalidJSON(t *testing.T) {
    var e EncryptedField
    err := e.Scan(12345) // int — 不支持的类型
    if err == nil {
        t.Fatal("expected error for unsupported scan type")
    }
}

func TestEncryptedField_MarshalJSON_Empty(t *testing.T) {
    e := NewEncryptedField("secret")
    data, err := json.Marshal(e)
    if err != nil {
        t.Fatalf("MarshalJSON: %v", err)
    }
    // 序列化为空字符串，泄露敏感信息
    if string(data) != `""` {
        t.Fatalf("expected empty JSON string, got %q", data)
    }
}

func TestEncryptedField_UnmarshalJSON(t *testing.T) {
    var e EncryptedField
    if err := json.Unmarshal([]byte(`"my-password"`), &e); err != nil {
        t.Fatalf("UnmarshalJSON: %v", err)
    }
    if e.GetPlaintext() != "my-password" {
        t.Fatalf("unmarshal: got %q", e.GetPlaintext())
    }
}

func TestEncryptedField_UnmarshalJSON_Empty(t *testing.T) {
    var e EncryptedField
    if err := json.Unmarshal([]byte(`""`), &e); err != nil {
        t.Fatalf("UnmarshalJSON empty: %v", err)
    }
    if e.GetPlaintext() != "" {
        t.Fatalf("expected empty after unmarshal, got %q", e.GetPlaintext())
    }
}

func TestEncryptedField_SetGetPlaintext(t *testing.T) {
    var e EncryptedField
    e.SetPlaintext("new-value")
    if e.GetPlaintext() != "new-value" {
        t.Fatal("SetPlaintext/GetPlaintext mismatch")
    }
}

func TestEncryptedField_DifferentInstancesEncryptDifferently(t *testing.T) {
    e1 := NewEncryptedField("password")
    e2 := NewEncryptedField("password")

    v1, _ := e1.Value()
    v2, _ := e2.Value()

    if v1.(string) == v2.(string) {
        t.Fatal("same plaintext should produce different ciphertext (unique nonce)")
    }
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/model/ -run TestEncryptedField -v -count=1`
Expected: 编译失败

- [ ] **Step 3: 实现 encrypted.go**

```go
package model

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"

    "jianmen/internal/crypto"
)

// EncryptedField 自动加解密的字符串字段。
// 写入数据库时通过 driver.Valuer 自动 AES-256-GCM 加密，
// 从数据库读取时通过 sql.Scanner 自动解密。
// JSON 序列化不输出明文（MarshalJSON 返回空字符串）。
type EncryptedField struct {
    plaintext string
}

// NewEncryptedField 创建包含明文的 EncryptedField。
func NewEncryptedField(plaintext string) EncryptedField {
    return EncryptedField{plaintext: plaintext}
}

// GetPlaintext 返回明文字符串。
func (e EncryptedField) GetPlaintext() string {
    return e.plaintext
}

// SetPlaintext 设置明文字符串。
func (e *EncryptedField) SetPlaintext(s string) {
    e.plaintext = s
}

// Value 实现 driver.Valuer。写入数据库时自动加密。
// 空字段返回 nil（数据库 NULL 值）。
func (e EncryptedField) Value() (driver.Value, error) {
    if e.plaintext == "" {
        return nil, nil
    }
    return crypto.Encrypt([]byte(e.plaintext))
}

// Scan 实现 sql.Scanner。从数据库读取时自动解密。
func (e *EncryptedField) Scan(src interface{}) error {
    if src == nil {
        e.plaintext = ""
        return nil
    }

    var s string
    switch v := src.(type) {
    case string:
        s = v
    case []byte:
        s = string(v)
    default:
        return fmt.Errorf("EncryptedField.Scan: unsupported type %T", src)
    }

    if s == "" {
        e.plaintext = ""
        return nil
    }

    plaintext, err := crypto.Decrypt(s)
    if err != nil {
        return fmt.Errorf("EncryptedField.Scan: decrypt: %w", err)
    }
    e.plaintext = string(plaintext)
    return nil
}

// MarshalJSON 实现 json.Marshaler。不输出明文。
func (e EncryptedField) MarshalJSON() ([]byte, error) {
    return json.Marshal("")
}

// UnmarshalJSON 实现 json.Unmarshaler。接收明文字符串。
func (e *EncryptedField) UnmarshalJSON(data []byte) error {
    var s string
    if err := json.Unmarshal(data, &s); err != nil {
        return err
    }
    e.plaintext = s
    return nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/model/ -run TestEncryptedField -v -count=1`
Expected: 所有测试 PASS

- [ ] **Step 5: 确保编译通过**

Run: `go build ./internal/model/`
Expected: 无错误（可能有其他模型引用的编译错误，待后续任务修复）

- [ ] **Step 6: 提交**

```bash
git add internal/model/encrypted.go internal/model/encrypted_test.go
git commit -m "feat: add EncryptedField type with automatic encrypt/decrypt"
```

---

### Task 3: 更新 HostAccount 和 DatabaseAccount 模型字段

**Files:**
- Modify: `internal/model/core.go` — HostAccount.Password, PrivateKeyPEM, Passphrase 字段类型
- Modify: `internal/model/core.go` — DatabaseAccount.UpstreamPassword 字段类型

**Interfaces:**
- Consumes: `model.EncryptedField`, `model.NewEncryptedField()`, `EncryptedField.GetPlaintext()`

- [ ] **Step 1: 修改 HostAccount 结构体**

在 `internal/model/core.go`，将 HostAccount 的 Password、PrivateKeyPEM、Passphrase 字段从 `string` 改为 `EncryptedField`：

```go
type HostAccount struct {
    ID            string         `gorm:"primaryKey;size:64" json:"id"`
    HostID        string         `gorm:"index;size:64;not null" json:"host_id"`
    Username      string         `gorm:"size:128;not null" json:"username"`
    AuthType      string         `gorm:"size:32" json:"auth_type,omitempty"`
    Password      EncryptedField `gorm:"type:text" json:"-"`
    PrivateKeyPEM EncryptedField `gorm:"type:text" json:"-"`
    Passphrase    EncryptedField `gorm:"type:text" json:"-"`
    CredentialRef string         `gorm:"size:255" json:"credential_ref,omitempty"`
    // ... 其余字段不变
}
```

- [ ] **Step 2: 修改 DatabaseAccount 结构体**

```go
type DatabaseAccount struct {
    ID               string         `gorm:"primaryKey;size:64" json:"id"`
    InstanceID       string         `gorm:"index;size:64;not null" json:"instance_id"`
    UniqueName       string         `gorm:"uniqueIndex;size:128;not null" json:"unique_name"`
    UpstreamUsername string         `gorm:"size:128;not null" json:"upstream_username"`
    UpstreamPassword EncryptedField `gorm:"type:text" json:"-"`
    // ... 其余字段不变
}
```

- [ ] **Step 3: 编译检查**

Run: `go build ./internal/model/`
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/model/core.go
git commit -m "feat: change HostAccount and DatabaseAccount sensitive fields to EncryptedField"
```

---

### Task 4: 更新 DBStore — 使用 EncryptedField 构造和读取

**Files:**
- Modify: `internal/store/dbstore.go` — AddTarget, UpdateTarget, AddDatabaseAccount, UpdateDatabaseAccount, targetConfig

**Interfaces:**
- Consumes: `model.NewEncryptedField()`, `EncryptedField.GetPlaintext()`, `EncryptedField.SetPlaintext()`

- [ ] **Step 1: 修改 `targetConfig()` 方法 — 读取时调用 GetPlaintext()**

找到 `targetConfig` 函数（约第254-273行），把 Password、PrivateKeyPEM、Passphrase 的赋值改为 `.GetPlaintext()`：

```go
func (s *DBStore) targetConfig(a model.HostAccount) TargetConfig {
    return TargetConfig{
        // ...
        Password:      a.Password.GetPlaintext(),
        PrivateKeyPEM: a.PrivateKeyPEM.GetPlaintext(),
        Passphrase:    a.Passphrase.GetPlaintext(),
        // ... 其他字段不变
    }
}
```

- [ ] **Step 2: 修改 `AddTarget()` — 写入时使用 NewEncryptedField()**

找到 `AddTarget` 函数（约第307-343行），把 `Password: target.Password` 改为 `Password: model.NewEncryptedField(target.Password)`，对 PrivateKeyPEM 和 Passphrase 同样处理：

```go
a := model.HostAccount{
    // ...
    Password:      model.NewEncryptedField(target.Password),
    PrivateKeyPEM: model.NewEncryptedField(target.PrivateKeyPEM),
    Passphrase:    model.NewEncryptedField(target.Passphrase),
    // ... 其余字段不变
}
```

同时注意第327-331行通过 `os.ReadFile` 读取私钥文件的地方：
```go
if pem, err := os.ReadFile(target.PrivateKeyPath); err == nil {
    a.PrivateKeyPEM = model.NewEncryptedField(string(pem))
}
```

- [ ] **Step 3: 修改 `UpdateTarget()` — 同理**

找到 `UpdateTarget` 函数（约第345-371行），修改赋值方式：

```go
a.Username = target.Username
a.AuthType = "password"
a.Password = model.NewEncryptedField(target.Password)
a.PrivateKeyPEM = model.NewEncryptedField(target.PrivateKeyPEM)
a.Passphrase = model.NewEncryptedField(target.Passphrase)
// os.ReadFile 的地方同样修改
if pem, err := os.ReadFile(target.PrivateKeyPath); err == nil {
    a.PrivateKeyPEM = model.NewEncryptedField(string(pem))
}
```

- [ ] **Step 4: 修改 `AddDatabaseAccount()` — 同理**

找到 `AddDatabaseAccount`（约第413-451行）：

```go
acct := model.DatabaseAccount{
    // ...
    UpstreamPassword: model.NewEncryptedField(upstreamPassword),
    // ... 其余字段不变
}
```

- [ ] **Step 5: 修改 `UpdateDatabaseAccount()` — 同理**

找到 `UpdateDatabaseAccount`（约第453-477行）：

```go
if upstreamPassword != "" {
    acct.UpstreamPassword = model.NewEncryptedField(upstreamPassword)
}
```

- [ ] **Step 6: 编译检查**

Run: `go build ./internal/store/`
Expected: 编译通过

- [ ] **Step 7: 提交**

```bash
git add internal/store/dbstore.go
git commit -m "refactor: adapt DBStore to use EncryptedField for sensitive fields"
```

---

### Task 5: 更新 StaticAdapter — 同步适配 EncryptedField

**Files:**
- Modify: `internal/access/static.go` — AddTarget, UpdateTarget 中使用 EncryptedField
- Modify: `internal/store/static_adapter.go` — 如需要

**Interfaces:**
- Consumes: `model.NewEncryptedField()`, `EncryptedField.GetPlaintext()`

- [ ] **Step 1: 修改 `access/static.go` 的 AddTarget**

在 `access/static.go` 找到 `AddTarget` 方法（约第846-873行），将直接赋值改为 EncryptedField：

```go
a := model.HostAccount{
    Password:      model.NewEncryptedField(target.Password),
    PrivateKeyPEM: model.NewEncryptedField(target.PrivateKeyPEM),
    Passphrase:    model.NewEncryptedField(target.Passphrase),
    // ...
}
```

- [ ] **Step 2: 修改 UpdateTarget**

```go
a.Password = model.NewEncryptedField(target.Password)
a.PrivateKeyPEM = model.NewEncryptedField(target.PrivateKeyPEM)
a.Passphrase = model.NewEncryptedField(target.Passphrase)
```

- [ ] **Step 3: 修改读取路径** — 搜索所有访问 `a.Password`、`a.PrivateKeyPEM`、`a.Passphrase` 为 string 的地方，改为 `.GetPlaintext()`

- [ ] **Step 4: 编译检查**

Run: `go build ./internal/access/ ./internal/store/`
Expected: 编译通过

- [ ] **Step 5: 提交**

```bash
git add internal/access/static.go internal/store/static_adapter.go
git commit -m "refactor: adapt StaticAdapter to use EncryptedField"
```

---

### Task 6: 升级用户密码哈希 — SHA-256 → bcrypt

**Files:**
- Modify: `internal/store/dbstore.go` — Authenticate, AuthenticateDirect 方法
- Modify: `internal/server/admin/server.go` — withAuthAndUser 中间件

**Interfaces:**
- Consumes: `golang.org/x/crypto/bcrypt`

- [ ] **Step 1: 修改 `Authenticate()` 方法**

在 `internal/store/dbstore.go` 找到 `Authenticate` 方法（约第34-50行）。

当前使用 SHA-256 比对 TokenHash 和 PasswordHash。改为：
- Token 认证保持 SHA-256（token 是 API key，不需要 bcrypt）
- Password 认证改为 bcrypt

先找到 compact login 认证的内部方法。查找 `authenticateCompact`（约第52-81行）：

如果该函数使用 `sha256.Sum256` 比对密码，改为 `bcrypt.CompareHashAndPassword`：

```go
// 将密码哈希比对改为 bcrypt
if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
    return model.User{}, ErrAuthFailed
}
```

- [ ] **Step 2: 修改 `AuthenticateDirect()` 方法**

找到 `AuthenticateDirect` 方法，同样将 password 验证改为 bcrypt：

```go
if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
    return model.User{}, ErrAuthFailed
}
```

- [ ] **Step 3: 修改 bootstrap.go 中的密码哈希**

在 `internal/storage/bootstrap.go` 找到 `bootstrapConfigUsers` 函数（约第58-109行）。由于 config.json 的 users 数组已清空，该函数不会再创建用户。但保留函数结构，密码哈希方式改为 bcrypt：

```go
if pw := strings.TrimSpace(cfgUser.Password); pw != "" {
    hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
    if err != nil {
        return fmt.Errorf("hash password for %s: %w", username, err)
    }
    user.PasswordHash = string(hash)
}
```

- [ ] **Step 4: 编译检查**

Run: `go build ./internal/store/ ./internal/storage/ ./internal/server/admin/`
Expected: 编译通过

- [ ] **Step 5: 提交**

```bash
git add internal/store/dbstore.go internal/storage/bootstrap.go
git commit -m "refactor: upgrade password hashing from SHA-256 to bcrypt"
```

---

### Task 7: 创建初始化 API 处理器

**Files:**
- Create: `internal/server/admin/init.go`
- Modify: `internal/server/admin/server.go` — 添加 initRoutes 路由注册

**Interfaces:**
- Consumes: `crypto.Init()`, `crypto.KeyExists()`, `store.Store`, `gorm.DB`
- Produces:
  - `GET /api/init/status` — `{"initialized": bool}`
  - `POST /api/init/setup` — `{"username", "password", "email"}` → `{"token": string}`
  - `GET /api/init/encryption-key` — 返回密钥 hex 编码

- [ ] **Step 1: 编写 init.go**

```go
package admin

import (
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "golang.org/x/crypto/bcrypt"

    "jianmen/internal/crypto"
    "jianmen/internal/model"
    "jianmen/internal/util"
    "gorm.io/gorm"
)

// Server 中新增字段（在 server.go 的 Server 结构体中添加）：
// dataDir string — 数据目录路径

type InitStatusResponse struct {
    Initialized bool `json:"initialized"`
}

type SetupRequest struct {
    Username string `json:"username"`
    Password string `json:"password"`
    Email    string `json:"email"`
}

type SetupResponse struct {
    Token string `json:"token"`
}

type EncryptionKeyResponse struct {
    Key string `json:"key"`
}

// handleInitStatus 返回系统初始化状态
func (s *Server) handleInitStatus(w http.ResponseWriter, r *http.Request) {
    var count int64
    s.db.Model(&model.User{}).Count(&count)
    writeJSON(w, http.StatusOK, InitStatusResponse{Initialized: count > 0})
}

// handleInitSetup 创建超级管理员用户
func (s *Server) handleInitSetup(w http.ResponseWriter, r *http.Request) {
    // 检查是否已初始化
    var count int64
    s.db.Model(&model.User{}).Count(&count)
    if count > 0 {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "already initialized"})
        return
    }

    var req SetupRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
        return
    }

    username := strings.TrimSpace(req.Username)
    password := strings.TrimSpace(req.Password)
    email := strings.TrimSpace(req.Email)

    if username == "" || password == "" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
        return
    }
    if len(password) < 8 {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
        return
    }

    // bcrypt 哈希密码
    passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
        return
    }

    // 生成 API token
    tokenBytes := make([]byte, 32)
    if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
        return
    }
    token := hex.EncodeToString(tokenBytes)

    // SHA-256 哈希 token 存储
    tokenHash := sha256.Sum256([]byte(token))
    tokenHashStr := hex.EncodeToString(tokenHash[:])

    user := model.User{
        ID:           util.NewID(),
        Username:     username,
        PasswordHash: string(passwordHash),
        TokenHash:    tokenHashStr,
        Email:        email,
        Status:       "active",
    }

    if err := s.db.Create(&user).Error; err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user: " + err.Error()})
        return
    }

    // 分配 admin 角色（如果内置角色已存在）
    assignAdminRole(s.db, user.ID)

    writeJSON(w, http.StatusCreated, SetupResponse{Token: token})
}

// handleInitEncryptionKey 返回加密密钥（一次性）
func (s *Server) handleInitEncryptionKey(w http.ResponseWriter, r *http.Request) {
    // 检查是否已初始化
    var count int64
    s.db.Model(&model.User{}).Count(&count)
    if count == 0 {
        writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "setup not completed"})
        return
    }

    keyPath := filepath.Join(s.dataDir, "encryption.key")
    keyData, err := os.ReadFile(keyPath)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read encryption key"})
        return
    }

    // 读取后立即删除，确保一次性
    // 注意：密钥文件本身保留（用于后续重启），我们通过另一个标记文件控制一次性显示
    markerPath := filepath.Join(s.dataDir, ".encryption_key_shown")
    if _, err := os.Stat(markerPath); err == nil {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "encryption key has already been retrieved"})
        return
    }

    // 写入标记文件
    if err := os.WriteFile(markerPath, []byte("1"), 0600); err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to mark key as shown"})
        return
    }

    writeJSON(w, http.StatusOK, EncryptionKeyResponse{
        Key: hex.EncodeToString(keyData),
    })
}

// assignAdminRole 为新创建的管理员用户分配 builtin-admin 角色
func assignAdminRole(db *gorm.DB, userID string) {
    var adminRole model.Role
    if err := db.Where("name = ?", "builtin-admin").First(&adminRole).Error; err != nil {
        return // 角色不存在就跳过
    }
    userRole := model.UserRole{
        ID:     util.NewID(),
        UserID: userID,
        RoleID: adminRole.ID,
    }
    db.Create(&userRole)
}
```

- [ ] **Step 2: 在 server.go 的 Server 结构体中添加 dataDir 字段**

```go
type Server struct {
    cfg    *config.Config
    store  store.Store
    logger *slog.Logger
    db     *gorm.DB
    dataDir string  // 新增
}
```

同时更新 `New()` 构造函数接受 `dataDir` 参数。

- [ ] **Step 3: 在 ListenAndServe 中注册 init 路由**

在 `ListenAndServe` 方法的 `mux.HandleFunc` 注册区域（约第82-110行），添加：

```go
mux.HandleFunc("/api/init/status", s.handleInitStatus)
mux.HandleFunc("/api/init/setup", s.handleInitSetup)
mux.HandleFunc("/api/init/encryption-key", s.handleInitEncryptionKey)
```

这些路由不使用 `withAuthAndUser` 中间件，因为是公开访问的。

- [ ] **Step 4: 编译检查**

Run: `go build ./internal/server/admin/`
Expected: 编译通过

- [ ] **Step 5: 提交**

```bash
git add internal/server/admin/init.go internal/server/admin/server.go
git commit -m "feat: add init API endpoints for setup wizard"
```

---

### Task 8: 更新 main.go — 启动顺序和 crypto 初始化

**Files:**
- Modify: `cmd/bastion-core/main.go`

**Interfaces:**
- Consumes: `crypto.Init()`, `admin.New()` 新增的 `dataDir` 参数

- [ ] **Step 1: 修改启动顺序**

在 `cmd/bastion-core/main.go` 中，将 `crypto.Init()` 调用插入到加载配置之后、打开数据库之前：

```go
func main() {
    configPath := flag.String("config", "config.json", "path to config file")
    flag.Parse()

    cfg, err := config.Load(*configPath)
    if err != nil {
        slog.Error("load config", "error", err)
        os.Exit(1)
    }

    // NEW: 初始化加密密钥（在任何数据库操作之前）
    dataDir := filepath.Dir(cfg.Database.DSN)
    if dataDir == "." || dataDir == "" {
        dataDir = "data"
    }
    newKeyGenerated, err := crypto.Init(dataDir)
    if err != nil {
        slog.Error("init crypto", "error", err)
        os.Exit(1)
    }
    if newKeyGenerated {
        slog.Info("generated new encryption key", "path", filepath.Join(dataDir, "encryption.key"))
        slog.Info("please save this key file, it is required to decrypt stored credentials")
    }

    // 数据库初始化
    db, err := storage.Open(cfg.Database)
    // ...
    storage.AutoMigrate(db)
    storage.BootstrapMetadata(db, cfg)

    // 创建 Store
    // ...

    // 创建 admin server（传入 dataDir）
    adminSrv := admin.New(cfg, appStore, logger, metadataDB, dataDir)
    // ...
}
```

- [ ] **Step 2: 编译检查**

Run: `go build ./cmd/bastion-core/`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add cmd/bastion-core/main.go
git commit -m "feat: integrate crypto init into startup order"
```

---

### Task 9: 更新 Proxy 层 — SSH 客户端配置使用 GetPlaintext()

**Files:**
- Modify: `internal/store/client.go` — ClientConfigForTarget 保持字符串接口，确认上游已传明文
- (Optional) 检查其他直接访问 model.HostAccount 字段的代码

**Interfaces:**
- Consumes: `TargetConfig.Password`（已是 string，由 targetConfig() 方法中的 GetPlaintext() 填充）

- [ ] **Step 1: 确认 ClientConfigForTarget 无需修改**

由于 `targetConfig()` 方法已经在返回 TargetConfig 时调用了 `.GetPlaintext()`，`ClientConfigForTarget` 接收到的 `TargetConfig.Password` 仍然是 `string` 类型。检查 `internal/store/client.go` 确认无需修改。

- [ ] **Step 2: 搜索所有直接访问 `hostAccount.Password` 等字段的地方**

Run: `rg "\.Password[^H]" internal/` (排除 PasswordHash)
检查是否有代码直接访问 `model.HostAccount.Password` 作为 string 使用，如果有则改为 `.GetPlaintext()`

- [ ] **Step 3: 搜索数据库代理中的密码使用**

Run: `rg "UpstreamPassword" internal/proxy/`
检查数据库代理是否直接访问 `DatabaseAccount.UpstreamPassword`，改为 `.GetPlaintext()`

- [ ] **Step 4: 编译检查**

Run: `go build ./...`
Expected: 全部编译通过

- [ ] **Step 5: 提交**

```bash
git add internal/proxy/ internal/store/client.go
git commit -m "refactor: adapt proxy layer to use GetPlaintext() for encrypted fields"
```

---

### Task 10: 创建前端设置向导页面

**Files:**
- Create: `web/src/views/SetupView.vue`

**Interfaces:**
- Consumes: 新增的 init API

- [ ] **Step 1: 创建 SetupView.vue 单文件组件**

```vue
<template>
  <div class="setup-container">
    <el-card class="setup-card">
      <!-- 步骤 1: 创建管理员 -->
      <template v-if="step === 1">
        <div class="setup-header">
          <h2>{{ t('setup.title') }}</h2>
          <p class="setup-desc">{{ t('setup.description') }}</p>
        </div>
        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-position="top"
          @submit.prevent="handleSetup"
        >
          <el-form-item :label="t('setup.username')" prop="username">
            <el-input v-model="form.username" autocomplete="username" />
          </el-form-item>
          <el-form-item :label="t('setup.email')" prop="email">
            <el-input v-model="form.email" type="email" autocomplete="email" />
          </el-form-item>
          <el-form-item :label="t('setup.password')" prop="password">
            <el-input
              v-model="form.password"
              type="password"
              show-password
              autocomplete="new-password"
            />
          </el-form-item>
          <el-form-item :label="t('setup.confirmPassword')" prop="confirmPassword">
            <el-input
              v-model="form.confirmPassword"
              type="password"
              show-password
              autocomplete="new-password"
            />
          </el-form-item>
          <el-form-item>
            <el-button
              type="primary"
              native-type="submit"
              :loading="submitting"
              class="setup-submit-btn"
            >
              {{ t('setup.submit') }}
            </el-button>
          </el-form-item>
        </el-form>
      </template>

      <!-- 步骤 2: 显示加密密钥和 API Token -->
      <template v-else-if="step === 2">
        <div class="setup-success">
          <el-icon :size="48" color="#67c23a"><CircleCheckFilled /></el-icon>
          <h3>{{ t('setup.success') }}</h3>
        </div>

        <div class="key-section">
          <h4>{{ t('setup.apiToken') }}</h4>
          <p class="key-hint">{{ t('setup.apiTokenHint') }}</p>
          <div class="key-display">
            <code>{{ apiToken }}</code>
            <el-button size="small" @click="copyToken">
              {{ t('setup.copy') }}
            </el-button>
          </div>
        </div>

        <div class="key-section">
          <h4>{{ t('setup.encryptionKey') }}</h4>
          <p class="key-hint warning">{{ t('setup.encryptionKeyHint') }}</p>
          <div class="key-display">
            <code>{{ encryptionKey }}</code>
            <el-button size="small" type="warning" @click="copyKey">
              {{ t('setup.copy') }}
            </el-button>
          </div>
          <el-alert
            type="warning"
            :title="t('setup.encryptionKeyWarning')"
            :closable="false"
            show-icon
            style="margin-top: 12px"
          />
        </div>

        <el-button type="primary" class="setup-submit-btn" @click="handleFinish">
          {{ t('setup.goToLogin') }}
        </el-button>
      </template>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue';
import { useRouter } from 'vue-router';
import { CircleCheckFilled } from '@element-plus/icons-vue';
import type { FormInstance, FormRules } from 'element-plus';
import { useI18n } from '@/i18n';
import { apiClient } from '@/api/client';
import { setToken } from '@/api/client';

const { t } = useI18n();
const router = useRouter();

const step = ref(1);
const submitting = ref(false);
const apiToken = ref('');
const encryptionKey = ref('');
const formRef = ref<FormInstance>();

const form = reactive({
  username: '',
  email: '',
  password: '',
  confirmPassword: '',
});

const validateConfirm = (_rule: unknown, value: string, callback: (err?: Error) => void) => {
  if (value !== form.password) {
    callback(new Error(t('setup.passwordMismatch')));
  } else {
    callback();
  }
};

const rules: FormRules = {
  username: [
    { required: true, message: 'Required', trigger: 'blur' },
    { min: 2, max: 64, message: '2-64 characters', trigger: 'blur' },
  ],
  email: [
    { type: 'email', message: 'Invalid email', trigger: 'blur' },
  ],
  password: [
    { required: true, message: 'Required', trigger: 'blur' },
    { min: 8, message: 'At least 8 characters', trigger: 'blur' },
  ],
  confirmPassword: [
    { required: true, message: 'Required', trigger: 'blur' },
    { validator: validateConfirm, trigger: 'blur' },
  ],
};

async function handleSetup() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  submitting.value = true;
  try {
    // Step 1: 创建管理员
    const setupResult = await apiClient.request<{ token: string }>('/api/init/setup', {
      method: 'POST',
      body: JSON.stringify({
        username: form.username.trim(),
        password: form.password,
        email: form.email.trim(),
      }),
    });
    
    const token = (setupResult as { token: string }).token || '';
    apiToken.value = token;

    // Step 2: 获取加密密钥
    const keyResult = await apiClient.request<{ key: string }>('/api/init/encryption-key');
    encryptionKey.value = (keyResult as { key: string }).key || '';

    step.value = 2;
  } catch (err: unknown) {
    // 错误处理
  } finally {
    submitting.value = false;
  }
}

async function copyToken() {
  await navigator.clipboard.writeText(apiToken.value);
}

async function copyKey() {
  await navigator.clipboard.writeText(encryptionKey.value);
}

function handleFinish() {
  // 将 API token 存入 localStorage
  setToken(apiToken.value);
  router.replace('/dashboard');
}
</script>

<style scoped>
.setup-container {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-bg-color-page);
}
.setup-card {
  width: 480px;
  max-width: 90vw;
}
.setup-header {
  text-align: center;
  margin-bottom: 24px;
}
.setup-header h2 {
  margin-bottom: 8px;
}
.setup-desc {
  color: var(--el-text-color-secondary);
}
.setup-submit-btn {
  width: 100%;
}
.setup-success {
  text-align: center;
  margin-bottom: 24px;
}
.key-section {
  margin-bottom: 20px;
}
.key-section h4 {
  margin-bottom: 4px;
}
.key-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}
.key-hint.warning {
  color: var(--el-color-warning);
}
.key-display {
  display: flex;
  gap: 8px;
  align-items: center;
}
.key-display code {
  flex: 1;
  padding: 8px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 12px;
  word-break: break-all;
  user-select: all;
}
</style>
```

- [ ] **Step 2: 添加国际化文案**

在 `web/src/i18n/` 中找到翻译文件（如 `zh-CN.ts`、`en-US.ts`），添加以下翻译 key：

```typescript
// zh-CN
setup: {
  title: '初始化设置',
  description: '创建超级管理员账号以开始使用 Jianmen',
  username: '用户名',
  email: '邮箱',
  password: '密码',
  confirmPassword: '确认密码',
  submit: '创建管理员',
  success: '设置完成',
  apiToken: 'API Token',
  apiTokenHint: '请保存此 Token，用于登录堡垒机管理后台',
  encryptionKey: '加密密钥',
  encryptionKeyHint: '请立即复制并安全保存此密钥',
  encryptionKeyWarning: '此密钥仅显示一次，丢失后无法恢复已加密的账号密码',
  copy: '复制',
  goToLogin: '我已保存，前往登录',
  passwordMismatch: '两次输入的密码不一致',
}
```

- [ ] **Step 3: 提交**

```bash
git add web/src/views/SetupView.vue web/src/i18n/
git commit -m "feat: add setup wizard page with admin creation and key display"
```

---

### Task 11: 更新前端路由和初始化逻辑

**Files:**
- Modify: `web/src/router/index.ts` — 添加 /setup 路由 + 初始化守卫
- Modify: `web/src/views/LoginView.vue` — 添加初始化检测
- Create: `web/src/api/init.ts` — 初始化 API 调用

**Interfaces:**
- Produces: `/setup` 路由，初始化状态检查，路由守卫重定向

- [ ] **Step 1: 创建 `web/src/api/init.ts`**

```typescript
import { apiClient } from './client';

export interface InitStatusResponse {
  initialized: boolean;
}

export interface SetupRequest {
  username: string;
  password: string;
  email: string;
}

export interface SetupResponse {
  token: string;
}

export interface EncryptionKeyResponse {
  key: string;
}

export async function getInitStatus(): Promise<InitStatusResponse> {
  const result = await apiClient.request<InitStatusResponse>('/api/init/status');
  return result as InitStatusResponse;
}

export async function setupAdmin(payload: SetupRequest): Promise<SetupResponse> {
  const result = await apiClient.request<SetupResponse>('/api/init/setup', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
  return result as SetupResponse;
}

export async function getEncryptionKey(): Promise<EncryptionKeyResponse> {
  const result = await apiClient.request<EncryptionKeyResponse>('/api/init/encryption-key');
  return result as EncryptionKeyResponse;
}
```

- [ ] **Step 2: 修改路由配置 `web/src/router/index.ts`**

添加 /setup 路由和初始化守卫：

```typescript
import { getInitStatus } from '@/api/init';
import { getToken } from '@/api/client';

// 新增 setup 路由
{
  path: '/setup',
  name: 'setup',
  component: () => import('@/views/SetupView.vue'),
  meta: { public: true } satisfies AppRouteMeta,
},

// 修改 beforeEach 守卫
let initChecked = false;
let needsInit = false;

router.beforeEach(async (to) => {
  // setup 页面始终可访问
  if (to.name === 'setup') return true;

  // 首次检查初始化状态
  if (!initChecked) {
    try {
      const status = await getInitStatus();
      needsInit = !status.initialized;
    } catch {
      // 如果检查失败（网络问题），允许继续（用户可能已登录）
      needsInit = false;
    }
    initChecked = true;
  }

  // 未初始化则跳转 setup
  if (needsInit) {
    return { name: 'setup' };
  }

  // 公开路由或已登录
  if (to.meta.public || getToken()) {
    return true;
  }

  return {
    name: 'login',
    query: { redirect: to.fullPath },
  };
});
```

- [ ] **Step 3: 修改 LoginView.vue**

在 LoginView 的 script setup 中添加初始化检测（页面加载时自动跳转已由路由守卫处理，LoginView 不需要额外修改）。但需要确保 setup 路由不在 `isLoginRoute` 判断中。

- [ ] **Step 4: 修改 App.vue 的 `isLoginRoute` 判断**

在 App.vue 中，将 setup 路由也纳入公开布局：

```typescript
const isPublicRoute = computed(() => {
  return route.name === 'login' || route.name === 'setup';
});
```

并将模板中的 `v-if="isLoginRoute"` 改为 `v-if="isPublicRoute"`。

- [ ] **Step 5: 编译检查**

Run: `npm run typecheck`
Expected: 通过

Run: `npm run build`
Expected: 通过

- [ ] **Step 6: 提交**

```bash
git add web/src/router/index.ts web/src/views/LoginView.vue web/src/App.vue web/src/api/init.ts
git commit -m "feat: add /setup route with init state guard"
```

---

### Task 12: 清理 config.json

**Files:**
- Modify: `config.json` — 清空 users 数组

- [ ] **Step 1: 清空 config.json 的 users 数组**

在 `config.json` 中将 `"users"` 数组清空为 `[]`：

```json
{
  "users": []
}
```

- [ ] **Step 2: 提交**

```bash
git add config.json
git commit -m "chore: clear config.json users array (now managed via setup wizard)"
```

---

### Task 13: 全量集成验证

**Files:**
- 无新增文件，验证现有代码

- [ ] **Step 1: 后端编译和测试**

```bash
go build ./...
go test ./... -count=1
```

Expected: 编译通过，所有测试通过。

- [ ] **Step 2: 前端 typecheck 和 build**

```bash
npm run typecheck
npm run build
```

Expected: 全部通过。

- [ ] **Step 3: 修复所有编译和测试错误**

检查 Go 测试输出，修复任何失败的测试。常见问题：
- 测试中直接使用 `string` 赋值给 `EncryptedField` 字段 → 改为 `model.NewEncryptedField()`
- 测试中读取 `Password` 字段作为 `string` → 改为 `.GetPlaintext()`
- 未使用的 import → 移除

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "chore: fix compilation and test issues from EncryptedField migration"
```

---

### Task 14: 合并到 dev 分支

按照 CLAUDE.md 的合并准则：
1. 先 `git merge dev`（或 `git rebase dev`）拉取最新 dev
2. 解决冲突
3. 验证：`go build ./...`、`go test ./... -count=1`、`npm run typecheck`、`npm run build`
4. 合并回 dev

- [ ] **Step 1: 拉取最新 dev 并合并**

```bash
git fetch origin dev
git merge origin/dev
```

- [ ] **Step 2: 解决冲突**

如有冲突，手动解决。

- [ ] **Step 3: 全量验证**

```bash
go build ./...
go test ./... -count=1
cd web && npm run typecheck && npm run build && cd ..
```

- [ ] **Step 4: 合并回 dev**

```bash
git checkout dev
git merge <worktree-branch>
git push origin dev
```
