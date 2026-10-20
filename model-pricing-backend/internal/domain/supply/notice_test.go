package supply

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func noticeFixture() *Notice {
	return &Notice{ID: "51", ResourceID: "31", Region: "CN", Kind: "RESOURCE_OFFLINED", Title: "示例资源已下架", Content: "请核对客户影响", Reason: "供应来源调整", AffectedStatus: "PENDING", PublishedAt: modelNow}
}

type noticeMemory struct {
	mu      *sync.Mutex
	notice  *Notice
	reads   map[string]*NoticeRead
	ops     []NoticeReadOperation
	failLog bool
	query   NoticeQuery
}

func newNoticeMemory() *noticeMemory {
	return &noticeMemory{mu: &sync.Mutex{}, notice: noticeFixture(), reads: map[string]*NoticeRead{}}
}
func (db *noticeMemory) Within(ctx context.Context, run func(NoticeTransaction) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	work := *db
	work.reads = map[string]*NoticeRead{}
	for key, read := range db.reads {
		work.reads[key] = read.Clone()
	}
	work.ops = append([]NoticeReadOperation(nil), db.ops...)
	if err := run(&work); err != nil {
		return err
	}
	db.reads = work.reads
	db.ops = work.ops
	return nil
}
func (db *noticeMemory) NoticeForUpdate(_ context.Context, id string) (*Notice, error) {
	if id != db.notice.ID {
		return nil, ErrNotFound
	}
	return db.notice.Clone(), nil
}
func (db *noticeMemory) InternalStaffExists(_ context.Context, id string) (bool, error) {
	return id == "7" || id == "8", nil
}
func (db *noticeMemory) GetReadForUpdate(_ context.Context, notice, staff string) (*NoticeRead, error) {
	return db.reads[notice+":"+staff].Clone(), nil
}
func (db *noticeMemory) CreateRead(_ context.Context, read *NoticeRead) error {
	key := read.NoticeID + ":" + read.StaffID
	if db.reads[key] != nil {
		return ErrRevisionConflict
	}
	db.reads[key] = read.Clone()
	return nil
}
func (db *noticeMemory) AppendReadOperation(_ context.Context, op NoticeReadOperation) error {
	if db.failLog {
		return errOperationLog
	}
	db.ops = append(db.ops, op)
	return nil
}
func (db *noticeMemory) ListNoticeReadStates(_ context.Context, q NoticeQuery) ([]NoticeReadRecord, int64, error) {
	db.query = q
	ids := q.StaffIDs
	if q.AllStaff {
		ids = []string{"7", "8"}
	}
	rows := []NoticeReadRecord{}
	for _, id := range ids {
		read := db.reads[db.notice.ID+":"+id]
		if (q.ReadStatus == "READ" && read == nil) || (q.ReadStatus == "UNREAD" && read != nil) {
			continue
		}
		rows = append(rows, NoticeReadRecord{Notice: db.notice.Clone(), StaffID: id, StaffName: "人员" + id, Read: read.Clone()})
	}
	return rows, int64(len(rows)), nil
}
func (db *noticeMemory) GetNoticeReadState(_ context.Context, id, staff string) (NoticeReadRecord, error) {
	if id != db.notice.ID {
		return NoticeReadRecord{}, ErrNotFound
	}
	return NoticeReadRecord{Notice: db.notice.Clone(), StaffID: staff, StaffName: "人员" + staff, Read: db.reads[id+":"+staff].Clone()}, nil
}

func TestNoticeReadStatesSeparateViewerFromSelectedPerson(t *testing.T) {
	db := newNoticeMemory()
	db.reads["51:8"] = &NoticeRead{NoticeID: "51", StaffID: "8", ReadAt: modelNow}
	reader := NewNoticeReader(db)
	page, err := reader.List(context.Background(), "7", NoticeQuery{StaffIDs: []string{"7", "8"}})
	if err != nil || page.Total != 2 || len(page.List) != 2 || !page.List[0].Permissions.CanMarkRead || page.List[1].Permissions.CanMarkRead || page.List[1].ReadStatus != "READ" {
		t.Fatalf("selected person became actor: %+v %v", page, err)
	}
	if db.query.SortBy != "published_at" || db.query.NoticeIDOrder != "desc" || db.query.StaffIDOrder != "asc" {
		t.Fatal("unstable notice/person ordering")
	}
	if len(db.reads) != 1 || len(db.ops) != 0 {
		t.Fatal("read query changed read state")
	}
	page, err = reader.List(context.Background(), "7", NoticeQuery{})
	if err != nil || len(page.List) != 1 || page.List[0].StaffID != "7" {
		t.Fatalf("default identity not authenticated staff: %+v %v", page, err)
	}
	other, err := reader.Get(context.Background(), "7", "51", "8")
	if err != nil || other.Permissions.CanMarkRead || other.ReadAt == nil || other.Notice.AffectedCustomerCount != nil {
		t.Fatalf("other person's details or unknown impact changed: %+v %v", other, err)
	}
	delete(db.reads, "51:8")
	unreadOther, err := reader.Get(context.Background(), "7", "51", "8")
	if err != nil || unreadOther.ReadStatus != "UNREAD" || unreadOther.Permissions.CanMarkRead {
		t.Fatalf("unread other person granted write: %+v %v", unreadOther, err)
	}
	all, err := reader.List(context.Background(), "7", NoticeQuery{AllStaff: true, ReadStatus: "UNREAD"})
	if err != nil || all.Total != 2 || len(all.List) != 2 || !db.query.AllStaff || len(db.query.StaffIDs) != 0 {
		t.Fatalf("all staff collapsed to current person: %+v %v", all, err)
	}
}

