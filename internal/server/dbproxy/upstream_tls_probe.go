package dbproxy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

// ProbeUpstreamTLS completes only the protocol negotiation and verified TLS
// handshake. It never sends database credentials or an authentication request.
func ProbeUpstreamTLS(ctx context.Context, instance model.DatabaseInstance) error {
	mode, err := dbtls.NormalizeMode(instance.TLSMode)
	if err != nil {
		return err
	}
	if mode == dbtls.ModeDisable {
		return errors.New("upstream TLS probe requires an enabled TLS mode")
	}

	var connection interface{ Close() error }
	switch strings.ToLower(strings.TrimSpace(instance.Protocol)) {
	case "mysql":
		secured, _, dialErr := dialMySQLUpstream(ctx, instance, "")
		if dialErr != nil {
			return dialErr
		}
		connection = secured
	case "postgres", "postgresql", "pg":
		secured, dialErr := dialPostgresUpstream(ctx, instance)
		if dialErr != nil {
			return dialErr
		}
		connection = secured
	case "redis":
		secured, dialErr := dialRedisUpstream(ctx, instance)
		if dialErr != nil {
			return dialErr
		}
		connection = secured
	default:
		return fmt.Errorf("unsupported database protocol %q", instance.Protocol)
	}
	_ = connection.Close()
	return nil
}
