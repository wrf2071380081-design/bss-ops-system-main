package customer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 客户「接入模型」绑定的领域测试：状态机逐行覆盖、乐观锁、资源准入校验、
// 下架联动（不自动停用）、重新配置来源、投影口径。
//
// 这些测试不连库：仓储用 fakeLinkStore 打桩，真实 SQL 与唯一索引由
// internal/infra/supplypg/customer_resource_link_store_integration_test.go 覆盖。

// fakeLinkStore 是 ResourceLinkStore 的内存实现，记录写入了什么，便于断言。
type fakeLinkStore struct {
	link      *ResourceLink
	facts     *ResourceFacts
	getErr    error
	createErr error
	rows      []CustomerModelServiceRow

	created      []CreateLinkRecord
	applied      []TransitionRecord
	rebound      []RebindRecord
	rebindResult *ResourceLink
}

func (f *fakeLinkStore) CreateLink(_ context.Context, in CreateLinkRecord) (*ResourceLink, error) {
	f.created = append(f.created, in)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &ResourceLink{
		ID: 1, CustomerProfileID: in.CustomerProfileID, StandardModelID: in.StandardModelID,
		ResourceID: in.ResourceID, State: in.State, Revision: 1,
	}, nil
}

func (f *fakeLinkStore) GetLink(_ context.Context, _ int64) (*ResourceLink, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.link == nil {
		return nil, ErrLinkNotFound
	}
	return f.link, nil
}

func (f *fakeLinkStore) ApplyTransition(_ context.Context, in TransitionRecord) (*ResourceLink, error) {
	f.applied = append(f.applied, in)
	current := f.link
	if current == nil {
		return nil, ErrLinkNotFound
	}
	next := *current
	next.State = in.To
	next.Revision = current.Revision + 1
	next.Archived = in.Archived
	next.StateBeforeOffline = in.StateBeforeOffline
	// 与真实仓储一致：迁移成功后读模型也要跟着变，
	// 否则「同一请求重复提交」这类顺序场景在桩上无从复现。
	f.link = &next
	return &next, nil
}

func (f *fakeLinkStore) RebindLink(_ context.Context, in RebindRecord) (*ResourceLink, error) {
	f.rebound = append(f.rebound, in)
	if f.rebindResult != nil {
		return f.rebindResult, nil
	}
	return &ResourceLink{ID: 2, State: LinkStateActive, Revision: 1}, nil
}

func (f *fakeLinkStore) LoadResourceFacts(_ context.Context, _ int64) (*ResourceFacts, error) {
	return f.facts, nil
}

// ListLinksByResource 供供应域联动测试使用：fake 只持有一条 link，按 resource 命中才返回。
func (f *fakeLinkStore) ListLinksByResource(_ context.Context, resourceID int64) ([]ResourceLink, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.link == nil || f.link.ResourceID != resourceID || f.link.Archived {
		return []ResourceLink{}, nil
	}
	return []ResourceLink{*f.link}, nil
}

func (f *fakeLinkStore) ListCustomerModelServices(_ context.Context, _ int64) ([]CustomerModelServiceRow, error) {
	return f.rows, nil
}

// ---- 状态机 ----

// TestLinkStateMachineLegalTransitions 覆盖状态机表的每一行（合法迁移）。
func TestLinkStateMachineLegalTransitions(t *testing.T) {
	cases := []struct {
		name               string
		from               string
		action             string
		stateBeforeOffline string
		want               string
	}{
		{"服务中→请求禁用", LinkStateActive, LinkActionDisableRequest, "", LinkStateDisablePending},
		{"禁用待确认→确认禁用", LinkStateDisablePending, LinkActionDisableConfirm, "", LinkStateDisabled},
		{"已禁用→请求恢复", LinkStateDisabled, LinkActionRestoreRequest, "", LinkStateRestorePending},
		{"恢复待确认→确认恢复", LinkStateRestorePending, LinkActionRestoreConfirm, "", LinkStateActive},
		{"服务中→资源下架转待处理", LinkStateActive, LinkActionSupplyOffline, "", LinkStateSupplyOfflinePending},
		{"已禁用→资源下架转待处理", LinkStateDisabled, LinkActionSupplyOffline, "", LinkStateSupplyOfflinePending},
		{"待处理→人工确认停用", LinkStateSupplyOfflinePending, LinkActionOfflineConfirm, "", LinkStateSupplyOffline},
		{"待处理→资源上架回服务中", LinkStateSupplyOfflinePending, LinkActionSupplyRestored, LinkStateActive, LinkStateActive},
		{"待处理→资源上架回已禁用", LinkStateSupplyOfflinePending, LinkActionSupplyRestored, LinkStateDisabled, LinkStateDisabled},
	}
	for _, c := range cases {
		got, err := NextState(c.from, c.action, c.stateBeforeOffline)
		if err != nil {
			t.Fatalf("%s：期望成功，实际报错 %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s：got %s want %s", c.name, got, c.want)
		}
	}
}

