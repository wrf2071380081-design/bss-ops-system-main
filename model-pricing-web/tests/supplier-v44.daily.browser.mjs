import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {chromium} from 'playwright';
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,'model_bss');assert.ok(process.env.BSS_TEST_PASSWORD&&process.env.BSS_NATIVE_MANIFEST);
const f=JSON.parse(await readFile(process.env.BSS_NATIVE_MANIFEST,'utf8'));assert.match(f.Run,/^QA-NATIVE-/);
const phase=process.env.BSS_NATIVE_PHASE||'profile',base='http://127.0.0.1:5173',out='.qa/supplier-v44-20261027';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'msedge',headless:true}),sessions=[],errors=[];
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6s0sAAAAASUVORK5CYII=','base64');
async function login(name){const context=await browser.newContext({viewport:{width:1600,height:1150},acceptDownloads:true}),page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fsuppliers`);await page.locator('#login-account').fill(name);await page.locator('#login-password').fill(process.env.BSS_TEST_PASSWORD);const pending=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/auth/login');await page.getByRole('button',{name:'登录',exact:true}).click();const r=await pending;assert.equal(r.status(),200);const s={context,page,auth:{Authorization:`Bearer ${(await r.json()).data.token}`}};sessions.push(s);return s;}
async function get(s,path){const r=await s.page.request.get(`${base}/api/internal${path}`,{headers:s.auth});assert.equal(r.status(),200,await r.text());return(await r.json()).data;}
async function post(s,path,data,key=crypto.randomUUID()){return s.page.request.post(`${base}/api/internal${path}`,{headers:{...s.auth,'Idempotency-Key':key},data});}
async function open(s,tab='profile'){await s.page.goto(`${base}/#/internal/suppliers/${f.Supplier}/models?tab=${tab}`);await s.page.getByRole('tab',{name:tab==='profile'?'档案与办理':tab==='quotes'?'该供应商报价':'接入模型',exact:true}).click();await s.page.getByRole('heading',{name:f.Run,exact:true}).waitFor();}
async function rename(s,name){await s.page.getByRole('button',{name:'维护档案',exact:true}).click();const d=s.page.getByRole('dialog',{name:'维护供应商主体',exact:true});await d.getByRole('textbox',{name:'供应商名称',exact:false}).fill(name);await d.getByRole('textbox',{name:'操作说明',exact:false}).fill(f.Run);const[r]=await Promise.all([s.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal/supplier-entities/${f.Supplier}`&&r.request().method()==='PUT'),d.getByRole('button',{name:'保存',exact:true}).click()]);assert.equal(r.status(),200,await r.text());await d.waitFor({state:'hidden'});return(await r.json()).data;}
async function batch(s,action,all=true){
 await open(s,'models');const table=s.page.locator('.supplier-model-panel > .el-tabs .el-tab-pane').filter({visible:true}).locator('.el-table').first();
 await table.locator('.el-table__row').first().waitFor();
 if(all)await table.locator('.el-table__header-wrapper .el-checkbox').check();
 else for(const row of await table.locator('.el-table__row').filter({hasText:'qa-extra-'}).all())await row.locator('.el-checkbox').click();
 const labels={CONFIGURE:'登记配置完成',CONFIRM:'确认已开通',CANCEL:'取消所选接入'},name=labels[action];
 await s.page.locator('.batch-access-actions').getByRole('button',{name,exact:true}).click();const d=s.page.getByRole('dialog',{name,exact:true});
 if(action==='CONFIGURE'){
  for(const item of await get(s,`/flows/submissions/${f.Submissions[0]}`).then(v=>v.rows)){
   await d.locator('.el-select').filter({has:s.page.getByRole('combobox',{name:`模型 ${item.resource_id} 正式配置`,exact:true})}).click();
   await s.page.getByRole('listbox',{name:`模型 ${item.resource_id} 正式配置`,exact:true}).getByRole('option').filter({hasText:f.Run}).click();
  }
 }
 if(action!=='CANCEL'){await d.getByText('已在外部平台完成所选模型的实际操作',{exact:true}).click();await d.getByRole('textbox',{name:'批量接入完成依据',exact:true}).fill('QA explicitly marked external operation evidence');}
 await d.getByRole('textbox',{name:'批量接入操作原因',exact:true}).fill(f.Run);
 const[r]=await Promise.all([s.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal/supplier-entities/${f.Supplier}/resource-actions`&&r.request().method()==='POST'),d.getByRole('button',{name,exact:true}).click()]);
 assert.equal(r.status(),200,await r.text());const result=(await r.json()).data;
 const request=r.request(),replay=await post(s,`/supplier-entities/${f.Supplier}/resource-actions`,request.postDataJSON(),request.headers()['idempotency-key']);assert.equal(replay.status(),200);assert.deepEqual((await replay.json()).data,result);
 await s.page.screenshot({path:`${out}/${action}.png`,fullPage:true});await d.getByRole('button',{name:'关闭结果',exact:true}).click();return result.results;
}
try{
 const actor=await login('buyer_a'),owner=await login('smoke_admin');
 if(phase==='profile'){
  await open(owner);const p=await get(owner,`/supplier-entities/${f.Supplier}`);assert.equal(p.aliases_supported,true);
  const renamed=`QA-RENAMED-${Date.now()}`;await rename(owner,renamed);
  const lookup=await get(owner,`/supplier-entities?keyword=${encodeURIComponent(f.Run)}&page=1&size=20`);assert.equal(lookup.list.length,1);assert.equal(lookup.list[0].id,f.Supplier);assert.equal(lookup.list[0].name,renamed);
  await owner.page.goto(`${base}/#/internal/suppliers?keyword=${encodeURIComponent(f.Run)}`);await owner.page.locator('tr').filter({hasText:renamed}).waitFor();
  await owner.page.goto(`${base}/#/internal/suppliers/${f.Supplier}/models?tab=profile`);await owner.page.getByRole('tab',{name:'档案与办理',exact:true}).click();await owner.page.getByRole('button',{name:'维护档案',exact:true}).waitFor();await rename(owner,f.Run);
  const restored=await get(owner,`/supplier-entities/${f.Supplier}`);assert.ok(restored.aliases.includes(renamed));assert.ok(!restored.aliases.includes(f.Run));
  const progress=await get(owner,`/flows/suppliers/${f.Supplier}/progress`);assert.equal(progress.list.length,6);assert.ok(progress.list.every(n=>n.assignee_id==='1'&&n.assignee_name));
  assert.equal(await owner.page.getByText('待 B 流程数据接入',{exact:true}).count(),0);
  const existing=await get(owner,`/supplier-entities/${f.Supplier}/qualifications?page=1&size=20`);
  if(!existing.list.length){await owner.page.getByRole('button',{name:'上传资质',exact:true}).click();let d=owner.page.getByRole('dialog',{name:'上传供应商资质',exact:true});
  for(const[name,value]of[['资质名称','QA营业执照'],['资质类型','营业执照'],['有效期起始','2026-01-01'],['有效期截止','2027-01-01'],['操作说明',f.Run]])await d.locator('.el-form-item').filter({hasText:name}).locator('input').fill(value);
  await d.locator('input[type=file]').setInputFiles({name:'qualification.png',mimeType:'image/png',buffer:png});
  const[r]=await Promise.all([owner.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal/supplier-entities/${f.Supplier}/qualifications`&&r.request().method()==='POST'),d.getByRole('button',{name:'保存资质',exact:true}).click()]);assert.equal(r.status(),200,await r.text());await d.waitFor({state:'hidden'});}
  const q=await get(owner,`/supplier-entities/${f.Supplier}/qualifications?page=1&size=20`);assert.equal(q.metadata_supported,true);assert.equal(q.list.length,1);assert.equal(q.list[0].qualification_type,'营业执照');assert.equal(q.list[0].review_status??null,null);assert.equal(q.list[0].revision,'1');
  const file=await owner.page.request.get(`${base}/api/internal/supplier-entities/${f.Supplier}/qualifications/${q.list[0].id}/file`,{headers:owner.auth});assert.equal(file.status(),200);assert.deepEqual(await file.body(),png);
  const wrong=await actor.page.request.get(`${base}/api/internal/supplier-entities/${f.Supplier}/qualifications/${q.list[0].id}/file`,{headers:actor.auth});assert.equal(wrong.status(),403);
  await owner.page.screenshot({path:`${out}/aliases-qualification-progress.png`,fullPage:true});console.log('v44 actual UI rename/search/rename-back, qualification upload, protected download and real six-node progress passed');
 }else if(phase==='persist'){
  const q=(await get(owner,`/supplier-entities/${f.Supplier}/qualifications?page=1&size=20`)).list[0];assert.equal(q.revision,'3');assert.equal(q.valid_from,'2026-01-01');assert.equal(q.valid_to,'2027-01-01');
  const file=await owner.page.request.get(`${base}/api/internal/supplier-entities/${f.Supplier}/qualifications/${q.id}/file`,{headers:owner.auth});assert.equal(file.status(),200);assert.deepEqual(await file.body(),png);
  const progress=await get(owner,`/flows/suppliers/${f.Supplier}/progress`);assert.equal(progress.list.filter(n=>n.key==='CONFIG'&&n.row_status==='CANCELED').length,2);
  await open(owner);await owner.page.locator('tr').filter({hasText:'QA营业执照草稿'}).waitFor();await owner.page.screenshot({path:`${out}/persistent-qualification.png`,fullPage:true});
  console.log('after same API restart, qualification metadata/file and real canceled-row projections remain intact');
 }else if(phase==='all-cancel'){
  const before=await get(actor,`/flows/submissions/${f.Submissions[0]}`);assert.equal(before.rows.length,1);const id=before.rows[0].resource_id,resource=await get(actor,`/resources/${id}`);
  const wrong=await post(owner,`/supplier-entities/${f.Supplier}/resource-actions`,{action:'CANCEL',items:[{resource_id:id,expected_revision:resource.revision}],reason:f.Run});assert.equal((await wrong.json()).data.results[0].status,403);
  const canceled=await batch(actor,'CANCEL');assert.equal(canceled.length,1);assert.equal(canceled[0].status,200);
  const after=await get(actor,`/flows/submissions/${f.Submissions[0]}`);assert.equal(after.status,'COMPLETED');assert.equal(after.rows[0].status,'CANCELED');assert.equal(after.nodes.find(n=>n.key==='CONFIG').status,'COMPLETED');assert.equal(after.nodes.filter(n=>n.key==='CONTRACT').length,0);assert.equal(after.can_handle_contract,false);
  assert.equal((await get(actor,`/resources/${id}`)).state,resource.state);
  const todo=await get(actor,`/workbench/inbox?state=TODO&size=100&keyword=${encodeURIComponent(f.Run)}`);assert.equal(todo.list.length,0);
  console.log('all rows canceled: actor403 protected, CONFIG closed, unstarted CONTRACT disabled, no false admission or residual TODO');
 }else if(phase==='closeout-logs'){
  const uploaded=await post(owner,`/supplier-entities/${f.Supplier}/qualifications`,{name:'QA日志资质',file_name:'qualification.png',content_base64:png.toString('base64'),qualification_type:'营业执照',valid_from:'2026-01-01',valid_to:'2027-01-01',reason:f.Run});assert.equal(uploaded.status(),200,await uploaded.text());
  const q=(await get(owner,`/supplier-entities/${f.Supplier}/qualifications?page=1&size=20`)).list[0];
  const revised=await owner.page.request.put(`${base}/api/internal/supplier-entities/${f.Supplier}/qualifications/${q.id}`,{headers:{...owner.auth,'Idempotency-Key':crypto.randomUUID()},data:{name:'QA日志资质修订',qualification_type:'营业执照',valid_from:'2026-01-01',valid_to:'2028-01-01',expected_revision:q.revision,reason:f.Run}});assert.equal(revised.status(),200,await revised.text());
  for(const action of ['CONFIGURE','CANCEL']){const results=await batch(actor,action);assert.ok(results.length>0&&results.every(r=>r.status===200));}
  const records=await get(owner,`/suppliers/${f.Supplier}/operations?page=1&size=100`);
  for(const [category,action]of [['supplier_qualification','UPDATE'],['resource','CONFIG_COMPLETE'],['resource','CANCEL_ACCESS']]){
   const filtered=await get(owner,`/suppliers/${f.Supplier}/operations?entity_type=${category}&page=1&size=20`);
   assert.ok(filtered.list.some(r=>r.action===action),`missing ${category}/${action}`);
   assert.ok(filtered.list.every(r=>r.entity_type===category&&r.supplier_id===f.Supplier&&!('after'in r)&&!('file_ref'in r)&&!('api_key'in r)));
   assert.ok(records.list.some(r=>r.action===action&&r.entity_type===category));
  }
  await open(owner);await owner.page.getByRole('tab',{name:'操作记录',exact:true}).click();
  const first=owner.page.locator('.operation-filters').locator('..');await first.locator('.el-table__row').filter({hasText:'资质 · 维护'}).waitFor();
  const resourceRow=first.locator('.el-table__row').filter({hasText:'取消接入'}).first();await resourceRow.getByRole('button',{name:'查看',exact:true}).click();
  let detail=owner.page.getByRole('dialog',{name:'操作记录详情',exact:true});await detail.waitFor();assert.ok(!(await detail.innerText()).includes('file_ref'));
  await detail.getByRole('button',{name:'查看关联资源 / 计划',exact:true}).click();await owner.page.waitForURL(new RegExp(`/internal/model-plaza/${f.Resource}`));
  await open(owner);await owner.page.getByRole('tab',{name:'操作记录',exact:true}).click();
  await owner.page.locator('.operation-filters').locator('..').locator('.el-table__row').filter({hasText:'资质 · 维护'}).getByRole('button',{name:'查看',exact:true}).click();
  detail=owner.page.getByRole('dialog',{name:'操作记录详情',exact:true});await detail.getByRole('button',{name:'查看业务资料',exact:true}).click();await owner.page.waitForURL(/section=supplier_qualification/);await owner.page.locator('tr').filter({hasText:'QA日志资质修订'}).waitFor();
  await owner.page.screenshot({path:`${out}/closeout-log-navigation.png`,fullPage:true});console.log('actual qualification UPDATE + CONFIG_COMPLETE + CANCEL_ACCESS: safe scoped logs, categories and resource/qualification navigation passed');
 }else if(phase==='qualification'){
  await open(owner);let q=(await get(owner,`/supplier-entities/${f.Supplier}/qualifications?page=1&size=20`)).list[0];assert.ok(q);
  const path=`/supplier-entities/${f.Supplier}/qualifications/${q.id}`;
  const data={name:q.name,qualification_type:q.qualification_type,valid_from:q.valid_from,valid_to:q.valid_to,expected_revision:q.revision,reason:f.Run};
  const denied=await actor.page.request.put(`${base}/api/internal${path}`,{headers:{...actor.auth,'Idempotency-Key':crypto.randomUUID()},data});assert.equal(denied.status(),403);
  const invalid=await owner.page.request.put(`${base}/api/internal${path}`,{headers:{...owner.auth,'Idempotency-Key':crypto.randomUUID()},data:{...data,valid_from:'2028-01-01',valid_to:'2027-01-01'}});assert.equal(invalid.status(),400);
  await owner.page.locator('tr').filter({hasText:q.name}).getByRole('button',{name:'维护',exact:true}).click();const d=owner.page.getByRole('dialog',{name:'维护供应商资质',exact:true});
  await d.getByRole('textbox',{name:'资质名称',exact:true}).fill('QA营业执照草稿');
  const concurrent=await owner.page.request.put(`${base}/api/internal${path}`,{headers:{...owner.auth,'Idempotency-Key':crypto.randomUUID()},data:{...data,name:'QA营业执照服务器'}});assert.equal(concurrent.status(),200,await concurrent.text());
  const[stale]=await Promise.all([owner.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}`&&r.request().method()==='PUT'),d.getByRole('button',{name:'保存资质',exact:true}).click()]);assert.equal(stale.status(),409,await stale.text());assert.equal(await d.getByRole('textbox',{name:'资质名称',exact:true}).inputValue(),'QA营业执照草稿');
  await d.getByRole('button',{name:'刷新最新修订并保留草稿',exact:true}).click();const retry=d.getByRole('button',{name:'按最新修订重试',exact:true});await retry.waitFor();
  const[saved]=await Promise.all([owner.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}`&&r.request().method()==='PUT'),retry.click()]);assert.equal(saved.status(),200,await saved.text());await d.waitFor({state:'hidden'});
  const latest=(await get(owner,`/supplier-entities/${f.Supplier}/qualifications?page=1&size=20`)).list[0];assert.equal(latest.name,'QA营业执照草稿');assert.equal(BigInt(latest.revision),BigInt(q.revision)+2n);assert.equal(latest.review_status??null,null);
  await owner.page.screenshot({path:`${out}/qualification-conflict-recovered.png`,fullPage:true});console.log('v44 real qualification: non-owner403, invalid dates400, stale409 preserves draft, refresh/retry200, review remains unknown');
 }else{
  assert.equal(phase,'stages');let v=await get(actor,`/flows/submissions/${f.Submissions[0]}`);assert.equal(v.rows.length,3);
  const resource=await get(actor,`/resources/${f.Resource}`);
  if(resource.state!=='ADMITTED'){
  const refreshed=await post(actor,`/resources/${f.Resource}/refresh-test-evidence`,{expected_revision:resource.revision,reason:f.Run});assert.equal(refreshed.status(),200,await refreshed.text());
  const noStage=await post(actor,`/supplier-entities/${f.Supplier}/resource-actions`,{action:'CONFIRM',items:[{resource_id:f.Resource,expected_revision:(await get(actor,`/resources/${f.Resource}`)).revision}],reason:f.Run,external_confirmed:true,completion_note:f.Run});assert.equal((await noStage.json()).data.results[0].status,409);
  const configured=await batch(actor,'CONFIGURE');assert.equal(configured.length,3);assert.ok(configured.every(r=>r.status===200));
  const confirmed=await batch(actor,'CONFIRM');assert.equal(confirmed.filter(r=>r.status===200).length,1);assert.equal(confirmed.filter(r=>r.status===409).length,2);}
  const admitted=await get(actor,`/resources/${f.Resource}`);assert.equal(admitted.state,'ADMITTED');
  const cancelAdmitted=await post(actor,`/supplier-entities/${f.Supplier}/resource-actions`,{action:'CANCEL',items:[{resource_id:f.Resource,expected_revision:admitted.revision}],reason:f.Run});assert.equal((await cancelAdmitted.json()).data.results[0].status,409);assert.equal((await get(actor,`/resources/${f.Resource}`)).state,'ADMITTED');
  const canceled=await batch(actor,'CANCEL',false);assert.equal(canceled.length,2);assert.ok(canceled.every(r=>r.status===200));
  v=await get(actor,`/flows/submissions/${f.Submissions[0]}`);assert.equal(v.rows.filter(r=>r.status==='CANCELED').length,2);assert.equal(v.rows.filter(r=>r.status==='ADMITTED').length,1);assert.equal(v.nodes.find(n=>n.key==='CONFIG').status,'COMPLETED');assert.equal(v.nodes.find(n=>n.key==='CONTRACT').status,'IN_PROGRESS');
  await open(actor,'quotes');await actor.page.locator('tr').filter({hasText:f.Submissions[0]}).getByRole('button',{name:'查看报价',exact:true}).click();const d=actor.page.getByRole('dialog',{name:'申报审批与分派',exact:true});await d.getByText('qa-extra-1',{exact:true}).waitFor();
  console.log('real three-model stages: CONFIGURE 3 success, CONFIRM 1 success/2 blocked, CANCEL pending 2 success; admitted resource protected; B completion and history detail passed');
 }
 assert.deepEqual(errors,[]);await writeFile(`${out}/${phase}-checks.json`,JSON.stringify({passed:true,phase,database:44,realAuthentication:true,realAPI:true,noRouteMocks:true,inspectorCalled:false,pageErrors:errors},null,2));
}catch(e){for(let i=0;i<sessions.length;i++){await sessions[i].page.screenshot({path:`${out}/${phase}-failure-${i}.png`,fullPage:true}).catch(()=>{});await writeFile(`${out}/${phase}-failure-${i}.txt`,await sessions[i].page.locator('body').innerText()).catch(()=>{});}throw e;}
finally{for(const s of sessions){await s.page.request.post(`${base}/api/internal/auth/logout`,{headers:s.auth}).catch(()=>{});await s.context.close();}await browser.close();}
