package admin

import (
	"context"
	"time"
)

const auditWriteTimeout = 5 * time.Second

func detachedAuditWriteContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), auditWriteTimeout)
}
