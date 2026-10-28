-- 000046 回滚：删除主体级测试凭据表。
--
-- submission_row.test_base_url / test_api_key_enc 上的快照数据**保留不删**：
-- 回滚后行级凭据语义恢复，这些快照正好能当回旧口径的初始值，
-- 供应商若在回滚前改过主体凭据，改回行级后需要重新逐行确认一次。

DROP TABLE IF EXISTS supplier_test_credential;

COMMENT ON COLUMN submission_row.test_base_url    IS '测试凭据';
COMMENT ON COLUMN submission_row.test_api_key_enc IS NULL;