// TestLinkStateMachineRejectsIllegalTransitions 非法迁移必须被拒（至少覆盖 4 条）。
func TestLinkStateMachineRejectsIllegalTransitions(t *testing.T) {
	cases := []struct {
		name               string
		from               string
		action             string
		stateBeforeOffline string
		want               error
	}{
		{"已禁用不能直接请求禁用", LinkStateDisabled, LinkActionDisableRequest, "", ErrLinkTransitionInvalid},
		{"已停用不能再确认停用", LinkStateSupplyOffline, LinkActionOfflineConfirm, "", ErrLinkTransitionInvalid},
		{"服务中不能直接确认恢复", LinkStateActive, LinkActionRestoreConfirm, "", ErrLinkTransitionInvalid},
		{"待处理不能直接请求禁用", LinkStateSupplyOfflinePending, LinkActionDisableRequest, "", ErrLinkTransitionInvalid},
		{"禁用待确认不能直接恢复", LinkStateDisablePending, LinkActionRestoreRequest, "", ErrLinkTransitionInvalid},
		{"未知动作被拒", LinkStateActive, "SOME_UNKNOWN_ACTION", "", ErrLinkTransitionInvalid},
		{"未知状态被拒", "WHATEVER", LinkActionDisableRequest, "", ErrLinkTransitionInvalid},
		{"缺下架前状态时不能自动恢复", LinkStateSupplyOfflinePending, LinkActionSupplyRestored, "", ErrLinkStateTraceMissing},
		{"下架前状态非法时不能恢复", LinkStateSupplyOfflinePending, LinkActionSupplyRestored, LinkStateSupplyOffline, ErrLinkStateTraceMissing},
		// 已人工确认停用（SUPPLY_OFFLINE）的绑定不能被「资源重新上架」事件悄悄复活，
		// 恢复必须走人工重新配置来源。
		{"已停用不能被上架事件复活", LinkStateSupplyOffline, LinkActionSupplyRestored, LinkStateActive, ErrLinkTransitionInvalid},
		{"已停用不能被上架事件复活（无留痕）", LinkStateSupplyOffline, LinkActionSupplyRestored, "", ErrLinkTransitionInvalid},
	}
	for _, c := range cases {
		if _, err := NextState(c.from, c.action, c.stateBeforeOffline); !errors.Is(err, c.want) {
			t.Fatalf("%s：期望 %v，实际 %v", c.name, c.want, err)
		}
	}
}

