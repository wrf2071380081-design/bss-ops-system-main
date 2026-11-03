// Package worker 的 event_deliver.go：11b event_outbox 推送消费者。
//
// 关键纪律（与 6b-3 COST_RECALC 同源）：
//   - 认领与推送分离：认领只动 event_outbox 行；推送是 HTTP 调用，不动库。
//   - 启动复位（ResetStaleRunning）：每个 tick 前先吃 RUNNING 残留，幂等。
//   - 单条失败不中断其他：遍历每条独立消费。
//   - 指数退避 1min × 5^(retry_count-1)，第 5 次置 DEAD + alert(EVENT_DELIVERY_FAILED, CRITICAL)。
//   - webhook_url 为空时 NOOP：状态机推进但跳过 HTTP（裁决 8）。
package worker

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"model_bss/internal/domain/supplier"
	"model_bss/internal/repo"
)

// EventDeliverJob 构造一个 event_outbox 推送消费者 JobFunc。
// 每次执行：先启动复位（每个 tick 前先吃残留，幂等），再批量 Poll 消费。
// webhook 配置每次 tick 从 sys_config 读（部署侧改了不用重启）。
func EventDeliverJob(
	eventJobs *repo.EventJobRepo,
	links LinkStateTransitioner,
	log *zap.Logger,
) JobFunc {
	return func(ctx context.Context, ident supplier.JobIdentity) error {
		now := time.Now().UTC()
		// 启动复位：进程强杀/超时强退残留的 RUNNING 放回 PENDING。
		// 放在每个 tick 前——第一次跑就吃存量，之后每次空转成本是一发 UPDATE 0 行，可忽略。
		resetN, err := eventJobs.ResetStaleRunning(ctx)
		if err != nil {
			log.Error("event-deliver reset stale running failed", zap.Error(err))
			return err // 复位失败不继续消费，防止并发抢同一批 RUNNING
		}
		if resetN > 0 {
			log.Warn("event-deliver reset stale running events", zap.Int64("count", resetN))
		}

		// webhook 配置每次 tick 从 sys_config 读（部署侧改了不用重启）。
		webhookURL, webhookSecret, err := eventJobs.LoadWebhookConfig(ctx)
		if err != nil {
			log.Error("event-deliver load webhook config failed", zap.Error(err))
			return err
		}
		// webhook_url 为空时**仍然消费**（裁决 8 的语义：状态机推进 + 客户联动，只跳过 HTTP）。
		//
		// 注意：event_outbox 没有"投递结果"字段（只有 status/retry_count/next_run_at），
		// 所以未配 webhook 时事件会被标记 DONE 且**不会补投**——之后配了 webhook 只对**新事件**生效。
		// 这是刻意的：事件是运维通知而不是业务账本，不要求补发；反过来，如果像旧实现那样在 poll 之前就
		// return，事件会永远停在 PENDING，retry_count 不涨、第 5 次 DEAD 的告警也就永远不会触发——
		// 那等于这套投递机制在未配 webhook 的环境里是死的，而且没人知道。
		if webhookURL == "" {
			// info 级（不是 debug）：线上默认级别看不到 debug，等于什么都没记。
			log.Info("event-deliver: webhook 未配置，事件仅推进状态机与客户联动，不做 HTTP 投递（历史事件不补投）")
		}

		rows, err := eventJobs.PollDue(ctx, now, 50)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		log.Info("event-deliver polling", zap.Int("due", len(rows)))
		var client *DeliverClient
		if webhookURL != "" {
			client = NewDeliverClient(webhookURL, webhookSecret)
		}
		var firstErr error
		for _, row := range rows {
			claimed, err := eventJobs.Claim(ctx, row.ID, now)
			if err != nil {
				log.Error("event-deliver claim failed", zap.Int64("event_id", row.ID), zap.Error(err))
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if !claimed {
				log.Debug("event-deliver event claimed by another consumer, skip", zap.Int64("event_id", row.ID))
				continue
			}
			// 1) 客户侧联动（资源上下架 → 绑定状态机）。与 webhook 是否配置无关。
			//
			// 放在投递之前：联动是业务数据修正，不能因为"通知没发出去"就不做；
			// 且联动失败时整条事件按失败重试（下一次重跑联动是幂等的），
			// 避免"通知已发、联动没做"这种无法从外部发现的不一致。
			var syncErr error
			if links == nil {
				// 客户绑定服务未装配（例如手工裁剪部署）：不阻断事件流转，
				// 但要留下痕迹——否则"联动没跑"会像"没发生过"一样沉默。
				if isResourceStateEvent(row.EventType) {
					log.Warn("event-deliver: link service 未装配，跳过客户联动",
						zap.Int64("event_id", row.ID), zap.String("event_type", row.EventType))
				}
			} else {
				syncErr = SyncResourceLinks(ctx, links, row.ID, row.EventType, row.Payload, log)
			}
			if syncErr != nil {
				log.Error("event-deliver link sync failed", zap.Int64("event_id", row.ID), zap.Error(syncErr))
				if err := eventJobs.MarkFailed(ctx, row.ID, syncErr.Error(), now); err != nil {
					log.Error("event-deliver mark failed failed", zap.Int64("event_id", row.ID), zap.Error(err))
				}
				if firstErr == nil {
					firstErr = syncErr
				}
				continue
			}
			// 2) 推送（HTTP，不动库）。webhook 未配置时跳过，但事件照常标记 DONE。
			if client == nil {
				if err := eventJobs.MarkDone(ctx, row.ID, now); err != nil {
					log.Error("event-deliver mark done failed", zap.Int64("event_id", row.ID), zap.Error(err))
					if firstErr == nil {
						firstErr = err
					}
				}
				continue
			}
			deliverErr := client.Deliver(ctx, row.ID, row.EventType, json.RawMessage(row.Payload), row.CreatedAt)
			if deliverErr == nil {
				if err := eventJobs.MarkDone(ctx, row.ID, now); err != nil {
					log.Error("event-deliver mark done failed", zap.Int64("event_id", row.ID), zap.Error(err))
					if firstErr == nil {
						firstErr = err
					}
				}
				continue
			}
			// 推送失败：MarkFailed（退避 + 第 5 次 DEAD + alert）
			log.Warn("event-deliver push failed", zap.Int64("event_id", row.ID), zap.Error(deliverErr))
			if err := eventJobs.MarkFailed(ctx, row.ID, deliverErr.Error(), now); err != nil {
				log.Error("event-deliver mark failed failed", zap.Int64("event_id", row.ID), zap.Error(err))
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		return firstErr
	}
}
