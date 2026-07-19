package objectstore

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestNewS3StoreConfiguresClientWithoutNetworkAccess(t *testing.T) {
	t.Parallel()

	store, err := New(context.Background(), Config{
		Provider:        " S3 ",
		Endpoint:        "127.0.0.1:9000",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		SessionToken:    "session-token",
		Bucket:          "rdp-recordings",
		Region:          "us-east-1",
		Prefix:          "/tenant/rdp/",
		Secure:          true,
		PathStyle:       true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	s3, ok := store.(*s3Store)
	if !ok {
		t.Fatalf("New() store type = %T, want *s3Store", store)
	}
	if s3.bucket != "rdp-recordings" {
		t.Fatalf("bucket = %q", s3.bucket)
	}
	if s3.prefix != "tenant/rdp" {
		t.Fatalf("prefix = %q", s3.prefix)
	}
	if s3.lookup != minio.BucketLookupPath {
		t.Fatalf("lookup = %v, want BucketLookupPath", s3.lookup)
	}
	if got := s3.client.EndpointURL().String(); got != "https://127.0.0.1:9000" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestNewS3StoreUsesAutomaticBucketLookupByDefault(t *testing.T) {
	t.Parallel()

	store, err := New(context.Background(), validS3Config())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	s3 := store.(*s3Store)
	if s3.lookup != minio.BucketLookupAuto {
		t.Fatalf("lookup = %v, want BucketLookupAuto", s3.lookup)
	}
	if got := s3.client.EndpointURL().String(); got != "http://127.0.0.1:9000" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestNewS3StoreRejectsInvalidConfigurationWithoutNetworkAccess(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name   string
		mutate func(*Config)
		ctx    context.Context
	}{
		{name: "missing endpoint", mutate: func(cfg *Config) { cfg.Endpoint = "" }},
		{name: "missing bucket", mutate: func(cfg *Config) { cfg.Bucket = "" }},
		{name: "invalid bucket", mutate: func(cfg *Config) { cfg.Bucket = "UPPERCASE" }},
		{name: "missing access key", mutate: func(cfg *Config) { cfg.AccessKeyID = "" }},
		{name: "missing secret key", mutate: func(cfg *Config) { cfg.SecretAccessKey = "" }},
		{name: "endpoint includes scheme", mutate: func(cfg *Config) { cfg.Endpoint = "http://127.0.0.1:9000" }},
		{name: "canceled context", ctx: canceled},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := validS3Config()
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			if _, err := New(ctx, cfg); err == nil {
				t.Fatal("New() error = nil, want configuration error")
			}
		})
	}
}

func TestMapS3Error(t *testing.T) {
	t.Parallel()

	notFound := minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404}
	if err := mapS3Error(notFound); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mapS3Error(not found) = %v, want ErrNotFound", err)
	}
	denied := minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403}
	if err := mapS3Error(denied); errors.Is(err, ErrNotFound) {
		t.Fatalf("mapS3Error(access denied) = %v, do not want ErrNotFound", err)
	}
}

func TestS3StoreValidatesOperationsBeforeNetworkAccess(t *testing.T) {
	t.Parallel()

	store, err := New(context.Background(), validS3Config())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := store.Put(context.Background(), "../escape", bytes.NewReader(nil), 0, "text/plain"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Put() unsafe key error = %v, want ErrInvalidKey", err)
	}
	if _, err := store.Put(context.Background(), "session.cast", nil, 0, "text/plain"); err == nil {
		t.Fatal("Put() nil source error = nil")
	}
	if _, err := store.Put(context.Background(), "session.cast", bytes.NewReader(nil), -2, "text/plain"); err == nil {
		t.Fatal("Put() invalid size error = nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, "session.cast", bytes.NewReader(nil), 0, "text/plain"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put() canceled context error = %v", err)
	}
	if _, err := store.Open(ctx, "session.cast"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() canceled context error = %v", err)
	}
	if _, err := store.Stat(ctx, "session.cast"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stat() canceled context error = %v", err)
	}
	if err := store.Delete(ctx, "session.cast"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete() canceled context error = %v", err)
	}
}

func validS3Config() Config {
	return Config{
		Provider:        ProviderS3,
		Endpoint:        "127.0.0.1:9000",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Bucket:          "rdp-recordings",
	}
}
