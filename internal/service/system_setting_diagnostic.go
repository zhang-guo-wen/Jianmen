package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"jianmen/internal/objectstore"
)

type SystemSettingsRuntimeInfrastructure struct {
	GuacdAddress       string
	SpoolDir           string
	GuacdRecordingRoot string
	LocalDriveRoot     string
	GuacdDriveRoot     string
	ReplayDir          string
	ObjectStorage      SystemSettingsObjectStorageInfrastructure
}

type SystemSettingsObjectStorageInfrastructure struct {
	Provider                  string
	LocalDir                  string
	Endpoint                  string
	Bucket                    string
	Region                    string
	Prefix                    string
	Secure                    bool
	PathStyle                 bool
	AutoCreateBucket          bool
	AccessKeyIDConfigured     bool
	SecretAccessKeyConfigured bool
	SessionTokenConfigured    bool
}

type SystemSettingsDiagnosticResult struct {
	OK        bool
	Message   string
	LatencyMS int64
}

type SystemSettingsDiagnosticService struct {
	infrastructure SystemSettingsRuntimeInfrastructure
	objects        objectstore.Store
	dialTimeout    time.Duration
	objectTimeout  time.Duration
}

func NewSystemSettingsDiagnosticService(
	infrastructure SystemSettingsRuntimeInfrastructure,
	objects objectstore.Store,
	dialTimeout time.Duration,
) (*SystemSettingsDiagnosticService, error) {
	if strings.TrimSpace(infrastructure.GuacdAddress) == "" {
		return nil, fmt.Errorf("guacd address is required")
	}
	if objects == nil {
		return nil, fmt.Errorf("object storage is required")
	}
	if dialTimeout <= 0 {
		dialTimeout = 15 * time.Second
	}
	return &SystemSettingsDiagnosticService{
		infrastructure: infrastructure,
		objects:        objects,
		dialTimeout:    dialTimeout,
		objectTimeout:  15 * time.Second,
	}, nil
}

func (s *SystemSettingsDiagnosticService) Infrastructure() SystemSettingsRuntimeInfrastructure {
	return s.infrastructure
}

func (s *SystemSettingsDiagnosticService) TestGuacd(ctx context.Context) SystemSettingsDiagnosticResult {
	startedAt := time.Now()
	if ctx == nil {
		return diagnosticResult(startedAt, false, "guacd 检测上下文无效")
	}
	dialer := net.Dialer{Timeout: s.dialTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", s.infrastructure.GuacdAddress)
	if err != nil {
		return diagnosticResult(startedAt, false, "guacd 连接失败")
	}
	if err := connection.Close(); err != nil {
		return diagnosticResult(startedAt, false, "guacd 连接关闭失败")
	}
	return diagnosticResult(startedAt, true, "guacd 连接正常")
}

func (s *SystemSettingsDiagnosticService) TestObjectStorage(ctx context.Context) SystemSettingsDiagnosticResult {
	startedAt := time.Now()
	if ctx == nil {
		return diagnosticResult(startedAt, false, "对象存储检测上下文无效")
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.objectTimeout)
	defer cancel()
	payload := []byte("jianmen object storage connectivity probe")
	key, err := diagnosticObjectKey()
	if err != nil {
		return diagnosticResult(startedAt, false, "无法生成对象存储检测标识")
	}
	cleanup := func() bool {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(probeCtx), 10*time.Second)
		defer cancel()
		return s.objects.Delete(cleanupCtx, key) == nil
	}
	if _, err := s.objects.Put(
		probeCtx,
		key,
		bytes.NewReader(payload),
		int64(len(payload)),
		"text/plain",
	); err != nil {
		return objectStorageFailure(startedAt, "对象存储写入检测失败", cleanup())
	}
	info, err := s.objects.Stat(probeCtx, key)
	if err != nil || info.Size != int64(len(payload)) {
		return objectStorageFailure(startedAt, "对象存储元数据检测失败", cleanup())
	}
	reader, err := s.objects.Open(probeCtx, key)
	if err != nil {
		return objectStorageFailure(startedAt, "对象存储读取检测失败", cleanup())
	}
	content, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(content, payload) {
		return objectStorageFailure(startedAt, "对象存储内容校验失败", cleanup())
	}
	if !cleanup() {
		return diagnosticResult(startedAt, false, "对象存储检测对象清理失败")
	}
	return diagnosticResult(startedAt, true, "对象存储读写正常")
}

func diagnosticObjectKey() (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "diagnostics/system-settings/" + hex.EncodeToString(random) + ".probe", nil
}

func diagnosticResult(
	startedAt time.Time,
	ok bool,
	message string,
) SystemSettingsDiagnosticResult {
	latency := time.Since(startedAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	return SystemSettingsDiagnosticResult{OK: ok, Message: message, LatencyMS: latency}
}

func objectStorageFailure(
	startedAt time.Time,
	message string,
	cleaned bool,
) SystemSettingsDiagnosticResult {
	if !cleaned {
		message += "，且检测对象清理失败"
	}
	return diagnosticResult(startedAt, false, message)
}
