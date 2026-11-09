-- 000053_demo_leader_staff：新增「领导 / 员工」两个业务角色 + 5 个演示人员。
--
-- 背景：原型（TokenOps 2026-11-04）只有两级——领导 / 员工，5 个固定人员：
--   林舟(领导) 许澄(领导) 陈乐(员工) 周宁(员工) 沈安(员工)
-- 而库里的权限体系是 11 个职能角色 × 60 个权限点（M1..M12 × V/E/A/C/P）。
--
-- 本迁移**不动原有 11 个角色**，只在其上新增两个"粗角色"，作为演示与后续演进的基础：
--   LEADER 领导：全部 60 个权限点，can_approve_supplier = true
--   STAFF  员工：全部 12 个 :V + 5 个业务 :E（M1/M3/M4/M8/M9），无 :A（审批）、无 :C（配置）、无 :P（特权）
--
-- 为什么这样切：
--   1. 审批（:A）与配置（:C）留给 LEADER，正好对应原型"仅超管/领导可审批"；
--   2. 员工保留业务编辑能力（供应商接入配置、客户开通、报价编辑），否则演示走不完主链路；
--   3. 未来的细分角色（MODEL_OPS / SALES / PROCUREMENT…）可以继续叠加到这两个粗角色之上，
--      也可以给某个人同时授 LEADER + MODEL_OPS，不需要改本迁移之外的结构。
--
-- 口令：与 000032 种子账号同一口令（见该迁移注释，本文不重复明文）。
-- 手机号：13900002001–2005（避开 000032 的 13900001001–08 与更早占用的号段）。
-- 幂等：按 account.login_id 判存；角色按 role.code 判存。

DO $$
DECLARE
  v_org_id  bigint;
  v_role_id bigint;
  v_staff_id bigint;
  v_login   text;
  v_name    text;
  v_role_cd text;
  v_mobile  text;
  v_leader  boolean;
  v_i       int;
  v_hash    text := '$2a$10$.cpUKKDEVdcZbf0.AmHQhu62mH5PbM0JDf0Q22nzZQL5Pm2dFk6TK';
  -- [登录名, 姓名, 角色, 手机号, 是否领导]
  v_plan text[][] := ARRAY[
    ['linzhou',  '林舟', 'LEADER', '13900002001', 'true'],
    ['xucheng',  '许澄', 'LEADER', '13900002002', 'true'],
    ['chenle',   '陈乐', 'STAFF',  '13900002003', 'false'],
    ['zhouning', '周宁', 'STAFF',  '13900002004', 'false'],
    ['shenan',   '沈安', 'STAFF',  '13900002005', 'false']
  ];
BEGIN
  -- ---------- 1. 两个角色 ----------
  INSERT INTO role(code, name, data_scope, field_mask, is_functional, created_at, updated_at)
  VALUES ('LEADER', '领导', 'ALL', NULL, true, now(), now())
  ON CONFLICT (code) DO NOTHING;

  INSERT INTO role(code, name, data_scope, field_mask, is_functional, created_at, updated_at)
  VALUES ('STAFF', '员工', 'ALL', NULL, true, now(), now())
  ON CONFLICT (code) DO NOTHING;

  -- ---------- 2. 权限授予 ----------
  -- LEADER：全部权限点
  INSERT INTO role_permission(role_id, permission_point_id, created_at, updated_at)
  SELECT r.id, p.id, now(), now()
    FROM role r JOIN permission_point p ON true
   WHERE r.code = 'LEADER'
  ON CONFLICT (role_id, permission_point_id) DO NOTHING;

  -- STAFF：全部 :V（能看）+ 业务 :E（能改），不给 :A / :C / :P
  INSERT INTO role_permission(role_id, permission_point_id, created_at, updated_at)
  SELECT r.id, p.id, now(), now()
    FROM role r
    JOIN permission_point p
      ON p.action_code = 'V'
      OR (p.action_code = 'E' AND p.module_code IN ('M1','M3','M4','M8','M9'))
   WHERE r.code = 'STAFF'
  ON CONFLICT (role_id, permission_point_id) DO NOTHING;

  -- ---------- 3. 组织节点（与 000013 / 000032 同一兜底口径） ----------
  SELECT id INTO v_org_id FROM org_unit ORDER BY id LIMIT 1;
  IF v_org_id IS NULL THEN
    INSERT INTO org_unit(parent_id, name, unit_type, path, status, created_at, updated_at,
                         request_id, created_by, updated_by)
    VALUES (NULL, '总部', 'COMPANY', '/', 'ACTIVE', now(), now(), NULL, 1, 1)
    RETURNING id INTO v_org_id;
    UPDATE org_unit SET path = '/' || v_org_id || '/' WHERE id = v_org_id;
  END IF;

  -- ---------- 4. 五个演示人员 ----------
  FOR v_i IN 1 .. array_length(v_plan, 1) LOOP
    v_login   := v_plan[v_i][1];
    v_name    := v_plan[v_i][2];
    v_role_cd := v_plan[v_i][3];
    v_mobile  := v_plan[v_i][4];
    v_leader  := v_plan[v_i][5]::boolean;

    IF EXISTS (SELECT 1 FROM account WHERE portal_type = 'INTERNAL' AND login_id = v_login) THEN
      RAISE NOTICE '000053 跳过：INTERNAL/% 已存在', v_login;
      CONTINUE;
    END IF;

    INSERT INTO internal_staff(org_unit_id, name, mobile, status, can_approve_supplier,
                               created_at, updated_at)
    VALUES (v_org_id, v_name, v_mobile, 'ACTIVE', v_leader, now(), now())
    ON CONFLICT (mobile) DO NOTHING
    RETURNING id INTO v_staff_id;

    IF v_staff_id IS NULL THEN
      SELECT id INTO v_staff_id FROM internal_staff WHERE mobile = v_mobile;
      -- 手机号已存在但姓名/审批权不同：以本迁移为准收敛
      UPDATE internal_staff
         SET name = v_name, can_approve_supplier = v_leader, updated_at = now()
       WHERE id = v_staff_id;
    END IF;

    INSERT INTO account(portal_type, owner_type, owner_id, login_id, password_hash, status,
                        created_at, updated_at)
    VALUES ('INTERNAL', 'STAFF', v_staff_id, v_login, v_hash, 'ACTIVE', now(), now());

    SELECT id INTO v_role_id FROM role WHERE code = v_role_cd;
    IF v_role_id IS NULL THEN
      RAISE EXCEPTION '000053 中止：role 不存在 %', v_role_cd;
    END IF;

    INSERT INTO role_grant(staff_id, role_id, granted_by, granted_at, created_at, updated_at)
    SELECT v_staff_id, v_role_id, 1, now(), now(), now()
     WHERE NOT EXISTS (
             SELECT 1 FROM role_grant g
              WHERE g.staff_id = v_staff_id AND g.role_id = v_role_id
           );

    RAISE NOTICE '000053 已建账号 % / % / % / 领导=%', v_login, v_name, v_role_cd, v_leader;
  END LOOP;
END $$;
