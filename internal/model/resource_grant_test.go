package model

import (
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestResourceGrantIndexesMapActiveMarkerOnce(t *testing.T) {
	parsed, err := schema.Parse(
		&ResourceGrant{},
		&sync.Map{},
		schema.NamingStrategy{},
	)
	if err != nil {
		t.Fatalf("parse resource grant schema: %v", err)
	}

	indexes := make(map[string][]string)
	for _, index := range parsed.ParseIndexes() {
		columns := make([]string, len(index.Fields))
		for position, option := range index.Fields {
			columns[position] = option.Field.DBName
		}
		indexes[index.Name] = columns
	}

	assertResourceGrantIndexColumns(
		t,
		indexes,
		"idx_resource_grants_active_marker",
		[]string{"active_marker"},
	)
	assertResourceGrantIndexColumns(
		t,
		indexes,
		"idx_rg_logic_active",
		[]string{
			"principal_type",
			"principal_id",
			"resource_type",
			"resource_id",
			"effect",
			"active_marker",
		},
	)
}

func assertResourceGrantIndexColumns(
	t *testing.T,
	indexes map[string][]string,
	name string,
	want []string,
) {
	t.Helper()
	got, exists := indexes[name]
	if !exists {
		t.Fatalf("resource grant index %s is missing", name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resource grant index %s columns = %v, want %v", name, got, want)
	}
}
