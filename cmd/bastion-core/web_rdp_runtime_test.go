package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func TestRDPObjectStoreRemainsAvailableWhenNewConnectionsAreDisabled(
	t *testing.T,
) {
	cfg := &config.Config{}
	cfg.WebRDP.Enabled = false
	cfg.ObjectStorage.Provider = "filesystem"
	cfg.ObjectStorage.LocalDir = t.TempDir()

	objects, err := newRDPObjectStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newRDPObjectStore() error = %v", err)
	}
	if objects == nil {
		t.Fatal("newRDPObjectStore() returned nil for disabled Web RDP")
	}
}

func TestDisabledWebRDPRuntimeKeepsAuditHandlersAvailable(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Enabled = true
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(root, "metadata.db")
	cfg.WebRDP.Enabled = false
	cfg.WebRDP.GuacdAddress = "127.0.0.1:4822"
	cfg.WebRDP.ConnectTimeoutSecs = 15
	cfg.WebRDP.SpoolDir = filepath.Join(root, "spool")
	cfg.WebRDP.GuacdRecordingRoot = cfg.WebRDP.SpoolDir
	cfg.WebRDP.LocalDriveRoot = filepath.Join(root, "drive")
	cfg.WebRDP.GuacdDriveRoot = cfg.WebRDP.LocalDriveRoot
	cfg.ObjectStorage.Provider = "filesystem"
	cfg.ObjectStorage.LocalDir = filepath.Join(root, "objects")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, _, closeDB, err := initializeMetadata(cfg, logger)
	if err != nil {
		t.Fatalf("initializeMetadata() error = %v", err)
	}
	t.Cleanup(closeDB)
	appStore := store.NewDBStore(db)
	identity, err := service.NewIdentityService(appStore)
	if err != nil {
		t.Fatal(err)
	}
	browserSessions, err := service.NewBrowserSessionService(appStore)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := service.NewAuthorizationService(
		identity,
		rbac.NewChecker(db),
		rbac.NewResourceGrantChecker(db),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	objects, err := newRDPObjectStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := newWebRDPRuntime(
		ctx,
		cfg,
		objects,
		appStore,
		identity,
		browserSessions,
		authorization,
		online.NewRegistry(),
		logger,
	)
	if err != nil {
		t.Fatalf("newWebRDPRuntime() error = %v", err)
	}
	if runtime.webRDP == nil || runtime.accessRequests == nil {
		t.Fatalf("disabled runtime handlers = %#v, want audit and approval handlers", runtime)
	}
}
