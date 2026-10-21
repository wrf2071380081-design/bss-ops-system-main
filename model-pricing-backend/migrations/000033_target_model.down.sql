-- 000033_target_model（down）：按依赖反序删除，全部 IF EXISTS，幂等。
-- 注意：resource.latest_test_report_id 与 test_report.resource_id 构成循环外键，
-- 先解掉 resource 上的后置外键，再删 test_report / test_job / resource。

DROP TABLE IF EXISTS margin_override;
DROP TABLE IF EXISTS assignment_log;
DROP TABLE IF EXISTS operation_log;
DROP TABLE IF EXISTS notice_read;
DROP TABLE IF EXISTS notice;
DROP TABLE IF EXISTS plan;
DROP TABLE IF EXISTS credit_line;
DROP TABLE IF EXISTS contract;
DROP TABLE IF EXISTS quote_version;
DROP TABLE IF EXISTS quote_line;
DROP TABLE IF EXISTS quote;
DROP TABLE IF EXISTS customer_link;
DROP TABLE IF EXISTS customer;
DROP TABLE IF EXISTS onboarding_batch;
DROP TABLE IF EXISTS onboarding_node;
DROP TABLE IF EXISTS onboarding_flow;
DROP TABLE IF EXISTS submission_version;
DROP TABLE IF EXISTS submission_row;
DROP TABLE IF EXISTS submission;
DROP TABLE IF EXISTS webhook_event;

-- 解循环外键后再删测试与资源
ALTER TABLE IF EXISTS resource DROP CONSTRAINT IF EXISTS fk_resource_latest_report;
DROP TABLE IF EXISTS test_report;
DROP TABLE IF EXISTS test_job;
DROP TABLE IF EXISTS resource;

DROP TABLE IF EXISTS supplier_qualification;
DROP TABLE IF EXISTS supplier_formal_config;
DROP TABLE IF EXISTS supplier;

DROP TABLE IF EXISTS crawl_diff;
DROP TABLE IF EXISTS crawl_job;
DROP TABLE IF EXISTS standard_model_version;
DROP TABLE IF EXISTS standard_model;
