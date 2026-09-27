package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"jianmen/internal/pkg/apiresp"
)

// requestIDMiddleware 为每个请求注入短 UUID 作为 request_id
func requestIDMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		reqID := hex.EncodeToString(b)
		ctx := context.WithValue(r.Context(), apiresp.CtxKeyRequestID, reqID)
		next(w, r.WithContext(ctx))
	}
}
