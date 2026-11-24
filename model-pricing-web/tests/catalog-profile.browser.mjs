import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {createServer} from 'vite';
import {chromium} from 'playwright';
process.env.VITE_AUTH_ENABLED='false';process.env.VITE_MOCK_ENABLED='false';process.env.VITE_API_BASE_URL='/api';
const entry=`import {createApp,h} from 'vue';import {createPinia} from 'pinia';import ElementPlus from 'element-plus';import 'element-plus/dist/index.css';import List from '/src/views/internal/models/StandardModelList.vue';import Review from '/src/views/internal/models/CatalogCrawlReview.vue';import {useSessionStore} from '/src/stores/session';const pinia=createPinia(),app=createApp({render:()=>location.search.includes('review')?h(Review,{sources:[{vendor:'deepseek',runId:'fixture-run'}]}):h(List)});app.use(pinia);const session=useSessionStore();session.user={id:'5',name:'测试员工',roleLabel:'模型运营'};session.permissionCodes=['M1:V','M1:E'];app.use(ElementPlus).mount('#app');`;
const plugin={name:'profile-fixture',resolveId(id){if(id==='/@profile')return '\0profile'},load(id){if(id==='\0profile')return entry},configureServer(server){server.middlewares.use(async(req,res,next)=>{if(!req.url.startsWith('/__profile'))return next();res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,'<html><body><div id="app"></div><script type="module" src="/@profile"></script></body></html>'))})}};
const capacity={key:'max_output_scope',label:'最大输出',value:'8192',disclosure:'DISCLOSED',unit:'TOKEN',semantics:'SHARED_WINDOW',condition:'标准模式',source_url:'https://example.com/limits',quote:'Max output: 8192'};
const price={item_key:'input_cn',label:'输入',task_type:'TEXT_INPUT',amount:'1',currency:'USD',unit_code:'TOKEN',unit_quantity:'1000000',condition:'标准 API 计费',dimensions:{region:'中国内地',context_tier:'0-128K',valid_from:'2026-01-01'},source_url:'https://example.com/pricing'};
const model={id:'1',maintainer_id:'3',revision:'1',version_no:'1',company:'DeepSeek',name:'展示名称',official_id:'fixture-model',model_type:'TEXT',input_modalities:['TEXT'],output_modalities:['TEXT'],capacity:[capacity],price_items:[price,{...price,item_key:'input_overseas',amount:'2',dimensions:{region:'海外',context_tier:'0-128K'}}],abilities:[{key:'vision',state:'UNVERIFIED',description:'待核实'}],api_access:[],lifecycle:[],sources:[],checked_at:'2026-11-01',status:'ACTIVE',verify_status:'INCOMPLETE',permissions:{can_edit:true,can_verify:false},missing_fields:[],quote_readiness:{ready:false,reasons:[],standard_model_version_no:'1'}};
let server,browser;
try{
 server=await createServer({plugins:[plugin],server:{host:'127.0.0.1',port:0},logLevel:'error'});await server.listen();
 browser=await chromium.launch({channel:'msedge',headless:true});const page=await browser.newPage({viewport:{width:1600,height:1000}});page.setDefaultTimeout(12000);const errors=[],writes=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/internal/**',async route=>{const url=new URL(route.request().url());let data;
  if(url.pathname.endsWith('/model-status'))data={'1':{state:'PARTIAL',attemptedAt:'2026-11-24T01:00:00Z',lastCompleteAt:'2026-11-01T01:00:00Z'}};
  else if(url.pathname.endsWith('/filter-options'))data={companies:['DeepSeek'],suppliers:[],has_more:false};
  else if(url.pathname.endsWith('/review')){
   if(route.request().method()==='POST'){writes.push(route.request().postDataJSON());data={bssSync:[{model_id:'fixture-model',status:'WRITTEN'}]};}
   else if(url.searchParams.get('view')==='index')data={rows:[{id:'fixture-model',name:'展示名称',pending:true,change:'CHANGED'}]};
   else data={pendingModelIds:['fixture-model'],canReview:true,latestAttempt:{id:'fixture-run',result:{models:[{id:'fixture-model',name:'展示名称',bss:model}]}},liveBss:{id:'1',revision:'1',current:{name:'展示名称',model_type:'TEXT',input_modalities:['TEXT'],output_modalities:['TEXT'],'capacity.max_output_scope':{...capacity,value:'4096'}},official:{name:'展示名称','capacity.max_output_scope':capacity},fields:[{field:'capacity.max_output_scope',old:{...capacity,value:'4096'},new:capacity,kind:'CHANGED'}]}};
  }else data={list:[model],total:1,page:1,size:100};
  await route.fulfill({json:{code:0,data}});
 });
 const base=`http://127.0.0.1:${server.httpServer.address().port}/__profile`;
 await page.goto(base);await page.getByText('部分成功',{exact:true}).waitFor();
 const text=await page.locator('body').innerText();assert.match(text,/8,192 Token.*共享窗口.*标准模式/);assert.match(text,/2026\/11\/01/);assert.match(text,/中国内地/);assert.match(text,/海外/);assert.doesNotMatch(text,/max_output_scope|SHARED_WINDOW/);
 await page.goto(base+'?review');await page.getByRole('cell',{name:'最大输出',exact:true}).waitFor();
 assert.match(await page.locator('body').innerText(),/8,192 Token.*标准模式/);
 await page.getByRole('checkbox').last().check();await page.getByRole('button',{name:/批准当前模型/}).click();
 await page.waitForFunction(()=>document.body.innerText.includes('写入')||document.body.innerText.includes('批准'));await page.waitForTimeout(150);
 assert.equal(writes.length,1);assert.equal(writes[0].modelId,'fixture-model');assert.equal(writes[0].overrides.capacity[0].condition,'标准模式');assert.equal(writes[0].overrides.capacity[0].source_url,capacity.source_url);
 assert.deepEqual(errors,[]);await mkdir('E:/Desktop/Dongxin/.tmp/catalog-profile-fields-20261124',{recursive:true});await page.screenshot({path:'E:/Desktop/Dongxin/.tmp/catalog-profile-fields-20261124/bss-review.png',fullPage:true});console.log('PASS scoped specification, regional prices, per-model partial status, retained full-success timestamp, selected-field evidence payload (mock API)');
}finally{await browser?.close();await server?.close()}
