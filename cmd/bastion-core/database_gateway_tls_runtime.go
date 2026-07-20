package main

import (
	"fmt"
	"log/slog"

	"jianmen/internal/config"
	"jianmen/internal/dbtls"
)

func prepareDatabaseGatewayTLS(
	cfg *config.Config,
	dataDir string,
	logger *slog.Logger,
) error {
	generated, err := dbtls.EnsureLocalUnifiedGatewayIdentity(
		&cfg.DatabaseGateway,
		dataDir,
	)
	if err != nil {
		return fmt.Errorf("prepare local unified gateway identity: %w", err)
	}
	if generated {
		logger.Info(
			"generated local unified database gateway TLS identity",
			"certificate", cfg.DatabaseGateway.Unified.CertFile,
		)
	}
	return nil
}
