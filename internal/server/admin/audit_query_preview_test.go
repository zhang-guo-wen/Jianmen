package admin

import (
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func auditDBQuerySQLPreview(query store.AuditDBQueryPreview) (string, map[string]any) {
	return service.AuditDBQuerySQLPreview(service.AuditDBQueryPreview{
		SQLText: query.SQLText, SQLStoredBytes: query.SQLStoredBytes,
		OriginalSQLBytes: query.OriginalSQLBytes, SQLTruncated: query.SQLTruncated,
	})
}

func auditDBQueryUTF8Prefix(value string, byteLimit int) (string, bool) {
	return service.AuditDBQueryUTF8Prefix(value, byteLimit)
}
