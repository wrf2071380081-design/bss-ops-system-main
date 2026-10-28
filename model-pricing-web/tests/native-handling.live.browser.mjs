import assert from 'node:assert/strict';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {chromium} from 'playwright';

// Live daily42 only. No browser route mocks and no Inspector connection.
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,'model_bss');
assert.ok(process.env.BSS_NATIVE_MANIFEST&&process.env.BSS_TEST_PASSWORD);
const f=JSON.parse(await readFile(process.env.BSS_NATIVE_MANIFEST,'utf8'));
assert.ok(f.Run.startsWith('QA-NATIVE-'));
const phase=process.env.BSS_NATIVE_PHASE||'admit',base='http://127.0.0.1:5173',evidence='.qa/native-handling-20261027';
await mkdir(evidence,{recursive:true});
const browser=await chromium.launch({channel:'msedge',headless:true});
const errors=[],sessions=[];
async function login(account){
 const context=await browser.newContext({viewport:{width:1500,height:1050}}),page=await context.newPage();
 page.on('pageerror',e=>errors.push(e.message));
 await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodel-plaza`);
 await page.locator('#login-account').fill(account);await page.locator('#login-password').fill(process.env.BSS_TEST_PASSWORD);
 const response=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/auth/login');
 await page.getByRole('button',{name:'登录',exact:true}).click();
 const result=await response;assert.equal(result.status(),200);const token=(await result.json()).data.token;
 const session={page,context,auth:{Authorization:`Bearer ${token}`}};sessions.push(session);return session;
}
async function get(session,path){const r=await session.page.request.get(`${base}/api/internal${path}`,{headers:session.auth});assert.equal(r.status(),200,`${path}: ${await r.text()}`);return (await r.json()).data;}
async function write(session,method,path,data,key=crypto.randomUUID()){
 const r=await session.page.request.fetch(`${base}/api/internal${path}`,{method,headers:{...session.auth,'Idempotency-Key':key},data});return r;
}
async function openOperations(session){
 await session.page.goto(`${base}/#/internal/model-detail/${f.Model}`);
 const row=session.page.locator('tr').filter({hasText:f.Run});
 await row.getByRole('button',{name:phase==='admit'?/^(登记接入|补测资源)$/:/^上下架计划$/}).click();
 if(phase!=='admit')await session.page.getByRole('tab',{name:'配置与改价',exact:true}).click();
}
try{
 const actor=await login('buyer_a'),buyer=await login('smoke_admin');
 const path=`/resources/${f.Resource}`;
 let resource=await get(actor,path);
 const denied=await get(buyer,`${path}/workflow-actions`);
 assert.equal(denied.can_bind,false);assert.equal(denied.can_admit,false);assert.deepEqual(denied.approved_prices,[]);
 if(phase==='admit'){
  assert.ok(['PENDING_ACCESS','PENDING_TEST'].includes(resource.state));assert.equal(resource.buyer_id,'3');assert.equal(resource.quote_readiness.eligible,false);
  const forbidden=await write(buyer,'PUT',`${path}/formal-config`,{expected_revision:resource.revision,formal_config_id:f.Config,expected_config_revision:'1',reason:f.Run});assert.equal(forbidden.status(),403);
  await openOperations(actor);
  await actor.page.getByRole('textbox',{name:'同步测试原因',exact:true}).fill(f.Run);
  const [synced]=await Promise.all([actor.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}/refresh-test-evidence`),actor.page.getByRole('button',{name:'同步测试结果',exact:true}).click()]);assert.equal(synced.status(),200);
  await actor.page.getByRole('textbox',{name:'操作原因',exact:true}).fill(f.Run);
  const [bound]=await Promise.all([actor.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}/formal-config`&&r.request().method()==='PUT'),actor.page.getByRole('button',{name:'关联正式配置',exact:true}).click()]);assert.equal(bound.status(),200,await bound.text());
  // A01: use the actual procurement person's configuration table. Being the
  // handler does not confer permission to disable that person's configuration.
  resource=await get(actor,path);
  const deniedDisable=await write(actor,'DELETE',`/suppliers/${f.Supplier}/formal-configs/${f.Config}`,{expected_revision:'1',reason:f.Run});assert.equal(deniedDisable.status(),403);
  await openOperations(buyer);
  const configRow=buyer.page.locator('.resource-operations .el-table__row').filter({hasText:f.Run});
  await configRow.getByRole('button',{name:'停用',exact:true}).click();
  const prompt=buyer.page.getByRole('dialog',{name:'停用正式配置',exact:true});
  await prompt.getByRole('textbox').fill(f.Run);
  const [protectedConfig]=await Promise.all([buyer.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal/suppliers/${f.Supplier}/formal-configs/${f.Config}`&&r.request().method()==='DELETE'),prompt.getByRole('button',{name:'确认停用',exact:true}).click()]);assert.equal(protectedConfig.status(),409,await protectedConfig.text());
  await buyer.page.locator('.resource-operations .el-alert--error').waitFor();
  await buyer.page.getByRole('button',{name:'重新加载配置',exact:true}).click();
  await configRow.getByText('有效',{exact:true}).waitFor();
  const configs=await get(buyer,`/suppliers/${f.Supplier}/formal-configs`);const protectedRow=configs.find(c=>c.id===f.Config);assert.equal(protectedRow.status,'ACTIVE');assert.equal(protectedRow.revision,'1');assert.equal((await get(actor,path)).revision,resource.revision);
  await buyer.page.screenshot({path:`${evidence}/referenced-config-protected.png`,fullPage:true});
  await actor.page.getByText('已在国内 B 端完成该模型实际配置',{exact:true}).click();
  await actor.page.getByRole('textbox',{name:'完成依据',exact:true}).fill('QA native external configuration ticket');
  await actor.page.getByRole('textbox',{name:'登记原因',exact:true}).fill(f.Run);
  const [admitted]=await Promise.all([actor.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}/admit`),actor.page.locator('.resource-operations').getByRole('button',{name:'登记接入',exact:true}).click()]);assert.equal(admitted.status(),200,await admitted.text());
  resource=await get(actor,path);assert.equal(resource.state,'ADMITTED');assert.equal(resource.quote_readiness.eligible,false);assert.deepEqual(resource.quote_readiness.blocked_reasons,['MODEL_NOT_READY']);
  const current=await get(buyer,`/standard-models/${f.Model}`);
  const input={item_key:'text_input_standard',label:'输入',task_type:'TEXT_INPUT',amount:'2.00000000',currency:'USD',unit_code:'TOKEN',unit_quantity:'1000000',condition:'标准计费',source_url:'https://openai.com/pricing'};
  const output={...input,item_key:'text_output_standard',label:'输出',task_type:'TEXT_OUTPUT'};
  const body={company:'OpenAI',name:f.Run,official_id:f.Run,model_type:'TEXT',input_modalities:['TEXT'],output_modalities:['TEXT'],capacity:[{key:'context',disclosure:'DISCLOSED',value:'128000'}],abilities:[{key:'tool_call',state:'UNDISCLOSED',description:''}],api_access:[{key:'protocol',disclosure:'DISCLOSED',value:'HTTPS'}],lifecycle:[{key:'release',disclosure:'UNDISCLOSED',value:''}],sources:[{label:'official',url:input.source_url,source_kind:'OFFICIAL_PRICING'}],price_items:[input,output],checked_at:new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Shanghai'}).format(new Date()),expected_revision:current.revision,reason:f.Run};
  const verified=await write(buyer,'PUT',`/standard-models/${f.Model}/manual-completion`,body);assert.equal(verified.status(),200,await verified.text());
  resource=await get(actor,path);assert.equal(resource.quote_readiness.eligible,true);
 }else{
  assert.equal(resource.quote_readiness.eligible,true);
  const actions=await get(actor,`${path}/workflow-actions`);assert.equal(actions.approved_prices.length,1);const source=actions.approved_prices[0];assert.equal(source.source_row_id,f.PriceRow);
  const invalid=await write(buyer,'POST',`${path}/apply-approved-price`,{expected_revision:resource.revision,...source,external_confirmed:true,completion_note:'QA forbidden',reason:f.Run});assert.equal(invalid.status(),403);
  const stale=await write(actor,'POST',`${path}/apply-approved-price`,{expected_revision:resource.revision,...source,source_revision:(BigInt(source.source_revision)+1n).toString(),external_confirmed:true,completion_note:'QA stale',reason:f.Run});assert.equal(stale.status(),409);
  await openOperations(actor);
  await actor.page.getByRole('heading',{name:'供应商批准改价',exact:true}).waitFor();
  await actor.page.getByText('已在外部平台完成批准的采购系数调整',{exact:true}).click();
  await actor.page.getByRole('textbox',{name:'改价完成依据',exact:true}).fill('QA native price operation ticket');
  await actor.page.getByRole('textbox',{name:'改价登记原因',exact:true}).fill(f.Run);
  const [applied]=await Promise.all([actor.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}/apply-approved-price`),actor.page.getByRole('button',{name:'登记批准改价完成',exact:true}).click()]);assert.equal(applied.status(),200,await applied.text());
  const request=applied.request();const replay=await write(actor,'POST',`${path}/apply-approved-price`,request.postDataJSON(),request.headers()['idempotency-key']);assert.equal(replay.status(),200);
  assert.deepEqual((await replay.json()).data,(await applied.json()).data);
  const after=await get(actor,path);assert.equal(after.coefficient,'0.3300');assert.equal(after.revision,(BigInt(resource.revision)+1n).toString());assert.equal(after.quote_readiness.eligible,true);assert.equal(after.formal_config_id,resource.formal_config_id);assert.equal(after.test.report_id,resource.test.report_id);
  assert.deepEqual((await get(actor,`${path}/workflow-actions`)).approved_prices,[]);
 }
 assert.deepEqual(errors,[]);
 await actor.page.screenshot({path:`${evidence}/${phase}-success.png`,fullPage:true});
 await writeFile(`${evidence}/${phase}-checks.json`,JSON.stringify({phase,realAuthentication:true,realAPI:true,noBrowserMocks:true,inspectorCalled:false,passed:true,pageErrors:errors}));
 console.log(`native ${phase} real browser/API succeeded; no mocked workflow or Inspector call`);
}catch(error){
 if(sessions[0]){await sessions[0].page.screenshot({path:`${evidence}/${phase}-failure.png`,fullPage:true}).catch(()=>{});await writeFile(`${evidence}/${phase}-failure.txt`,await sessions[0].page.locator('body').innerText()).catch(()=>{});}
 throw error;
}finally{for(const s of sessions){await s.page.request.post(`${base}/api/internal/auth/logout`,{headers:s.auth}).catch(()=>{});await s.context.close();}await browser.close();}
