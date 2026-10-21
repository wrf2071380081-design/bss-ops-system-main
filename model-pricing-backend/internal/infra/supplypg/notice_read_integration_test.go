package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNoticeReadPostgres34(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("dedicated notice PostgreSQL test not requested")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("explicit dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	var org string
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES ('A公告读取测试','/a-notice-test/') RETURNING id::text`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	staff := make([]string, 2)
	for i := range staff {
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES ($1,$2,$3) RETURNING id::text`, org, fmt.Sprintf("A公告人员%d", i), fmt.Sprintf("ant-%s-%d", org, i)).Scan(&staff[i]); err != nil {
			t.Fatal(err)
		}
	}
	ids := make([]string, 4)
	for i := range ids {
		n := noticeDataFixture()
		n.Title = fmt.Sprintf("公告%d_100%%", i)
		n.SourceEventID = ""
		if i == 2 {
			n.Region = "OVERSEAS"
			n.ResourceID = "33"
		}
		raw, err := EncodeNoticePayload(n)
		if err != nil {
			t.Fatal(err)
		}
		if i == 3 {
			raw = []byte(`{"contract":"trade.notice","version":1}`)
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.notice(type,target_type,target_id,payload,created_at) VALUES ($1,'RESOURCE',$2,$3::jsonb,$4) RETURNING id::text`, n.Kind, n.ResourceID, string(raw), n.PublishedAt).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	readAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.notice_read(notice_id,staff_id,read_at) VALUES ($1,$2,$3)`, ids[1], staff[1], readAt); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Fixtures must be committed so the adapter's independent read snapshots see
	// them; cleanup removes only these exact IDs in the dedicated test DB.
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, id := range ids {
			if _, err := db.ExecContext(cleanup, `DELETE FROM public.notice WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		}
		for _, id := range staff {
			if _, err := db.ExecContext(cleanup, `DELETE FROM public.internal_staff WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.org_unit WHERE id=$1`, org); err != nil {
			t.Error(err)
		}
	}()
	store := NewNoticeReadStore(db)
	reader := supply.NewNoticeReader(store)
	t.Run("person combinations sorting and pagination", func(t *testing.T) {
		page, err := reader.List(ctx, staff[0], supply.NoticeQuery{StaffIDs: staff, Region: "CN", ResourceID: "9007199254740993", Page: 1, Size: 3})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 || len(page.List) != 3 || page.List[0].Notice.ID != ids[1] || page.List[0].StaffID != staff[0] || page.List[1].StaffID != staff[1] || page.List[2].Notice.ID != ids[0] {
			t.Fatalf("incorrect pairs/order %+v", page)
		}
		if !page.List[0].Permissions.CanMarkRead || page.List[1].Permissions.CanMarkRead || page.List[1].ReadStatus != "READ" {
			t.Fatal("selected person became actor")
		}
		last, err := reader.List(ctx, staff[0], supply.NoticeQuery{StaffIDs: staff, Region: "CN", Page: 2, Size: 3})
		if err != nil || last.Total != 4 || len(last.List) != 1 {
			t.Fatalf("last page %+v %v", last, err)
		}
		beyond, err := reader.List(ctx, staff[0], supply.NoticeQuery{StaffIDs: staff, Region: "CN", Page: 9, Size: 3})
		if err != nil || beyond.Total != 4 || len(beyond.List) != 0 {
			t.Fatalf("out of range total %+v %v", beyond, err)
		}
	})
	t.Run("read status filtered before count and pagination", func(t *testing.T) {
		for _, tc := range []struct {
			status string
			total  int64
		}{{"READ", 1}, {"UNREAD", 3}} {
			p, err := reader.List(ctx, staff[0], supply.NoticeQuery{StaffIDs: staff, Region: "CN", ReadStatus: tc.status, Size: 1})
			if err != nil || p.Total != tc.total || len(p.List) != 1 || p.List[0].ReadStatus != tc.status {
				t.Fatalf("filter %s: %+v %v", tc.status, p, err)
			}
		}
	})
	t.Run("all staff uses full directory and literal keyword", func(t *testing.T) {
		var staffCount int64
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM public.internal_staff").Scan(&staffCount); err != nil {
			t.Fatal(err)
		}
		p, err := reader.List(ctx, staff[0], supply.NoticeQuery{AllStaff: true, Keyword: "_100%", Size: 100})
		if err != nil || p.Total != staffCount*3 {
			t.Fatalf("directory cross product %+v %v", p, err)
		}
		p, err = reader.List(ctx, staff[0], supply.NoticeQuery{StaffIDs: staff, Keyword: "_200%", Size: 100})
		if err != nil || p.Total != 0 {
			t.Fatalf("wildcard matched %+v %v", p, err)
		}
	})
	t.Run("detail is personal and GET leaves unread", func(t *testing.T) {
		own, err := reader.Get(ctx, staff[0], ids[1], "")
		if err != nil || own.ReadStatus != "UNREAD" || !own.Permissions.CanMarkRead {
			t.Fatalf("own %+v %v", own, err)
		}
		other, err := reader.Get(ctx, staff[0], ids[1], staff[1])
		if err != nil || other.ReadStatus != "READ" || other.Permissions.CanMarkRead || !other.ReadAt.Equal(readAt) {
			t.Fatalf("other %+v %v", other, err)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.notice_read WHERE notice_id IN ($1,$2,$3,$4)`, ids[0], ids[1], ids[2], ids[3]).Scan(&count); err != nil || count != 1 {
			t.Fatalf("GET changed read state count=%d %v", count, err)
		}
	})
	t.Run("missing persons and incompatible A payload fail", func(t *testing.T) {
		if _, err := reader.Get(ctx, staff[0], ids[0], "9223372036854775807"); !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("missing person %v", err)
		}
		if _, err := reader.List(ctx, staff[0], supply.NoticeQuery{StaffIDs: []string{staff[0], "9223372036854775807"}}); !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("partial staff selection %v", err)
		}
		if _, err := reader.Get(ctx, staff[0], ids[3], ""); !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("foreign contract %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE public.notice SET payload=jsonb_set(payload,'{version}','2'::jsonb) WHERE id=$1`, ids[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.Get(ctx, staff[0], ids[0], ""); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("unknown version %v", err)
		}
	})
}
