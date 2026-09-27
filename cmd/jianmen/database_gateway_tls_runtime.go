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
	generated, err := dbtls.EnsureLocalGatewayIdentities(
		&cfg.DatabaseGateway,
		dataDir,
	)
	if err != nil {
		return fmt.Errorf("prepare managed database gateway identity: %w", err)
	}
	if generated {
		logger.Info("generated managed database gateway TLS identity")
	}
	return nil
}
