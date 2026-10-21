package supply

import (
	"context"
	"testing"
	"time"
)

func TestNoticeFirstReadReceiptUsesPersistableMicrosecondPrecision(t *testing.T) {
	db := newNoticeMemory()
	now := modelNow.Add(123456789 * time.Nanosecond)
	svc := NewNoticeService(db, func() time.Time { return now })
	r, err := svc.MarkRead(context.Background(), WriteMeta{ActorID: "7", Reason: "阅读公告", RequestID: "notice-precision"}, db.notice.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := now.UTC().Truncate(time.Microsecond)
	if !r.ReadAt.Equal(want) || !db.reads[db.notice.ID+":7"].ReadAt.Equal(want) || !db.ops[0].OccurredAt.Equal(want) {
		t.Fatalf("receipt/read/log cannot roundtrip PostgreSQL precision: receipt=%s want=%s", r.ReadAt, want)
	}
}
