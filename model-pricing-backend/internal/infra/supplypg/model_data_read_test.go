package supplypg

import (
	"context"
	"errors"
	"model_bss/internal/domain/supply"
	"reflect"
	"testing"
)

func TestModelDataReadRequiresTransactionAndCanonicalIdentity(t *testing.T) {
	if _, err := ReadModelData(context.Background(), nil, "1"); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing transaction: %v", err)
	}
	for _, id := range []string{"0", "01", "-1", "9223372036854775808", "1 OR true"} {
		if _, err := ReadModelData(context.Background(), nil, id); err == nil || errors.Is(err, supply.ErrDependencyUnavailable) {
			t.Fatalf("invalid identity must fail before query: %q %v", id, err)
		}
	}
}

func TestModelModalitiesKeepStringsAndRejectNonStringArrays(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{`[]`, []string{}},
		{`["TEXT","quote\"comma,slash\\中"]`, []string{"TEXT", "quote\"comma,slash\\中"}},
	} {
		got, err := modelModalities([]byte(tc.raw), "input_modalities")
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got=%#v err=%v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{`null`, `[null]`, `[["TEXT"]]`, `[1]`, `{}`, `[] {}`, ``} {
		if _, err := modelModalities([]byte(raw), "input_modalities"); err == nil {
			t.Fatalf("accepted invalid array %s", raw)
		}
	}
}
