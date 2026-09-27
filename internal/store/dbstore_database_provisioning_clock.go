package store

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

type databaseProvisioningStatementClock struct {
	validLease        string
	expiredOrUnset    string
	currentTimestamp  string
	remainingDuration string
	dialect           string
}

func newDatabaseProvisioningStatementClock(db *gorm.DB) (databaseProvisioningStatementClock, error) {
	if db == nil || db.Dialector == nil {
		return databaseProvisioningStatementClock{},
			errors.New("database provisioning clock is unavailable")
	}
	switch db.Dialector.Name() {
	case "sqlite":
		return databaseProvisioningStatementClock{
			dialect:    "sqlite",
			validLease: "julianday(lease_expires_at) > julianday('now')",
			expiredOrUnset: "(lease_expires_at IS NULL OR " +
				"julianday(lease_expires_at) <= julianday('now'))",
			currentTimestamp:  "strftime('%Y-%m-%d %H:%M:%f', 'now')",
			remainingDuration: "(julianday(lease_expires_at) - julianday('now')) * 86400.0",
		}, nil
	case "mysql":
		return databaseProvisioningStatementClock{
			dialect:    "mysql",
			validLease: "lease_expires_at > CURRENT_TIMESTAMP(6)",
			expiredOrUnset: "(lease_expires_at IS NULL OR " +
				"lease_expires_at <= CURRENT_TIMESTAMP(6))",
			currentTimestamp:  "CURRENT_TIMESTAMP(6)",
			remainingDuration: "TIMESTAMPDIFF(MICROSECOND, CURRENT_TIMESTAMP(6), lease_expires_at) / 1000000.0",
		}, nil
	case "postgres":
		return databaseProvisioningStatementClock{
			dialect:    "postgres",
			validLease: "lease_expires_at > clock_timestamp()",
			expiredOrUnset: "(lease_expires_at IS NULL OR " +
				"lease_expires_at <= clock_timestamp())",
			currentTimestamp:  "clock_timestamp()",
			remainingDuration: "EXTRACT(EPOCH FROM (lease_expires_at - clock_timestamp()))",
		}, nil
	default:
		return databaseProvisioningStatementClock{},
			errors.New("database provisioning clock dialect is unsupported")
	}
}

func (c databaseProvisioningStatementClock) validLeaseCondition() string {
	return c.validLease
}

func (c databaseProvisioningStatementClock) expiredOrUnsetLeaseCondition() string {
	return c.expiredOrUnset
}

func (c databaseProvisioningStatementClock) currentTimestampExpression() any {
	return gorm.Expr(c.currentTimestamp)
}

func (c databaseProvisioningStatementClock) leaseExpiryExpression(duration time.Duration) (any, error) {
	if duration < time.Microsecond || duration%time.Microsecond != 0 {
		return nil, errors.New("database provisioning lease duration is unsupported")
	}
	microseconds := duration / time.Microsecond
	switch c.dialect {
	case "sqlite":
		seconds := float64(microseconds) / float64(time.Second/time.Microsecond)
		modifier := "+" + strconv.FormatFloat(seconds, 'f', 6, 64) + " seconds"
		return gorm.Expr("strftime('%Y-%m-%d %H:%M:%f', 'now', ?)", modifier), nil
	case "mysql":
		return gorm.Expr("TIMESTAMPADD(MICROSECOND, ?, CURRENT_TIMESTAMP(6))", microseconds), nil
	case "postgres":
		return gorm.Expr("clock_timestamp() + (? * interval '1 microsecond')", microseconds), nil
	default:
		return nil, errors.New("database provisioning clock dialect is unsupported")
	}
}

func (c databaseProvisioningStatementClock) leaseWindow(
	ctx context.Context,
	db *gorm.DB,
	id string,
	maxRemaining time.Duration,
) (service.DatabaseProvisioningLeaseWindow, error) {
	if ctx == nil || db == nil || maxRemaining <= 0 {
		return service.DatabaseProvisioningLeaseWindow{},
			errors.New("database provisioning lease window is unavailable")
	}
	var seconds float64
	result := db.WithContext(ctx).Raw(
		"SELECT "+c.remainingDuration+
			" FROM database_provisioning_operations WHERE id = ? AND active_marker = ?",
		id, model.ActiveMarkerValue,
	).Scan(&seconds)
	if result.Error != nil || result.RowsAffected != 1 {
		return service.DatabaseProvisioningLeaseWindow{},
			errors.New("read database provisioning lease window")
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 ||
		seconds > float64(math.MaxInt64)/float64(time.Second) {
		return service.DatabaseProvisioningLeaseWindow{},
			errors.New("database provisioning lease window is invalid")
	}
	nanoseconds := math.Floor(seconds * float64(time.Second))
	if nanoseconds <= 0 || nanoseconds > float64(math.MaxInt64) {
		return service.DatabaseProvisioningLeaseWindow{},
			errors.New("database provisioning lease window is invalid")
	}
	remaining := time.Duration(nanoseconds)
	if remaining > maxRemaining {
		remaining = maxRemaining
	}
	return service.DatabaseProvisioningLeaseWindow{Remaining: remaining}, nil
}

func databaseNow(ctx context.Context, db *gorm.DB) (time.Time, error) {
	if ctx == nil || db == nil || db.Dialector == nil {
		return time.Time{}, errors.New("database provisioning clock is unavailable")
	}
	var query string
	switch db.Dialector.Name() {
	case "sqlite":
		query = "SELECT (julianday('now') - 2440587.5) * 86400.0"
	case "mysql":
		query = "SELECT UNIX_TIMESTAMP(CURRENT_TIMESTAMP(6))"
	case "postgres":
		query = "SELECT EXTRACT(EPOCH FROM clock_timestamp())"
	default:
		return time.Time{}, errors.New("database provisioning clock dialect is unsupported")
	}
	var seconds float64
	if err := db.WithContext(ctx).Raw(query).Row().Scan(&seconds); err != nil {
		return time.Time{}, errors.New("read database provisioning clock")
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 ||
		seconds > float64(math.MaxInt64/time.Second) {
		return time.Time{}, errors.New("database provisioning clock is invalid")
	}
	whole := math.Floor(seconds)
	nanoseconds := math.Round((seconds - whole) * float64(time.Second))
	if nanoseconds >= float64(time.Second) {
		whole++
		nanoseconds = 0
	}
	return time.Unix(int64(whole), int64(nanoseconds)).UTC(), nil
}
