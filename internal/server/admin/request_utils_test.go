package admin

import (
	"net/http/httptest"
	"testing"
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
