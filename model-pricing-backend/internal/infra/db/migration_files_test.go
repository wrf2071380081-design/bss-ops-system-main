package db

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Duplicate numbers are rejected by the production migrate file source even if names differ.
func TestMigrationVersionsAreUnique(t *testing.T) {
	entries, err := os.ReadDir("../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^(\d{6})_(.+)\.(up|down)\.sql$`)
	seen := map[string]string{}
	for _, entry := range entries {
		match := pattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		key := match[1] + "." + match[3]
		if previous, ok := seen[key]; ok {
			t.Errorf("duplicate migration %s: %s and %s", key, previous, entry.Name())
		}
		seen[key] = filepath.Base(entry.Name())
	}
}
