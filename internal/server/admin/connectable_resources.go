package admin

import (
	"net/http"
	"strings"
)

func connectableOnly(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("connectable")), "true")
}
