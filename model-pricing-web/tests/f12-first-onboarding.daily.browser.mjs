import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {chromium} from 'playwright';

// Explicit daily environment only. Native approval, records and attachment APIs;
// no route interception, fabricated assignee, or Inspector request.
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,'model_bss');
assert.ok(process.env.BSS_NATIVE_MANIFEST&&process.env.BSS_TEST_PASSWORD);
const f=JSON.parse(await readFile(process.env.BSS_NATIVE_MANIFEST,'utf8'));
assert.match(f.Run,/^QA-NATIVE-/);
const phase=process.env.BSS_NATIVE_PHASE||'documents',base='http://127.0.0.1:5173';
const out='.qa/f12-first-v43-20261027';await mkdir(out,{recursive:true});
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6s0sAAAAASUVORK5CYII=','base64');
const hash=b=>createHash('sha256').update(b).digest('hex');
const browser=await chromium.launch({channel:'msedge',headless:true}),sessions=[],errors=[];
async function login(account){
 const context=await browser.newContext({viewport:{width:1500,height:1150},acceptDownloads:true}),page=await context.newPage();
 page.on('pageerror',e=>errors.push(e.message));
 await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fworkbench`);
 await page.locator('#login-account').fill(account);await page.locator('#login-password').fill(process.env.BSS_TEST_PASSWORD);
 const pending=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/auth/login');
 await page.getByRole('button',{name:'登录',exact:true}).click();
 const r=await pending;assert.equal(r.status(),200);const token=(await r.json()).data.token;
 const s={page,context,auth:{Authorization:`Bearer ${token}`}};sessions.push(s);return s;
}
async function get(s,path){const r=await s.page.request.get(`${base}/api/internal${path}`,{headers:s.auth});assert.equal(r.status(),200,await r.text());return(await r.json()).data;}
async function post(s,path,data,key=crypto.randomUUID()){return s.page.request.post(`${base}/api/internal${path}`,{headers:{...s.auth,'Idempotency-Key':key},data});}
async function open(s,node){
 await s.page.goto(`${base}/#/internal/workbench?node_id=${node}`);
 const dialog=s.page.getByRole('dialog',{name:'合同与授信办理',exact:true});
 await dialog.locator('.f12-subject-panel').waitFor();
 await dialog.getByRole('button',{name:'新增合同',exact:true}).waitFor();
 return dialog;
}
async function saveContract(s,revise=false){
 const panel=s.page.locator('.f12-subject-panel');
 if(revise)await panel.getByRole('button',{name:'修订',exact:true}).click();
 else await panel.getByRole('button',{name:'新增合同',exact:true}).click();
 const dialog=s.page.getByRole('dialog',{name:revise?'修订合同资料':'新增合同资料',exact:true});
 await dialog.getByRole('textbox',{name:'合同名称',exact:true}).fill(`${f.Run} contract${revise?' v2':''}`);
 await dialog.getByRole('textbox',{name:'合同编号',exact:true}).fill(`${f.Run}-C`);
 await dialog.getByRole('textbox',{name:'变更原因',exact:true}).fill(`${f.Run} ${revise?'revise':'create'} contract`);
 if(!revise)await dialog.locator('input[type=file]').setInputFiles({name:'contract.png',mimeType:'image/png',buffer:png});
 const [r]=await Promise.all([s.page.waitForResponse(r=>new URL(r.url()).pathname.startsWith('/api/internal/contracts')&&r.request().method()==='POST'),dialog.getByRole('button',{name:'保存为版本',exact:true}).click()]);
 assert.equal(r.status(),200,await r.text());await dialog.waitFor({state:'hidden'});
 return(await r.json()).data;
}
async function saveCredit(s,amount){
 await s.page.locator('.f12-subject-panel').getByRole('button',{name:'新增 / 调整授信',exact:true}).click();
 const dialog=s.page.getByRole('dialog',{name:'新增 / 调整授信',exact:true});
 for(const [name,value] of [['额度',amount],['币种','CNY'],['依据名称',`${f.Run} credit ${amount}`],['依据编号',`${f.Run}-L`],['变更原因',`${f.Run} credit ${amount}`]])await dialog.getByRole('textbox',{name,exact:true}).fill(value);
 await dialog.locator('input[type=file]').setInputFiles({name:`credit-${amount}.png`,mimeType:'image/png',buffer:png});
 const[r]=await Promise.all([s.page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/credits'&&r.request().method()==='POST'),dialog.getByRole('button',{name:'保存为新版本',exact:true}).click()]);
 assert.equal(r.status(),200,await r.text());await dialog.waitFor({state:'hidden'});return(await r.json()).data;
}
async function records(s){return {contracts:await get(s,`/contracts?subject_type=SUPPLIER&subject_id=${f.Supplier}&size=20&page=1`),credits:await get(s,`/credits?subject_type=SUPPLIER&subject_id=${f.Supplier}&size=20&page=1`)};}
async function readiness(s,v){return get(s,`/f12/readiness?subject_type=SUPPLIER&subject_id=${f.Supplier}&flow_id=${v.flow_id}&module_key=CONTRACT`);}
async function verifyFiles(s,files){for(const file of files){const r=await s.page.request.get(`${base}/api/internal/f12/attachments/${encodeURIComponent(file.id)}?subject_type=SUPPLIER&subject_id=${f.Supplier}&purpose=${file.purpose}`,{headers:s.auth});assert.equal(r.status(),200,await r.text());assert.equal(hash(await r.body()),hash(png));}}
async function tasks(s,state){return(await get(s,`/workbench/inbox?state=${state}&size=100&keyword=${encodeURIComponent(f.Run)}`)).list.filter(x=>x.biz_type==='SUBMISSION_CONTRACT');}
try{
 const actor=await login('buyer_a'),buyer=await login('smoke_admin');
 let v=await get(actor,`/flows/submissions/${f.Submissions[0]}`);const node=v.nodes.find(n=>n.key==='CONTRACT');assert.equal(node.assignee_id,'1');
 const path=`/flows/nodes/${node.id}/complete-contract`,completion={expected_flow_revision:v.flow_revision,reason:`${f.Run} commercial completed`};
 if(phase==='documents'){
  assert.equal(node.status,'WAITING_PREDECESSOR');assert.equal(v.can_handle_contract,true);assert.equal(v.can_complete_contract,false);
  const dialog=await open(actor,node.id);
  const existing=await records(actor);
  await dialog.getByText(existing.contracts.list.length?/办理资料已齐备/:/办理资料待补齐/).waitFor();
  assert.equal(await dialog.getByRole('button',{name:'登记合同授信完成',exact:true}).isDisabled(),true);
  if(!existing.contracts.list.length){assert.equal((await readiness(actor,v)).ready,false);await saveContract(actor);}
  if(!existing.credits.list.length)await saveCredit(actor,'100.00');
  await dialog.getByText(/办理资料已齐备/).waitFor();assert.equal((await readiness(actor,v)).ready,true);
  assert.equal(await dialog.getByRole('button',{name:'登记合同授信完成',exact:true}).isDisabled(),true);
  const premature=await post(actor,path,completion);assert.equal(premature.status(),409,await premature.text());
  const wrongActor=await post(buyer,path,completion);assert.equal(wrongActor.status(),403,await wrongActor.text());
  const rows=await records(actor);assert.equal(rows.contracts.list.length,1);assert.equal(rows.credits.list.length,1);
  const c=rows.contracts.list[0],l=rows.credits.list[0];
  const attempted=await post(buyer,'/contracts',{subject_type:'SUPPLIER',subject_id:f.Supplier,document_name:'forbidden',document_no:'forbidden',file_id:c.file_id,reason:f.Run,source_flow_id:v.flow_id,source_module_key:'CONTRACT',expected_flow_revision:v.flow_revision});
  assert.equal(attempted.status(),403,await attempted.text());
  const files=[{id:c.file_id,purpose:'CONTRACT'},{id:l.file_id,purpose:'CREDIT'}];await verifyFiles(actor,files);
  const downloaded=actor.page.waitForEvent('download');await dialog.locator('.f12-subject-panel').getByRole('button',{name:/下载/}).first().click();
  const download=await downloaded;assert.equal(await download.failure(),null);
  await writeFile(`${out}/fixture.json`,JSON.stringify({run:f.Run,files,contractKey:c.contract_key,flow:v.flow_id,node:node.id,pngHash:hash(png)},null,2));
  await actor.page.screenshot({path:`${out}/documents-ready-waiting-config.png`,fullPage:true});
  console.log('v43 F12 real uploads and records saved; readiness true, predecessor/assignee gates retained');
 }else if(phase==='complete'){
  const saved=JSON.parse(await readFile(`${out}/fixture.json`,'utf8'));assert.equal(saved.run,f.Run);
  assert.equal(node.status,'IN_PROGRESS');assert.equal((await tasks(actor,'TODO')).length,1);
  const dialog=await open(actor,node.id);await dialog.getByText(/办理资料已齐备/).waitFor();
  const before=await records(actor);
  if(before.contracts.list.length===1)await saveContract(actor,true);
  if(before.credits.list.length===1)await saveCredit(actor,'80.00');
  await dialog.getByText(/办理资料已齐备/).waitFor();
  const rows=await records(actor);assert.equal(rows.contracts.list.length,2);assert.equal(rows.credits.list.length,2);
  assert.deepEqual(rows.contracts.list.map(c=>c.version).sort(),[1,2]);
  const latest=rows.credits.list.find(c=>c.version===2);assert.equal(Number(latest.amount),80);
  assert.equal(rows.contracts.list.find(c=>c.version===2).file_id,saved.files[0].id);
  const ready=await readiness(actor,v);assert.equal(ready.ready,true);assert.equal(ready.contracts[0].version,'2');assert.equal(ready.credits[0].version,'2');
  const stale=await post(actor,path,{...completion,expected_flow_revision:(BigInt(v.flow_revision)+1n).toString()});assert.equal(stale.status(),409,await stale.text());
  const denied=await post(buyer,path,completion);assert.equal(denied.status(),403,await denied.text());
  await dialog.getByRole('textbox',{name:'合同授信完成说明',exact:true}).fill(completion.reason);
  const[r]=await Promise.all([actor.page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal${path}`&&r.request().method()==='POST'),dialog.getByRole('button',{name:'登记合同授信完成',exact:true}).click()]);
  assert.equal(r.status(),200,await r.text());const receipt=(await r.json()).data,request=r.request();
  const replay=await post(actor,path,request.postDataJSON(),request.headers()['idempotency-key']);assert.equal(replay.status(),200);assert.deepEqual((await replay.json()).data,receipt);
  const changed=await post(actor,path,{...completion,reason:`${f.Run} changed completion`});assert.equal(changed.status(),409,await changed.text());
  v=await get(actor,`/flows/submissions/${f.Submissions[0]}`);assert.equal(v.status,'COMPLETED');assert.ok(v.nodes.every(n=>n.status==='COMPLETED'));
  assert.equal((await tasks(actor,'TODO')).length,0);assert.equal((await tasks(actor,'DONE')).length,1);
  saved.files.push({id:latest.file_id,purpose:'CREDIT'});await verifyFiles(actor,saved.files);await writeFile(`${out}/fixture.json`,JSON.stringify(saved,null,2));
  await actor.page.screenshot({path:`${out}/commercial-completed.png`,fullPage:true});
  console.log('v43 first supplier CONTRACT+credit V1/V2 -> true readiness -> completion -> DONE; stale/actor/replay checked');
 }else{
  assert.equal(phase,'persist');const saved=JSON.parse(await readFile(`${out}/fixture.json`,'utf8'));assert.equal(saved.run,f.Run);
  await verifyFiles(actor,saved.files);await verifyFiles(buyer,saved.files);assert.equal(v.status,'COMPLETED');assert.equal((await tasks(actor,'DONE')).length,1);
  await actor.page.goto(`${base}/#/internal/workbench?node_id=${node.id}`);
  const dialog=actor.page.getByRole('dialog',{name:'合同与授信办理',exact:true});await dialog.getByText(/当前为只读查看/).first().waitFor();
  assert.equal(await dialog.getByRole('button',{name:'登记合同授信完成',exact:true}).count(),0);
  await actor.page.screenshot({path:`${out}/after-api-restart.png`,fullPage:true});
  console.log('same daily API restart: all real attachments byte-identical, completed task remains read-only');
 }
 assert.deepEqual(errors,[]);await writeFile(`${out}/${phase}-checks.json`,JSON.stringify({passed:true,phase,databaseVersion:43,realBrowser:true,realAPI:true,realRecords:true,noRouteMocks:true,inspectorCalled:false,pageErrors:errors},null,2));
}catch(error){for(let i=0;i<sessions.length;i++){await sessions[i].page.screenshot({path:`${out}/${phase}-failure-${i}.png`,fullPage:true}).catch(()=>{});await writeFile(`${out}/${phase}-failure-${i}.txt`,await sessions[i].page.locator('body').innerText()).catch(()=>{});}throw error;}
finally{for(const s of sessions){await s.page.request.post(`${base}/api/internal/auth/logout`,{headers:s.auth}).catch(()=>{});await s.context.close();}await browser.close();}
