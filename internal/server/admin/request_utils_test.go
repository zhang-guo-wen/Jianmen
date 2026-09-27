package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/store"
)

func TestPaginateSliceDefaultsToFiftyItems(t *testing.T) {
	items := make([]int, 75)
	request := httptest.NewRequest("GET", "/api/test", nil)

	page := paginateSlice(items, request, func(int, string) bool { return true })
	pageItems, ok := page.Items.([]int)
	if !ok {
		t.Fatalf("items type = %T, want []int", page.Items)
	}
	if page.Page != 1 || page.PageSize != 50 || page.Total != 75 || len(pageItems) != 50 {
		t.Fatalf("page = %+v with %d items, want page=1 page_size=50 total=75 items=50", page, len(pageItems))
	}
}

func TestPaginateHostsDefaultsToTwentyItems(t *testing.T) {
	hosts := make([]store.HostView, 25)
	request := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)

	page := paginateHosts(hosts, request)
	pageItems, ok := page.Items.([]store.HostView)
	if !ok {
		t.Fatalf("items type = %T, want []store.HostView", page.Items)
	}
	if page.PageSize != 20 || len(pageItems) != 20 {
		t.Fatalf("page_size = %d with %d items, want 20 and 20", page.PageSize, len(pageItems))
	}
}

func TestFilterByGroupSupportsExactAndUngroupedQueries(t *testing.T) {
	type item struct {
		Group string
	}
	items := []item{{Group: "生产"}, {Group: "production"}, {Group: ""}, {Group: "  "}}

	exactRequest := httptest.NewRequest(http.MethodGet, "/?group=PRODUCTION", nil)
	exact := filterByGroup(items, exactRequest, func(value item) string { return value.Group })
	if len(exact) != 1 || exact[0].Group != "production" {
		t.Fatalf("exact group result = %#v, want production", exact)
	}

	ungroupedRequest := httptest.NewRequest(http.MethodGet, "/?ungrouped=true", nil)
	ungrouped := filterByGroup(items, ungroupedRequest, func(value item) string { return value.Group })
	if len(ungrouped) != 2 {
		t.Fatalf("ungrouped result count = %d, want 2", len(ungrouped))
	}
}
