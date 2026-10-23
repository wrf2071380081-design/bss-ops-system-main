// Package worker 的 jobs.go：把 5b/5d/6b 已有的领域方法包成 JobFunc。
// 6b-3 起注册 cost-recalc（COST_RECALC 消费者）：认领靠条件更新 PENDING→RUNNING，
// 业务事务与任务状态推进分离，退避 1min×5^(n-1)，第 3 次置 DEAD+CRITICAL 告警。
package worker

import (
	"context"
	"time"

	"go.uber.org/zap"

	"model_bss/internal/domain/cost"
	"model_bss/internal/domain/supplier"
	tpdomain "model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/config"
	"model_bss/internal/repo"
)

// RegisterJobs 按配置注册任务。返回注册的任务数（供启动日志）。
// costSvc / costJobs 为 6b-3 新增：COST_RECALC 消费者；传 nil 时跳过注册（老调用点兼容）。
// eventJobs 为 11b 新增：event_outbox 推送消费者；传 nil 时跳过注册。
func RegisterJobs(s *Scheduler, cfg config.Worker, approveSvc *supplier.ApproveService, lifecycleSvc *supplier.LifecycleService, costSvc *cost.Service, costJobs *CostJobRepo, eventJobs *repo.EventJobRepo) int {
	if !cfg.Enable {
		return 0
	}
	n := 0
	if cfg.Jobs.ActivateQuote.Enable {
		interval := cfg.Jobs.ActivateQuote.Interval
		if interval <= 0 {
			interval = time.Minute // 默认 1 分钟
		}
		s.Register("activate-quote", func(ctx context.Context, ident supplier.JobIdentity) error {
			_, err := approveSvc.ActivateDueQuotes(ctx, supplier.ActivateParams{
				OperatorID:   ident.OperatorID,
				OperatorRole: ident.OperatorRole,
				SourceType:   ident.SourceType,
				RequestID:    ident.RequestID,
			})
			return err
		}, interval, "")
		n++
	}
	if cfg.Jobs.CostRecalc.Enable && costSvc != nil && costJobs != nil {
		interval := cfg.Jobs.CostRecalc.Interval
		if interval <= 0 {
			interval = time.Minute // 默认 1 分钟
		}
		s.Register("cost-recalc", CostRecalcJob(costSvc, costJobs, s.log), interval, "")
		n++
	}
	if cfg.Jobs.EventDeliver.Enable && eventJobs != nil {
		interval := cfg.Jobs.EventDeliver.Interval
		if interval <= 0 {
			interval = time.Minute // 默认 1 分钟
		}
		s.Register("event-deliver", EventDeliverJob(eventJobs, s.log), interval, "")
		n++
	}
	if cfg.Jobs.QuoteExpireScan.Enable {
		daily := cfg.Jobs.QuoteExpireScan.Daily
		if daily == "" {
			daily = "07:00" // 兜底（05-quotes §12.3）
		}
		s.Register("quote-expire-scan", func(ctx context.Context, ident supplier.JobIdentity) error {
			_, err := lifecycleSvc.RunExpireScan(ctx, ident)
			return err
		}, 0, daily)
		n++
	}
	if cfg.Jobs.QuoteExpireFinal.Enable {
		daily := cfg.Jobs.QuoteExpireFinal.Daily
		if daily == "" {
			daily = "07:10"
		}
		s.Register("quote-expire-final", func(ctx context.Context, ident supplier.JobIdentity) error {
			_, err := lifecycleSvc.RunExpireFinal(ctx, ident)
			return err
		}, 0, daily)
		n++
	}
	if cfg.Jobs.QuoteAnomalyScan.Enable {
		daily := cfg.Jobs.QuoteAnomalyScan.Daily
		if daily == "" {
			daily = "07:20"
		}
		s.Register("quote-anomaly-scan", func(ctx context.Context, ident supplier.JobIdentity) error {
			_, err := lifecycleSvc.RunAnomalyScan(ctx, 30, ident)
			return err
		}, 0, daily)
		n++
	}
	return n
}

// TestPlatformPoller 是测试任务轮询端口（domain/testplatform.Service 实现）。
type TestPlatformPoller interface {
	Poll(context.Context, int) (tpdomain.PollResult, error)
}

// RegisterTestPlatformPollJob 注册测试平台结果轮询。
//
// 设计成"轮询为主、回调为辅"（Q11）：回调不可达时仍能收口，
// 所以这个任务是测试链路的必需件，而回调接口是可选加速件。
func RegisterTestPlatformPollJob(s *Scheduler, workerCfg config.Worker, tp config.TestPlatform, poller TestPlatformPoller, log *zap.Logger) int {
	if s == nil || !workerCfg.Enable || !tp.Enable || poller == nil {
		return 0
	}
	interval := tp.PollInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	limit := tp.PollLimit
	if limit <= 0 {
		limit = 50
	}
	s.Register("test-platform-poll", func(ctx context.Context, _ supplier.JobIdentity) error {
		result, err := poller.Poll(ctx, limit)
		if err != nil {
			log.Error("test-platform-poll failed", zap.Error(err), zap.Int("scanned", result.Scanned))
			return err
		}
		if result.Completed > 0 || result.Failed > 0 {
			log.Info("test-platform-poll finished",
				zap.Int("scanned", result.Scanned), zap.Int("completed", result.Completed), zap.Int("failed", result.Failed))
		}
		return nil
	}, interval, "")
	return 1
}

// RegisterPlanReminderJob schedules A's due-date notice publisher.
type PlanReminderPublisher interface {
	PublishDuePlanReminders(context.Context, time.Time, int) (int, error)
}

func RegisterPlanReminderJob(s *Scheduler, cfg config.Worker, store PlanReminderPublisher, log *zap.Logger) int {
	if s == nil || !cfg.Enable || !cfg.Jobs.PlanReminder.Enable || store == nil {
		return 0
	}
	interval := cfg.Jobs.PlanReminder.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	s.Register("supply-plan-reminder", func(ctx context.Context, _ supplier.JobIdentity) error {
		count, err := store.PublishDuePlanReminders(ctx, time.Now().UTC(), 100)
		if err != nil {
			log.Error("supply-plan-reminder failed", zap.Error(err))
			return err
		}
		if count > 0 {
			log.Info("supply-plan-reminder notices published", zap.Int("count", count))
		}
		return nil
	}, interval, "")
	return 1
}
