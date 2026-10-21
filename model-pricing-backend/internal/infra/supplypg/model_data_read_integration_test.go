package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestModelDataPostgres34RoundTrip(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("dedicated PostgreSQL data test not requested")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("explicit dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong test database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.standard_model(company, model_type, name, official_id) VALUES ('A-data-read-test', 'TEXT', '资料读取测试', 'roundtrip') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Run("migration defaults remain incomplete", func(t *testing.T) {
		data, err := ReadModelData(ctx, tx, id)
		if err != nil {
			t.Fatal(err)
		}
		if data.CheckedAt != "" || data.InputModalities == nil || len(data.InputModalities) != 0 || data.OutputModalities == nil || len(data.OutputModalities) != 0 || len(data.PriceItems) != 0 || len(data.Capacity) != 0 {
			t.Fatalf("invented/default facts: %+v", data)
		}
	})
	t.Run("array escaping JSONB exact prices and date", func(t *testing.T) {
		data := supply.ModelData{Company: "A-data-read-test", ModelType: "TEXT", Name: "资料读取测试", OfficialID: "roundtrip", InputModalities: []string{"TEXT", `quote"comma,slash\中`}, OutputModalities: []string{"TEXT"}, Capacity: []supply.OfficialFact{{Key: "context", Disclosure: "DISCLOSED", Value: "9007199254740993"}}, Abilities: []supply.Ability{{Key: "cache", State: "UNKNOWN", Description: "未披露"}}, PriceItems: []supply.PriceItem{{ItemKey: "input", Amount: "0.00000000", UnitQuantity: "1000000", Dimensions: map[string]string{"tier": "9007199254740993"}}, {ItemKey: "output", Amount: "123456789.12345678", UnitQuantity: "1000000"}}, APIAccess: []supply.OfficialFact{}, Lifecycle: []supply.OfficialFact{}, Sources: []supply.Source{{Label: "官方", URL: "https://example.com/pricing", Kind: "OFFICIAL_PRICING"}}, CheckedAt: "2026-10-20"}
		row, err := EncodeModelData(data)
		if err != nil {
			t.Fatal(err)
		}
		// The fixture uses parameterized SQL only; future writes must also supply
		// maintainer/CAS/version/log atomically, not expose this as a write API.
		_, err = tx.ExecContext(ctx, `UPDATE public.standard_model SET input_modalities=$2::text[], output_modalities=$3::text[], capacity=$4::jsonb, abilities=$5::jsonb, price_items=$6::jsonb, api_access=$7::jsonb, lifecycle=$8::jsonb, sources=$9::jsonb, checked_at=$10 WHERE id=$1`, id, row.InputModalities, row.OutputModalities, string(row.Capacity), string(row.Abilities), string(row.PriceItems), string(row.APIAccess), string(row.Lifecycle), string(row.Sources), row.CheckedAt)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ReadModelData(ctx, tx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, data) {
			t.Fatalf("round trip mismatch\ngot: %#v\nwant: %#v", got, data)
		}
	})
	t.Run("legacy facts and null array elements rejected", func(t *testing.T) {
		if _, err := tx.ExecContext(ctx, `UPDATE public.standard_model SET capacity='{"legacy":123}'::jsonb WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadModelData(ctx, tx, id); err == nil || !strings.Contains(err.Error(), "incompatible capacity") {
			t.Fatalf("legacy object must fail at capacity mapping: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE public.standard_model SET capacity='[]'::jsonb, input_modalities=ARRAY['TEXT',NULL]::text[] WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadModelData(ctx, tx, id); err == nil || !strings.Contains(err.Error(), "incompatible input_modalities") {
			t.Fatalf("NULL modality must fail at modality mapping: %v", err)
		}
	})
	t.Run("not found and canceled context propagate", func(t *testing.T) {
		if _, err := ReadModelData(ctx, tx, "9223372036854775807"); !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("missing row: %v", err)
		}
		canceled, stop := context.WithCancel(ctx)
		stop()
		if _, err := ReadModelData(canceled, tx, id); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled context: %v", err)
		}
	})
	// Roll back explicitly and verify the fixture never persists.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.standard_model WHERE company='A-data-read-test' AND official_id='roundtrip'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("fixture remained: count=%d err=%v", count, err)
	}
}