// TestLinkTransitionErrorIsReadable 错误文案要能直接给运营看：带状态与动作的中文名。
func TestLinkTransitionErrorIsReadable(t *testing.T) {
	_, err := NextState(LinkStateDisabled, LinkActionDisableRequest, "")
	if err == nil {
		t.Fatal("期望报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "已禁用") || !strings.Contains(msg, "请求禁用") {
		t.Fatalf("错误文案缺少中文状态/动作：%s", msg)
	}
}

// TestLinkSystemActionsAreNotClientCallable 系统动作不能从动作接口进来。
func TestLinkSystemActionsAreNotClientCallable(t *testing.T) {
	if IsLinkActionAllowed(LinkActionSupplyOffline) || IsLinkActionAllowed(LinkActionSupplyRestored) {
		t.Fatal("系统动作不应允许客户端提交")
	}
	for _, action := range []string{LinkActionDisableRequest, LinkActionDisableConfirm,
		LinkActionRestoreRequest, LinkActionRestoreConfirm, LinkActionOfflineConfirm} {
		if !IsLinkActionAllowed(action) {
			t.Fatalf("%s 应允许客户端提交", action)
		}
	}
}

// TestArchivedAfterOnlyOnOfflineConfirm 只有「人工确认停用」才归档。
func TestArchivedAfterOnlyOnOfflineConfirm(t *testing.T) {
	if !ArchivedAfter(LinkActionOfflineConfirm) {
		t.Fatal("OFFLINE_CONFIRM 必须归档")
	}
	for _, action := range []string{LinkActionDisableRequest, LinkActionDisableConfirm,
		LinkActionRestoreRequest, LinkActionRestoreConfirm} {
		if ArchivedAfter(action) {
			t.Fatalf("%s 不应归档", action)
		}
	}
}

// ---- 服务：建绑定 ----

func TestResourceLinkCreateValidatesResourceFacts(t *testing.T) {
	base := CreateLinkInput{CustomerProfileID: 7, StandardModelID: 11, ResourceID: 21}

	t.Run("资源不存在", func(t *testing.T) {
		svc := NewResourceLinkService(&fakeLinkStore{facts: nil})
		if _, err := svc.Create(context.Background(), base, 1, "req"); !errors.Is(err, ErrLinkResourceNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("模型不匹配", func(t *testing.T) {
		svc := NewResourceLinkService(&fakeLinkStore{facts: &ResourceFacts{ID: 21, StandardModelID: 99, State: "ADMITTED"}})
		if _, err := svc.Create(context.Background(), base, 1, "req"); !errors.Is(err, ErrLinkModelMismatch) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("资源未准入", func(t *testing.T) {
		svc := NewResourceLinkService(&fakeLinkStore{facts: &ResourceFacts{ID: 21, StandardModelID: 11, State: "OFFLINE"}})
		if _, err := svc.Create(context.Background(), base, 1, "req"); !errors.Is(err, ErrLinkResourceNotAdmitted) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("缺客户", func(t *testing.T) {
		svc := NewResourceLinkService(&fakeLinkStore{})
		if _, err := svc.Create(context.Background(), CreateLinkInput{StandardModelID: 11, ResourceID: 21}, 1, "req"); !errors.Is(err, ErrLinkCustomerRequired) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("正常建绑定从 ACTIVE 开始并写 activated_at", func(t *testing.T) {
		store := &fakeLinkStore{facts: &ResourceFacts{ID: 21, StandardModelID: 11, State: "ADMITTED"}}
		svc := NewResourceLinkService(store)
		link, err := svc.Create(context.Background(), base, 42, "req-1")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if link.State != LinkStateActive {
			t.Fatalf("新建绑定必须 ACTIVE，got %s", link.State)
		}
		if len(store.created) != 1 {
			t.Fatalf("期望写入 1 条，got %d", len(store.created))
		}
		record := store.created[0]
		if record.State != LinkStateActive || record.ActivatedAt == nil || record.CreatedBy != 42 || record.RequestID != "req-1" {
			t.Fatalf("写入记录不符：%+v", record)
		}
	})
}

// TestResourceLinkCreateTranslatesUniqueConflicts 唯一索引冲突要原样透出可读业务错误。
func TestResourceLinkCreateTranslatesUniqueConflicts(t *testing.T) {
	for _, want := range []error{ErrLinkDuplicateResource, ErrLinkModelAlreadyActive} {
		store := &fakeLinkStore{
			facts:     &ResourceFacts{ID: 21, StandardModelID: 11, State: "ADMITTED"},
			createErr: want,
		}
		svc := NewResourceLinkService(store)
		_, err := svc.Create(context.Background(), CreateLinkInput{CustomerProfileID: 7, StandardModelID: 11, ResourceID: 21}, 1, "req")
		if !errors.Is(err, want) {
			t.Fatalf("期望 %v，got %v", want, err)
		}
	}
	if !strings.Contains(ErrLinkModelAlreadyActive.Error(), "先归档旧绑定") {
		t.Fatalf("同模型冲突文案必须指导用户归档旧绑定：%s", ErrLinkModelAlreadyActive.Error())
	}
}

// ---- 服务：动作与乐观锁 ----

func activeLink() *ResourceLink {
	return &ResourceLink{ID: 5, CustomerProfileID: 7, StandardModelID: 11, ResourceID: 21, State: LinkStateActive, Revision: 3}
}

func TestResourceLinkActRequiresRevision(t *testing.T) {
	store := &fakeLinkStore{link: activeLink()}
	svc := NewResourceLinkService(store)
	_, err := svc.Act(context.Background(), LinkActionInput{ID: 5, Action: LinkActionDisableRequest}, 1, "req")
	if !errors.Is(err, ErrLinkRevisionRequired) {
		t.Fatalf("got %v", err)
	}
	if len(store.applied) != 0 {
		t.Fatal("缺 expected_revision 不应写库")
	}
}

func TestResourceLinkActRejectsRevisionMismatch(t *testing.T) {
	store := &fakeLinkStore{link: activeLink()} // revision = 3
	svc := NewResourceLinkService(store)
	_, err := svc.Act(context.Background(), LinkActionInput{ID: 5, Action: LinkActionDisableRequest, ExpectedRevision: 2}, 1, "req")
	if !errors.Is(err, ErrLinkRevisionConflict) {
		t.Fatalf("got %v", err)
	}
	if len(store.applied) != 0 {
		t.Fatal("版本冲突不应写库")
	}
}

// TestResourceLinkActSequentialReplayReturnsConflict 钉住判断顺序：revision 必须先于状态机。
//
// 确定性复现（不是并发）：第一次「请求禁用」成功后状态已变成 DISABLE_PENDING、revision+1；
// 客户端用同一个旧 revision 再提交一次。若先跑状态机，就会命中
// 「禁用待确认 不允许执行『请求禁用』」→ 400，让人以为按钮点错了；
// 正确结果必须是 409（他人已修改，请刷新后重试），且库里状态不能被二次改动。
func TestResourceLinkActSequentialReplayReturnsConflict(t *testing.T) {
	store := &fakeLinkStore{link: activeLink()} // ACTIVE, revision = 3
	svc := NewResourceLinkService(store)
	ctx := context.Background()

	first, err := svc.Act(ctx, LinkActionInput{ID: 5, Action: LinkActionDisableRequest, ExpectedRevision: 3}, 1, "req-1")
	if err != nil {
		t.Fatalf("首次提交应成功：%v", err)
	}
	if first.State != LinkStateDisablePending || first.Revision != 4 {
		t.Fatalf("首次迁移结果不符：state=%s revision=%d", first.State, first.Revision)
	}

	// 同一 revision 重放：必须 409 语义，而不是「非法迁移」。
	_, err = svc.Act(ctx, LinkActionInput{ID: 5, Action: LinkActionDisableRequest, ExpectedRevision: 3}, 1, "req-1")
	if errors.Is(err, ErrLinkTransitionInvalid) {
		t.Fatalf("重复提交被误判成非法迁移（会被映射成 400）：%v", err)
	}
	if !errors.Is(err, ErrLinkRevisionConflict) {
		t.Fatalf("重复提交必须返回版本冲突（409），got %v", err)
	}
	if len(store.applied) != 1 {
		t.Fatalf("冲突不得写库：期望 1 次迁移，got %d", len(store.applied))
	}
	reloaded, err := store.GetLink(ctx, 5)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.State != LinkStateDisablePending || reloaded.Revision != 4 {
		t.Fatalf("冲突后库里状态不应变化：state=%s revision=%d", reloaded.State, reloaded.Revision)
	}

	// 用最新 revision 继续操作必须成功（证明只是并发保护，不是把人锁死）。
	if _, err := svc.Act(ctx, LinkActionInput{ID: 5, Action: LinkActionDisableConfirm, ExpectedRevision: 4}, 1, "req-2"); err != nil {
		t.Fatalf("用最新 revision 继续操作应成功：%v", err)
	}
}

// TestSystemEventsPreferRevisionConflict 系统事件同样要先判 revision，避免并发事件被误报成非法迁移。
func TestSystemEventsPreferRevisionConflict(t *testing.T) {
	store := &fakeLinkStore{link: &ResourceLink{ID: 9, State: LinkStateActive, Revision: 5}}
	svc := NewResourceLinkService(store)
	if _, err := svc.MarkSupplyOffline(context.Background(), 9, 4, "", "req"); !errors.Is(err, ErrLinkRevisionConflict) {
		t.Fatalf("下架事件旧 revision 应返回冲突，got %v", err)
	}
	store.link.State = LinkStateSupplyOfflinePending
	store.link.StateBeforeOffline = LinkStateActive
	store.link.Revision = 7
	if _, err := svc.MarkSupplyRestored(context.Background(), 9, 6, "req"); !errors.Is(err, ErrLinkRevisionConflict) {
		t.Fatalf("上架事件旧 revision 应返回冲突，got %v", err)
	}
	if len(store.applied) != 0 {
		t.Fatal("冲突不得写库")
	}
}

func TestResourceLinkActRejectsSystemAndUnknownActions(t *testing.T) {
	store := &fakeLinkStore{link: activeLink()}
	svc := NewResourceLinkService(store)
	for _, action := range []string{LinkActionSupplyOffline, LinkActionSupplyRestored, "WHATEVER"} {
		_, err := svc.Act(context.Background(), LinkActionInput{ID: 5, Action: action, ExpectedRevision: 3}, 1, "req")
		if !errors.Is(err, ErrLinkActionUnsupported) {
			t.Fatalf("%s：got %v", action, err)
		}
	}
}

func TestResourceLinkActRejectsArchivedLink(t *testing.T) {
	archived := activeLink()
	archived.Archived = true
	svc := NewResourceLinkService(&fakeLinkStore{link: archived})
	_, err := svc.Act(context.Background(), LinkActionInput{ID: 5, Action: LinkActionDisableRequest, ExpectedRevision: 3}, 1, "req")
	if !errors.Is(err, ErrLinkArchived) {
		t.Fatalf("got %v", err)
	}
}

func TestResourceLinkActWritesTransitionAndNote(t *testing.T) {
	store := &fakeLinkStore{link: activeLink()}
	svc := NewResourceLinkService(store)
	if _, err := svc.Act(context.Background(), LinkActionInput{
		ID: 5, Action: LinkActionDisableRequest, Reason: "客户要求先停一下", ExpectedRevision: 3,
	}, 1, "req"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(store.applied) != 1 {
		t.Fatalf("期望 1 次迁移，got %d", len(store.applied))
	}
	record := store.applied[0]
	if record.To != LinkStateDisablePending || record.Archived {
		t.Fatalf("迁移记录不符：%+v", record)
	}
	if !strings.Contains(record.Note, "客户要求先停一下") {
		t.Fatalf("人工原因未留痕：%s", record.Note)
	}
}

// ---- 下架联动（用户口径：不自动停用客户） ----

func TestMarkSupplyOfflineKeepsCustomerUsable(t *testing.T) {
	for _, from := range []string{LinkStateActive, LinkStateDisabled} {
		store := &fakeLinkStore{link: &ResourceLink{ID: 9, State: from, Revision: 1}}
		svc := NewResourceLinkService(store)
		link, err := svc.MarkSupplyOffline(context.Background(), 9, 1, "供应商主动下架", "req")
		if err != nil {
			t.Fatalf("%s: unexpected: %v", from, err)
		}
		if link.State != LinkStateSupplyOfflinePending {
			t.Fatalf("%s: 应转「来源待处理」，got %s", from, link.State)
		}
		// 关键：不能自动归档 / 停用，客户仍可用。
		if link.Archived {
			t.Fatalf("%s: 下架不能自动停用客户（archived 必须为 false）", from)
		}
		record := store.applied[0]
		if record.StateBeforeOffline != from {
			t.Fatalf("%s: 必须记住下架前状态，got %q", from, record.StateBeforeOffline)
		}
		if record.Archived {
			t.Fatalf("%s: 迁移记录不应要求归档", from)
		}
		if !strings.Contains(record.Note, "供应商主动下架") || !strings.Contains(record.Note, "待人工确认停用") {
			t.Fatalf("%s: 留痕文案不符：%s", from, record.Note)
		}
	}
}

func TestMarkSupplyOfflineRejectsIllegalSourceState(t *testing.T) {
	svc := NewResourceLinkService(&fakeLinkStore{link: &ResourceLink{ID: 9, State: LinkStateSupplyOfflinePending, Revision: 1}})
	if _, err := svc.MarkSupplyOffline(context.Background(), 9, 1, "", "req"); !errors.Is(err, ErrLinkTransitionInvalid) {
		t.Fatalf("待处理态再次下架应被拒，got %v", err)
	}
}

func TestOfflineConfirmArchivesAndFreesSlot(t *testing.T) {
	store := &fakeLinkStore{link: &ResourceLink{
		ID: 9, State: LinkStateSupplyOfflinePending, StateBeforeOffline: LinkStateActive, Revision: 4,
	}}
	svc := NewResourceLinkService(store)
	link, err := svc.Act(context.Background(), LinkActionInput{
		ID: 9, Action: LinkActionOfflineConfirm, Reason: "客户已迁移", ExpectedRevision: 4,
	}, 1, "req")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if link.State != LinkStateSupplyOffline || !link.Archived {
		t.Fatalf("确认停用后必须 SUPPLY_OFFLINE + archived，got state=%s archived=%v", link.State, link.Archived)
	}
	if store.applied[0].StateBeforeOffline != "" {
		t.Fatal("归档后应清空 state_before_offline，避免误导后续读取")
	}
}

func TestMarkSupplyRestoredReturnsToPreviousState(t *testing.T) {
	cases := []struct {
		before string
		want   string
	}{
		{LinkStateActive, LinkStateActive},
		{LinkStateDisabled, LinkStateDisabled},
	}
	for _, c := range cases {
		store := &fakeLinkStore{link: &ResourceLink{
			ID: 9, State: LinkStateSupplyOfflinePending, StateBeforeOffline: c.before, Revision: 2,
		}}
		svc := NewResourceLinkService(store)
		link, err := svc.MarkSupplyRestored(context.Background(), 9, 2, "req")
		if err != nil {
			t.Fatalf("%s: unexpected: %v", c.before, err)
		}
		if link.State != c.want {
			t.Fatalf("%s: 应回到原状态 %s，got %s", c.before, c.want, link.State)
		}
		if link.Archived {
			t.Fatalf("%s: 回原状态不应归档", c.before)
		}
		if store.applied[0].StateBeforeOffline != "" {
			t.Fatalf("%s: 离开待处理态应清空 state_before_offline", c.before)
		}
	}
}

func TestMarkSupplyRestoredRequiresTrace(t *testing.T) {
	svc := NewResourceLinkService(&fakeLinkStore{link: &ResourceLink{
		ID: 9, State: LinkStateSupplyOfflinePending, Revision: 2,
	}})
	if _, err := svc.MarkSupplyRestored(context.Background(), 9, 2, "req"); !errors.Is(err, ErrLinkStateTraceMissing) {
		t.Fatalf("got %v", err)
	}
}

// TestMarkSupplyRestoredRejectsArchivedLink 钉住口径分叉：只有「未人工确认的待处理」才随上架自动回原状态。
// 已经人工确认停用并归档的绑定（SUPPLY_OFFLINE + archived=true）**不得**被上架事件复活，
// 恢复必须走人工重新配置来源——否则等于绕过人工确认，把刚归档的来源悄悄打开。
func TestMarkSupplyRestoredRejectsArchivedLink(t *testing.T) {
	cases := []struct {
		name string
		link *ResourceLink
	}{
		{"已确认停用的归档绑定", &ResourceLink{ID: 9, State: LinkStateSupplyOffline, Archived: true, Revision: 3}},
		{"归档后即便原状态是 ACTIVE 也不许", &ResourceLink{ID: 9, State: LinkStateActive, Archived: true, Revision: 3}},
		{"归档的待处理绑定", &ResourceLink{
			ID: 9, State: LinkStateSupplyOfflinePending, StateBeforeOffline: LinkStateActive, Archived: true, Revision: 3,
		}},
	}
	for _, c := range cases {
		store := &fakeLinkStore{link: c.link}
		svc := NewResourceLinkService(store)
		if _, err := svc.MarkSupplyRestored(context.Background(), 9, 3, "req"); !errors.Is(err, ErrLinkArchived) {
			t.Fatalf("%s：期望 ErrLinkArchived，got %v", c.name, err)
		}
		if len(store.applied) != 0 {
			t.Fatalf("%s：已归档绑定不得写库", c.name)
		}
	}
}

// ---- 重新配置来源 ----

func TestRebindArchivesOldAndCreatesActive(t *testing.T) {
	store := &fakeLinkStore{
		link:  &ResourceLink{ID: 5, CustomerProfileID: 7, StandardModelID: 11, ResourceID: 21, State: LinkStateActive, Revision: 6},
		facts: &ResourceFacts{ID: 22, StandardModelID: 11, State: "ADMITTED"},
	}
	svc := NewResourceLinkService(store)
	link, err := svc.Rebind(context.Background(), RebindInput{ID: 5, ExpectedRevision: 6, ResourceID: 22, Note: "换成本地更便宜的来源"}, 42, "req")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if link.State != LinkStateActive {
		t.Fatalf("新绑定必须 ACTIVE，got %s", link.State)
	}
	if len(store.rebound) != 1 {
		t.Fatalf("期望 1 次重绑，got %d", len(store.rebound))
	}
	record := store.rebound[0]
	if record.ArchiveID != 5 || record.ExpectedRevision != 6 {
		t.Fatalf("必须先归档旧绑定：%+v", record)
	}
	if record.Next.ResourceID != 22 || record.Next.StandardModelID != 11 || record.Next.State != LinkStateActive {
		t.Fatalf("新绑定参数不符：%+v", record.Next)
	}
	if !strings.Contains(record.Next.ActivationNote, "更便宜的来源") {
		t.Fatalf("人工说明未留痕：%s", record.Next.ActivationNote)
	}
}

func TestRebindGuards(t *testing.T) {
	disabled := &ResourceLink{ID: 5, StandardModelID: 11, ResourceID: 21, State: LinkStateDisabled, Revision: 2}
	svc := NewResourceLinkService(&fakeLinkStore{link: disabled, facts: &ResourceFacts{ID: 22, StandardModelID: 11, State: "ADMITTED"}})
	if _, err := svc.Rebind(context.Background(), RebindInput{ID: 5, ExpectedRevision: 2, ResourceID: 22}, 1, "req"); !errors.Is(err, ErrLinkRebindOnlyActive) {
		t.Fatalf("已禁用不应允许换来源，got %v", err)
	}

	same := &ResourceLink{ID: 5, StandardModelID: 11, ResourceID: 21, State: LinkStateActive, Revision: 2}
	svc = NewResourceLinkService(&fakeLinkStore{link: same, facts: &ResourceFacts{ID: 21, StandardModelID: 11, State: "ADMITTED"}})
	if _, err := svc.Rebind(context.Background(), RebindInput{ID: 5, ExpectedRevision: 2, ResourceID: 21}, 1, "req"); !errors.Is(err, ErrLinkDuplicateResource) {
		t.Fatalf("换成同一条资源应被拒，got %v", err)
	}

	svc = NewResourceLinkService(&fakeLinkStore{link: same, facts: &ResourceFacts{ID: 22, StandardModelID: 99, State: "ADMITTED"}})
	if _, err := svc.Rebind(context.Background(), RebindInput{ID: 5, ExpectedRevision: 2, ResourceID: 22}, 1, "req"); !errors.Is(err, ErrLinkModelMismatch) {
		t.Fatalf("新来源模型不一致应被拒，got %v", err)
	}
}

// ---- 投影口径 ----

// TestProjectMapsServiceStateAndRiskFlags 投影的服务状态与风险标记必须按优先级来。
func TestProjectMapsServiceStateAndRiskFlags(t *testing.T) {
	cases := []struct {
		name        string
		row         CustomerModelServiceRow
		wantState   string
		mustHave    []string
		mustNotHave []string
	}{
		{
			name:      "服务中且可用",
			row:       CustomerModelServiceRow{PrimaryState: LinkStateActive, BoundCount: 1, ActiveCount: 1, AvailableCount: 3},
			wantState: ServiceStateServing,
		},
		{
			name:      "来源待处理优先于一切（需人工介入）",
			row:       CustomerModelServiceRow{PrimaryState: LinkStateActive, BoundCount: 1, ActiveCount: 1, OfflinePendingCount: 1, AvailableCount: 3},
			wantState: ServiceStateOfflinePending,
			mustHave:  []string{"来源已下架待处理"},
		},
		{
			name:      "禁用待确认仍算服务中",
			row:       CustomerModelServiceRow{PrimaryState: LinkStateDisablePending, BoundCount: 1, ActiveCount: 1, AvailableCount: 2},
			wantState: ServiceStateServing,
			mustHave:  []string{"禁用待确认"},
		},
		{
			name:      "已禁用",
			row:       CustomerModelServiceRow{PrimaryState: LinkStateDisabled, BoundCount: 1, DisabledCount: 1, AvailableCount: 2},
			wantState: ServiceStateDisabled,
			mustHave:  []string{"绑定资源已禁用"},
		},
		{
			name:      "恢复待确认仍算已禁用（可用性尚未恢复）",
			row:       CustomerModelServiceRow{PrimaryState: LinkStateRestorePending, BoundCount: 1, AvailableCount: 2},
			wantState: ServiceStateDisabled,
		},
		{
			name:      "无可用来源",
			row:       CustomerModelServiceRow{PrimaryState: "", BoundCount: 0, AvailableCount: 0},
			wantState: ServiceStateNoSource,
			mustHave:  []string{"无可用来源"},
		},
	}
	for _, c := range cases {
		got := c.row.Project()
		if got.ServiceState != c.wantState {
			t.Fatalf("%s：state got %s want %s", c.name, got.ServiceState, c.wantState)
		}
		for _, flag := range c.mustHave {
			if !hasFlag(got.RiskFlags, flag) {
				t.Fatalf("%s：缺少风险标记 %s（got %v）", c.name, flag, got.RiskFlags)
			}
		}
		for _, flag := range c.mustNotHave {
			if hasFlag(got.RiskFlags, flag) {
				t.Fatalf("%s：不应出现风险标记 %s", c.name, flag)
			}
		}
	}
}

// TestProjectNeverReturnsEmptyServiceState service_state 必须始终是确定的枚举值。
// 前端按 service_state 分支渲染，空串会掉进「未知」分支且不报错，所以在领域层就兜住。
func TestProjectNeverReturnsEmptyServiceState(t *testing.T) {
	known := map[string]bool{
		ServiceStateServing:        true,
		ServiceStateAccessing:      true,
		ServiceStateDisabled:       true,
		ServiceStateOfflinePending: true,
		ServiceStateNoSource:       true,
	}
	rows := []CustomerModelServiceRow{
		{},                                     // 空行（无绑定）
		{PrimaryState: ""},                     // 显式空状态
		{PrimaryState: "WHATEVER"},             // 脏数据：未知状态
		{PrimaryState: LinkStateSupplyOffline}, // 正常流程不会出现（已归档不进投影）
		{PrimaryState: LinkStateSupplyOfflinePending},
		{PrimaryState: LinkStateActive},
		{PrimaryState: LinkStateDisablePending},
		{PrimaryState: LinkStateDisabled},
		{PrimaryState: LinkStateRestorePending},
		{OfflinePendingCount: 1}, // 只有计数、没有主状态
	}
	for i, row := range rows {
		got := row.Project()
		if got.ServiceState == "" {
			t.Fatalf("第 %d 行（%+v）service_state 为空串", i, row)
		}
		if !known[got.ServiceState] {
			t.Fatalf("第 %d 行产出了未登记的 service_state=%q", i, got.ServiceState)
		}
	}
	if (CustomerModelServiceRow{}).Project().ServiceState != ServiceStateNoSource {
		t.Fatal("没有任何绑定时必须落到「无可用来源」")
	}
	if (CustomerModelServiceRow{PrimaryState: "WHATEVER"}).Project().ServiceState != ServiceStateNoSource {
		t.Fatal("未知状态必须有确定兜底值")
	}
	if (CustomerModelServiceRow{PrimaryState: LinkStateSupplyOffline}).Project().ServiceState != ServiceStateDisabled {
		t.Fatal("未归档的 SUPPLY_OFFLINE 脏数据应读作「已禁用」而不是「无可用来源」")
	}
}

// TestProjectRegionConvention region 的空值约定：没有未归档绑定时是空串（不是 null）。
func TestProjectRegionConvention(t *testing.T) {
	if got := (CustomerModelServiceRow{}).Project(); got.Region != "" {
		t.Fatalf("无绑定时 region 约定为空串，got %q", got.Region)
	}
	row := CustomerModelServiceRow{PrimaryState: LinkStateActive, Region: "OVERSEAS", BoundCount: 1, AvailableCount: 2}
	if got := row.Project(); got.Region != "OVERSEAS" {
		t.Fatalf("有绑定时 region 必须透传仓库聚合结果，got %q", got.Region)
	}
}

func TestListCustomerModelsPassesThroughCounts(t *testing.T) {
	store := &fakeLinkStore{rows: []CustomerModelServiceRow{{
		StandardModelID: 11, ModelName: "DeepSeek-V3", Region: "CN",
		PrimaryState: LinkStateActive, BoundCount: 2, ActiveCount: 1, AvailableCount: 5,
	}}}
	svc := NewResourceLinkService(store)
	list, err := svc.ListCustomerModels(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 行，got %d", len(list))
	}
	if list[0].AvailableCount != 5 || list[0].Region != "CN" || list[0].BoundCount != 2 {
		t.Fatalf("投影数值不符：%+v", list[0])
	}
	if list[0].ServiceState != ServiceStateServing {
		t.Fatalf("state got %s", list[0].ServiceState)
	}

	if _, err := svc.ListCustomerModels(context.Background(), 0); !errors.Is(err, ErrLinkCustomerIDOutOfRange) {
		t.Fatalf("非法客户 ID 应被拒，got %v", err)
	}
}

// TestServiceWithoutStoreFailsLoudly 没接仓储时必须显式报错，不能静默成功。
func TestServiceWithoutStoreFailsLoudly(t *testing.T) {
	var svc *ResourceLinkService
	if _, err := svc.ListCustomerModels(context.Background(), 1); !errors.Is(err, ErrLinkStoreUnavailable) {
		t.Fatalf("got %v", err)
	}
	if _, err := NewResourceLinkService(nil).Create(context.Background(), CreateLinkInput{CustomerProfileID: 1, StandardModelID: 1, ResourceID: 1}, 1, ""); !errors.Is(err, ErrLinkStoreUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func hasFlag(flags []string, want string) bool {
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}
	return false
}
