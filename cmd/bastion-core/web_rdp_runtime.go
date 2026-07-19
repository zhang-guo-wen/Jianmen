package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/handler/accessrequest"
	"jianmen/internal/handler/webrdp"
	"jianmen/internal/objectstore"
	"jianmen/internal/online"
	"jianmen/internal/proxy/rdpproxy"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

type webRDPRuntime struct {
	webRDP         *webrdp.Handler
	accessRequests *accessrequest.Handler
}

func newWebRDPRuntime(
	ctx context.Context,
	cfg *config.Config,
	objects objectstore.Store,
	appStore *store.DBStore,
	identity *service.IdentityService,
	browserSessions *service.BrowserSessionService,
	authorization *service.AuthorizationService,
	onlineSessions *online.Registry,
	logger *slog.Logger,
) (webRDPRuntime, error) {
	if objects == nil {
		return webRDPRuntime{}, errors.New("RDP recording object store is required")
	}
	approvals, err := service.NewAccessRequestService(appStore)
	if err != nil {
		return webRDPRuntime{}, err
	}
	control, err := service.NewWebRDPService(appStore, authorization, approvals)
	if err != nil {
		return webRDPRuntime{}, err
	}
	recording, err := service.NewRDPRecordingService(service.RDPRecordingConfig{
		SpoolRoot: cfg.WebRDP.SpoolDir, GuacdRecordingRoot: cfg.WebRDP.GuacdRecordingRoot,
		LocalDriveRoot: cfg.WebRDP.LocalDriveRoot, GuacdDriveRoot: cfg.WebRDP.GuacdDriveRoot,
		AllowUnrecorded: cfg.WebRDP.AllowUnrecorded,
	}, appStore, objects)
	if err != nil {
		return webRDPRuntime{}, err
	}
	if err := recording.Recover(ctx, true); err != nil {
		logger.Warn("recover interrupted RDP recordings", "error", err)
	}
	startRDPRecordingRecovery(ctx, recording, logger)
	connector := rdpproxy.NewConnector(cfg.WebRDP.GuacdAddress)
	connector.Timeout = time.Duration(cfg.WebRDP.ConnectTimeoutSecs) * time.Second
	webRDPHandler, err := webrdp.New(
		webrdp.Config{
			Enabled:        cfg.WebRDP.Enabled,
			ConnectTimeout: connector.Timeout,
		},
		browserSessions, identity, control, recording, connector, appStore,
		objects, authorization, onlineSessions, logger,
	)
	if err != nil {
		return webRDPRuntime{}, err
	}
	accessHandler, err := accessrequest.New(approvals, control, authorization)
	if err != nil {
		return webRDPRuntime{}, err
	}
	return webRDPRuntime{webRDP: webRDPHandler, accessRequests: accessHandler}, nil
}

func newRDPObjectStore(
	ctx context.Context,
	cfg *config.Config,
) (objectstore.Store, error) {
	return objectstore.New(ctx, objectstore.Config{
		Provider: cfg.ObjectStorage.Provider, LocalDir: cfg.ObjectStorage.LocalDir,
		Endpoint: cfg.ObjectStorage.Endpoint, AccessKeyID: cfg.ObjectStorage.AccessKeyID,
		SecretAccessKey: cfg.ObjectStorage.SecretAccessKey,
		SessionToken:    cfg.ObjectStorage.SessionToken, Bucket: cfg.ObjectStorage.Bucket,
		Region: cfg.ObjectStorage.Region, Prefix: cfg.ObjectStorage.Prefix,
		Secure: cfg.ObjectStorage.Secure, PathStyle: cfg.ObjectStorage.PathStyle,
		AutoCreateBucket: cfg.ObjectStorage.AutoCreateBucket,
	})
}

func startRDPRecordingRecovery(
	ctx context.Context,
	recording *service.RDPRecordingService,
	logger *slog.Logger,
) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := recording.Recover(ctx, false); err != nil {
					logger.Warn("retry failed RDP recordings", "error", err)
				}
			}
		}
	}()
}
