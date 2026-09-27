package service

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"jianmen/internal/objectstore"
)

func TestSystemSettingsDiagnosticServiceTestsGuacd(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan struct{})
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = connection.Close()
		}
		close(accepted)
	}()

	diagnostics := newTestSystemSettingsDiagnostics(t, listener.Addr().String())
	result := diagnostics.TestGuacd(context.Background())
	if !result.OK {
		t.Fatalf("TestGuacd() = %#v, want successful result", result)
	}
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("guacd diagnostic did not establish a connection")
	}
}

func TestSystemSettingsDiagnosticServiceReportsGuacdFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	diagnostics := newTestSystemSettingsDiagnostics(t, address)
	result := diagnostics.TestGuacd(context.Background())
	if result.OK || result.Message == "" {
		t.Fatalf("TestGuacd() = %#v, want failed result with message", result)
	}
}

func TestSystemSettingsDiagnosticServiceTestsObjectStorage(t *testing.T) {
	diagnostics := newTestSystemSettingsDiagnostics(t, "127.0.0.1:4822")
	result := diagnostics.TestObjectStorage(context.Background())
	if !result.OK {
		t.Fatalf("TestObjectStorage() = %#v, want successful result", result)
	}
}

func TestSystemSettingsDiagnosticServiceBoundsObjectStorageProbe(t *testing.T) {
	diagnostics, err := NewSystemSettingsDiagnosticService(
		SystemSettingsRuntimeInfrastructure{GuacdAddress: "127.0.0.1:4822"},
		blockingObjectStore{},
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewSystemSettingsDiagnosticService() error = %v", err)
	}
	diagnostics.objectTimeout = 20 * time.Millisecond
	startedAt := time.Now()

	result := diagnostics.TestObjectStorage(context.Background())

	if result.OK {
		t.Fatalf("TestObjectStorage() = %#v, want timeout failure", result)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("TestObjectStorage() elapsed = %v, want bounded probe", elapsed)
	}
}

type blockingObjectStore struct{}

func (blockingObjectStore) Put(
	ctx context.Context,
	_ string,
	_ io.Reader,
	_ int64,
	_ string,
) (objectstore.Info, error) {
	<-ctx.Done()
	return objectstore.Info{}, ctx.Err()
}

func (blockingObjectStore) Open(context.Context, string) (objectstore.Reader, error) {
	return nil, objectstore.ErrNotFound
}

func (blockingObjectStore) Stat(context.Context, string) (objectstore.Info, error) {
	return objectstore.Info{}, objectstore.ErrNotFound
}

func (blockingObjectStore) Delete(context.Context, string) error {
	return nil
}

func newTestSystemSettingsDiagnostics(
	t *testing.T,
	guacdAddress string,
) *SystemSettingsDiagnosticService {
	t.Helper()
	objects, err := objectstore.New(context.Background(), objectstore.Config{
		Provider: objectstore.ProviderFilesystem,
		LocalDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	diagnostics, err := NewSystemSettingsDiagnosticService(
		SystemSettingsRuntimeInfrastructure{GuacdAddress: guacdAddress},
		objects,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewSystemSettingsDiagnosticService() error = %v", err)
	}
	return diagnostics
}