func TestNoticeKnownZeroImpactAndUnknownImpactRemainDistinct(t *testing.T) {
	db := newNoticeMemory()
	reader := NewNoticeReader(db)
	unknown, err := reader.Get(context.Background(), "7", "51", "")
	if err != nil || unknown.Notice.AffectedStatus != "PENDING" || unknown.Notice.AffectedCustomerCount != nil {
		t.Fatalf("unknown impact became zero: %+v %v", unknown, err)
	}
	zero := uint64(0)
	db.notice.AffectedStatus = "READY"
	db.notice.AffectedCustomerCount = &zero
	known, err := reader.Get(context.Background(), "7", "51", "")
	if err != nil || known.Notice.AffectedCustomerCount == nil || *known.Notice.AffectedCustomerCount != 0 {
		t.Fatalf("actual zero impact disappeared: %+v %v", known, err)
	}
	*known.Notice.AffectedCustomerCount = 5
	if *db.notice.AffectedCustomerCount != 0 {
		t.Fatal("view changed persisted impact")
	}
}

func TestNoticeReadIsPersonalAndKeepsFirstTimestamp(t *testing.T) {
	db := newNoticeMemory()
	now := modelNow.Add(time.Hour)
	svc := NewNoticeService(db, func() time.Time { return now })
	ctx := context.Background()
	first, err := svc.MarkRead(ctx, writeMeta("7"), "51")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	same, err := svc.MarkRead(ctx, writeMeta("7"), "51")
	if err != nil || !same.ReadAt.Equal(first.ReadAt) || same.Created || len(db.reads) != 1 || len(db.ops) != 2 || db.ops[1].Action != "READ_NOOP" || !db.ops[1].OccurredAt.Equal(now) {
		t.Fatalf("repeat changed first read or lost audit: %+v %v", same, err)
	}
	other, err := svc.MarkRead(ctx, writeMeta("8"), "51")
	if err != nil || other.StaffID != "8" || len(db.reads) != 2 || !other.Created || db.ops[2].ActorID != "8" {
		t.Fatalf("staff read rows collapsed: %+v %v", other, err)
	}
}

func TestNoticeReadLogFailureRollsBackAndConcurrentReadsDoNotDuplicate(t *testing.T) {
	db := newNoticeMemory()
	svc := NewNoticeService(db, func() time.Time { return modelNow })
	ctx := context.Background()
	db.failLog = true
	if result, err := svc.MarkRead(ctx, writeMeta("7"), "51"); result != nil || !errors.Is(err, errOperationLog) {
		t.Fatalf("failed log returned success: %+v %v", result, err)
	}
	if len(db.reads) != 0 || len(db.ops) != 0 {
		t.Fatal("read partially committed")
	}
	db.failLog = false
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := svc.MarkRead(ctx, writeMeta("7"), "51"); results <- err }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if len(db.reads) != 1 || len(db.ops) != 2 {
		t.Fatal("concurrent reads duplicated rows or lost new intent")
	}
	db.failLog = true
	if result, err := svc.MarkRead(ctx, writeMeta("7"), "51"); result != nil || !errors.Is(err, errOperationLog) {
		t.Fatalf("noop log failure returned success: %+v %v", result, err)
	}
}

func TestNoticeQueryRejectsAmbiguousOrMalformedPeople(t *testing.T) {
	for _, q := range []NoticeQuery{{StaffIDs: []string{"7", "7"}}, {StaffIDs: []string{"01"}}, {StaffIDs: []string{"8"}, AllStaff: true}, {ReadStatus: "DONE"}, {Region: "OTHER"}, {Size: 101}, {ResourceID: "0"}} {
		if _, err := NewNoticeReader(newNoticeMemory()).List(context.Background(), "7", q); err == nil {
			t.Fatalf("bad notice query accepted: %+v", q)
		}
	}
}
