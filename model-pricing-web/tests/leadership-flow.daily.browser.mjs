import assert from 'node:assert/strict';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {chromium} from 'playwright';

// Existing model_bss daily42 only; no route mocks or external Inspector calls.
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,'model_bss');
assert.ok(process.env.BSS_NATIVE_MANIFEST&&process.env.BSS_TEST_PASSWORD);
const f=JSON.parse(await readFile(process.env.BSS_NATIVE_MANIFEST,'utf8'));
assert.ok(f.Run.startsWith('QA-NATIVE-')&&f.LeaderLogin&&f.Submissions.length===1);
const base='http://127.0.0.1:5173',out='.qa/leadership-flow-20261027';
await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'msedge',headless:true});
const sessions=[],errors=[];
async function login(account){
 const context=await browser.newContext({viewport:{width:1500,height:1050}}),page=await context.newPage();
 page.on('pageerror',e=>errors.push(e.message));
 await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fworkbench`);
 await page.locator('#login-account').fill(account);await page.locator('#login-password').fill(process.env.BSS_TEST_PASSWORD);
 const pending=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/auth/login');
 await page.getByRole('button',{name:'登录',exact:true}).click();
 const r=await pending;assert.equal(r.status(),200);const token=(await r.json()).data.token;
 const s={context,page,auth:{Authorization:`Bearer ${token}`}};sessions.push(s);return s;
}
async function get(s,path){const r=await s.page.request.get(`${base}/api/internal${path}`,{headers:s.auth});assert.equal(r.status(),200,await r.text());return (await r.json()).data;}
async function post(s,path,data,key=crypto.randomUUID()){return s.page.request.post(`${base}/api/internal${path}`,{headers:{...s.auth,'Idempotency-Key':key},data});}
async function task(s,type,state='TODO'){
 const inbox=await get(s,`/workbench/inbox?state=${state}&size=100&keyword=${encodeURIComponent(f.Run)}`);
 return inbox.list.filter(i=>i.biz_type===type);
}
try{
 const leader=await login(f.LeaderLogin),handler=await login('buyer_a');
 const path=`/flows/submissions/${f.Submissions[0]}`;
 if(process.env.BSS_NATIVE_PHASE==='complete'){
  const view=await get(handler,path);assert.equal(view.can_handle,false);assert.equal(view.can_approve,false);
  assert.equal((await task(handler,'SUBMISSION_HANDLING')).length,0);
  assert.equal((await task(handler,'SUBMISSION_HANDLING','DONE')).length,1);
  assert.equal((await task(leader,'SUBMISSION_APPROVAL','DONE')).length,1);
  // Actual DONE filter and detail remain readable after CONFIG finishes.
  await handler.page.goto(`${base}/#/internal/workbench`);
  await handler.page.getByRole('checkbox',{name:'待办',exact:true}).uncheck();
  await handler.page.getByRole('checkbox',{name:'待阅',exact:true}).uncheck();
  await handler.page.getByRole('checkbox',{name:'已办',exact:true}).check();
  await handler.page.locator('tr').filter({hasText:f.Run}).filter({hasText:'测试与资源配置办理'}).getByRole('button',{name:'详情',exact:true}).click();
  const dialog=handler.page.getByRole('dialog',{name:'测试与资源配置办理',exact:true});
  await dialog.getByRole('button',{name:'查看资源',exact:true}).waitFor();
  assert.equal(await dialog.getByRole('button',{name:'办理资源',exact:true}).count(),0);
  assert.deepEqual(errors,[]);
  await handler.page.screenshot({path:`${out}/handling-completed.png`,fullPage:true});
  await writeFile(`${out}/completed-checks.json`,JSON.stringify({passed:true,handlingTodoRemoved:true,handlingDoneReadable:true,leaderDoneReadable:true,noDelegatedHandling:true,pageErrors:errors},null,2));
  console.log('completed A handling moved from TODO to DONE; actual workbench detail is read-only');
 }else{
 const current=await get(leader,path);assert.equal(current.status,'PENDING_APPROVAL');assert.equal(current.can_approve,true);assert.equal(current.rows[0].resource_id,f.Resource||'');
 assert.equal((await task(leader,'SUBMISSION_APPROVAL')).length,1);
 assert.equal((await task(handler,'SUBMISSION_HANDLING')).length,0);
 const contractAssignee=process.env.BSS_NATIVE_CONTRACT_ASSIGNEE||'3';
 const data={expected_revision:current.revision,test_assignee_id:'1',contract_assignee_id:contractAssignee,reason:f.Run};
 const denied=await post(handler,`${path}/approve`,data);assert.equal(denied.status(),403,await denied.text());
 const stale=await post(leader,`${path}/approve`,{...data,expected_revision:(BigInt(current.revision)+1n).toString()});assert.equal(stale.status(),409,await stale.text());
 const legacy=await post(leader,`/approvals/submissions/${current.id}/approve`,data);assert.equal(legacy.status(),404);
 const people=(await get(leader,'/workbench/people')).list;
 await leader.page.goto(`${base}/#/internal/workbench`);
 const row=leader.page.locator('tr').filter({hasText:f.Run}).filter({hasText:'供应商模型申报审批'});
 await row.getByRole('button',{name:'详情',exact:true}).click();
 const dialog=leader.page.getByRole('dialog',{name:'申报审批与分派',exact:true});
 await dialog.locator('.el-select').filter({has:leader.page.getByRole('combobox',{name:'测试与资源配置办理人',exact:true})}).click();
 await leader.page.getByRole('listbox',{name:'测试与资源配置办理人',exact:true}).getByRole('option',{name:people.find(p=>String(p.id)==='1').name,exact:true}).click();
 await dialog.locator('.el-select').filter({has:leader.page.getByRole('combobox',{name:'合同与授信办理人',exact:true})}).click();
 await leader.page.getByRole('listbox',{name:'合同与授信办理人',exact:true}).getByRole('option',{name:people.find(p=>String(p.id)===contractAssignee).name,exact:true}).click();
 await dialog.getByRole('textbox',{name:'审批意见',exact:true}).fill(f.Run);
 const [approved]=await Promise.all([leader.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}/approve`),dialog.getByRole('button',{name:'审批通过并分派',exact:true}).click()]);
 assert.equal(approved.status(),200,await approved.text());
 const receipt=(await approved.json()).data,request=approved.request();
 const replay=await post(leader,`${path}/approve`,request.postDataJSON(),request.headers()['idempotency-key']);assert.equal(replay.status(),200);assert.deepEqual((await replay.json()).data,receipt);
 // The shared middleware also replays the same semantic request with a new key.
 const duplicate=await post(leader,`${path}/approve`,data);assert.equal(duplicate.status(),200);assert.deepEqual((await duplicate.json()).data,receipt);
 const changedDecision=await post(leader,`${path}/approve`,{...data,reason:`${f.Run} another decision`});assert.equal(changedDecision.status(),409);
 const after=await get(leader,path);assert.equal(after.status,'PROCESSING');assert.equal(after.can_approve,false);assert.equal(after.rows.length,Number(process.env.BSS_NATIVE_EXPECTED_ROWS||1));assert.match(after.rows[0].resource_id,/^[1-9]\d*$/);
 assert.equal((await task(leader,'SUBMISSION_APPROVAL')).length,0);assert.equal((await task(leader,'SUBMISSION_APPROVAL','DONE')).length,1);
 const tasks=await task(handler,'SUBMISSION_HANDLING');assert.equal(tasks.length,1);assert.match(tasks[0].deeplink,/\/internal\/workbench\?node_id=/);
 await handler.page.goto(`${base}/#/internal/workbench`);
 await handler.page.getByRole('button',{name:'刷新',exact:true}).click();
 await handler.page.locator('tr').filter({hasText:f.Run}).filter({hasText:'测试与资源配置办理'}).getByRole('button',{name:'详情',exact:true}).click();
 const handling=handler.page.getByRole('dialog',{name:'测试与资源配置办理',exact:true});
 assert.equal(await handling.getByRole('button',{name:'审批通过并分派',exact:true}).count(),0);
 await handling.getByRole('button',{name:'办理资源',exact:true}).first().click();
 await handler.page.waitForURL(url=>url.hash.includes(`/internal/model-plaza/${after.rows[0].resource_id}`)&&url.hash.includes('tab=operations'));
 await handler.page.getByRole('button',{name:'同步测试结果',exact:true}).waitFor();
 await handler.page.screenshot({path:`${out}/assigned-handling.png`,fullPage:true});
 assert.deepEqual(errors,[]);
 await writeFile(`${out}/checks.json`,JSON.stringify({passed:true,realAuthentication:true,realAPI:true,approvalInWorkbench:true,assignedHandlingNavigation:true,unauthorized403:true,stale409:true,idempotentReplay200:true,semanticReplay200:true,changedDecision409:true,noBrowserMocks:true,inspectorCalled:false,pageErrors:errors},null,2));
 console.log('leader workbench approval -> assignment -> A resource handling passed; 403/409/replay verified');
 }
}catch(error){for(let i=0;i<sessions.length;i++){await sessions[i].page.screenshot({path:`${out}/failure-${i}.png`,fullPage:true}).catch(()=>{});await writeFile(`${out}/failure-${i}.txt`,await sessions[i].page.locator('body').innerText()).catch(()=>{});}throw error;}
finally{for(const s of sessions){await s.page.request.post(`${base}/api/internal/auth/logout`,{headers:s.auth}).catch(()=>{});await s.context.close();}await browser.close();}
