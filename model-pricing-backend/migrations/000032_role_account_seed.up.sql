-- 000032_role_account_seed：补 8 个「单角色」内部门户账号，用于权限边界演示与验收。
--
-- 背景：内部门户此前只有 smoke_admin 一个管理角色账号，且它是
-- PLATFORM_ADMIN + MODEL_OPS + PRICING_OP **三合一**——任何功能都能过，
-- 无法验证「单角色能看到什么、不能看到什么」。
-- 本迁移按内部角色逐个建号，每个账号只授 1 个角色，与 role_mutex 无交集。
--
-- 账号清单（均为 INTERNAL / STAFF / ACTIVE，口令 Test@1234）：
--   model_ops  → MODEL_OPS      销售之外的全模型写
--   pricing_op → PRICING_OP     成本参数 / 价目表 / 传导决策
--   finance_op → FINANCE        汇率锁定
--   sales_op   → SALES          客户报价 / 特价
--   procure_op → PROCUREMENT    锁主供应商 / 报价审批
--   ops_admin  → OPS_ADMIN      零写（000031 收敛后）
--   retro_op   → RETRO_OP       报价追溯
--   audit_view → AUDIT_READONLY 审计模块动作
--
-- 口令：Test@1234（bcrypt cost 10，与库里 smoke_admin 等账号同口径）。
-- ⚠️ bcrypt 每次加盐结果都不同，**不能用「密文是否相同」判断是否同口令**。
--
-- 幂等：按 login_id 判存；role_grant 无唯一约束，用 NOT EXISTS 兜底。
-- 手机号段 13900001001–08，避开已占用：13800000011/12、13900000001/02/03/99/200。
--
-- 变量统一 v_ 前缀：避免与 role_grant.staff_id 等同名列冲突（PL/pgSQL 报 ambiguous）。

DO $$
DECLARE
  v_org_id   bigint;
  v_staff_id bigint;
  v_role_id  bigint;
  v_role_cd  text;
  v_login    text;
  v_name     text;
  v_mobile   text;
  v_i        int;
  v_hash     text := '$2a$10$.cpUKKDEVdcZbf0.AmHQhu62mH5PbM0JDf0Q22nzZQL5Pm2dFk6TK';
  v_plan text[][] := ARRAY[
    ['model_ops',  '模型运营',   'MODEL_OPS',      '13900001001'],
    ['pricing_op', '定价运营',   'PRICING_OP',     '13900001002'],
    ['finance_op', '财务',       'FINANCE',        '13900001003'],
    ['sales_op',   '销售',       'SALES',          '13900001004'],
    ['procure_op', '采购',       'PROCUREMENT',    '13900001005'],
    ['ops_admin',  '运营管理',   'OPS_ADMIN',      '13900001006'],
    ['retro_op',   '补录专员',   'RETRO_OP',       '13900001007'],
    ['audit_view', '审计只读',   'AUDIT_READONLY', '13900001008']
  ];
BEGIN
  -- 组织：与 000013 / 000027 同一兜底口径
  SELECT id INTO v_org_id FROM org_unit ORDER BY id LIMIT 1;
  IF v_org_id IS NULL THEN
    INSERT INTO org_unit(parent_id, name, unit_type, path, status, created_at, updated_at,
                         request_id, created_by, updated_by)
    VALUES (NULL, '总部', 'COMPANY', '/', 'ACTIVE', now(), now(), NULL, 1, 1)
    RETURNING id INTO v_org_id;
    UPDATE org_unit SET path = '/' || v_org_id || '/' WHERE id = v_org_id;
  END IF;

  FOR v_i IN 1 .. array_length(v_plan, 1) LOOP
    v_login   := v_plan[v_i][1];
    v_name    := v_plan[v_i][2];
    v_role_cd := v_plan[v_i][3];
    v_mobile  := v_plan[v_i][4];

    -- 账号已存在（含本地手工种过的同名账号）则整体跳过，不制造第二行 staff
    IF EXISTS (SELECT 1 FROM account WHERE portal_type = 'INTERNAL' AND login_id = v_login) THEN
      RAISE NOTICE '000032 跳过：INTERNAL/% 已存在', v_login;
      CONTINUE;
    END IF;

    -- 员工：手机号唯一冲突时复用原记录
    INSERT INTO internal_staff(org_unit_id, name, mobile, status, created_at, updated_at)
    VALUES (v_org_id, v_name, v_mobile, 'ACTIVE', now(), now())
    ON CONFLICT (mobile) DO NOTHING
    RETURNING id INTO v_staff_id;
    IF v_staff_id IS NULL THEN
      SELECT id INTO v_staff_id FROM internal_staff WHERE mobile = v_mobile;
    END IF;

    -- 账号
    INSERT INTO account(portal_type, owner_type, owner_id, login_id, password_hash, status,
                        created_at, updated_at)
    VALUES ('INTERNAL', 'STAFF', v_staff_id, v_login, v_hash, 'ACTIVE', now(), now());

    -- 单角色授权
    SELECT id INTO v_role_id FROM role WHERE code = v_role_cd;
    IF v_role_id IS NULL THEN
      RAISE EXCEPTION '000032 中止：role 不存在 %', v_role_cd;
    END IF;
    INSERT INTO role_grant(staff_id, role_id, granted_by, granted_at, created_at, updated_at)
    SELECT v_staff_id, v_role_id, 1, now(), now(), now()
     WHERE NOT EXISTS (
           SELECT 1 FROM role_grant g WHERE g.staff_id = v_staff_id AND g.role_id = v_role_id
         );

    RAISE NOTICE '000032 已建账号 % / % / %', v_login, v_role_cd, v_mobile;
  END LOOP;
END $$;
