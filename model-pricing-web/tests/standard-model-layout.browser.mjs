import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {createServer} from 'vite';
import {chromium} from 'playwright';

// UI-only fixture: actual Vue page and controls, no Docker/database changes.
process.env.VITE_AUTH_ENABLED='false';process.env.VITE_MOCK_ENABLED='false';process.env.VITE_API_BASE_URL='/api';
const out='.qa/standard-model-layout-20261027';await mkdir(out,{recursive:true});
const entry=`import {createApp,h} from 'vue';import {createPinia} from 'pinia';import {createRouter,createWebHistory,RouterView} from 'vue-router';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import '/src/styles/index.css';import Page from '/src/views/internal/models/StandardModelList.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const router=createRouter({history:createWebHistory(),routes:[{path:'/__model-layout',component:Page}]});const app=createApp({render:()=>h(RouterView)});app.use(pinia).use(router).use(ElementPlus,{locale:zhCn});useSessionStore(pinia).restoreSession();router.isReady().then(()=>app.mount('#app'));`;
const plugin={name:'model-layout-test',resolveId(id){if(id==='/@model-layout')return '\0model-layout';},load(id){if(id==='\0model-layout')return entry;},configureServer(server){server.middlewares.use(async(req,res,next)=>{if(req.url!=='/__model-layout')return next();res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,'<html lang="zh-CN"><body><div id="app"></div><script type="module" src="/@model-layout"></script></body></html>'));});}};
const price={item_key:'input',label:'输入',task_type:'TEXT_INPUT',amount:'1.00000000',currency:'USD',unit_code:'TOKEN',unit_quantity:'1000000',condition:'标准计费',dimensions:{},source_url:'https://vendor.example/pricing'};
const model={id:'101',revision:'1',version_no:'1',current_version_no:'1',maintainer_id:'7',company:'DeepSeek',name:'DeepSeek-V3',official_id:'deepseek-v3',model_type:'TEXT',input_modalities:['TEXT'],output_modalities:['TEXT'],capacity:[{key:'context_window',disclosure:'DISCLOSED',value:'128000 tokens'}],abilities:[{key:'tool_call',state:'UNDISCLOSED',description:''}],api_access:[{key:'protocol',disclosure:'DISCLOSED',value:'HTTPS'}],lifecycle:[{key:'version',disclosure:'DISCLOSED',value:'DeepSeek-V3 原始权重'},{key:'release_date',disclosure:'UNVERIFIED',value:''}],price_items:[price,{...price,item_key:'output',label:'输出',task_type:'TEXT_OUTPUT'}],sources:[{label:'官方价格',url:'https://vendor.example/pricing',source_kind:'OFFICIAL_PRICING'},{label:'官方模型卡',url:'https://vendor.example/model',source_kind:'OFFICIAL_DOCS'}],checked_at:'2026-10-14',missing_fields:[],verify_status:'VERIFIED',quote_readiness:{ready:true,reasons:[],standard_model_version_no:'1'},permissions:{can_edit:true,can_verify:true},status:'ACTIVE',created_at:'2026-10-20T01:00:00Z',updated_at:'2026-10-20T01:00:00Z'};
let server,browser;const errors=[],writes=[];
try{
 server=await createServer({plugins:[plugin],server:{host:'127.0.0.1',port:0},logLevel:'error'});await server.listen();
 browser=await chromium.launch({channel:'msedge',headless:true});const page=await browser.newPage({viewport:{width:1208,height:760}});page.setDefaultTimeout(10000);page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/internal/**',route=>{const r=route.request(),path=new URL(r.url()).pathname;const send=data=>route.fulfill({contentType:'application/json',body:JSON.stringify({code:0,message:'ok',data})});if(r.method()==='PUT'){writes.push(r.postDataJSON());return send({...model,revision:'2',version_no:'2'});}if(path.endsWith('/filter-options'))return send({companies:['DeepSeek'],suppliers:[],has_more:false});if(path.endsWith('/versions'))return send({list:[model],total:1,page:1,size:20});if(path.endsWith('/operations'))return send({list:[],total:0,page:1,size:100});if(path.endsWith('/101'))return send(model);return send({list:[model],total:1,page:1,size:100});});
 await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__model-layout`);await page.getByRole('button',{name:'维护',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'手动补全',exact:true});await dialog.getByLabel('官方标准名称',{exact:true}).waitFor();
 // RED: the old implementation adds unrelated fields and history below the date.
 assert.equal(await dialog.getByLabel('操作说明',{exact:true}).count(),0,'手动补全主表单不应增加操作说明');
 assert.equal(await dialog.getByText('补充官方来源',{exact:false}).count(),0);assert.equal(await dialog.getByText('补充计费信息',{exact:true}).count(),0);
 assert.equal(await dialog.getByRole('button',{name:'历史版本',exact:true}).count(),0);
 assert.equal(await dialog.locator('input[aria-label="核验日期"]').getAttribute('type'),'date');
 const add=dialog.getByRole('button',{name:'＋ 增加计费项',exact:true});await add.scrollIntoViewIfNeeded();
 const bounds=await add.boundingBox(),editor=await dialog.locator('.price-editor').boundingBox();assert.ok(Math.abs(bounds.width-editor.width)<3,'增加计费项按钮应占满表单宽度');
 await dialog.locator('input[aria-label="核验日期"]').scrollIntoViewIfNeeded();await page.screenshot({path:`${out}/manual-completion.png`,fullPage:true});
 assert.equal(await dialog.getByRole('button',{name:'保存并留痕',exact:true}).isVisible(),true);
 await dialog.getByRole('button',{name:'保存并留痕',exact:true}).click();await dialog.waitFor({state:'hidden'});assert.equal(writes.length,1);assert.ok(writes[0].reason.trim());assert.deepEqual(writes[0].sources,model.sources);assert.equal(writes[0].checked_at,'2026-10-14');
 // Traceability is still available from the list, outside the manual form.
 await page.getByRole('button',{name:'更多模型操作',exact:true}).click();await page.getByRole('menuitem',{name:'历史版本',exact:true}).click();
 const trace=page.getByRole('dialog',{name:'模型资料追溯',exact:true});await trace.getByRole('cell',{name:'1',exact:true}).first().waitFor();await trace.getByRole('button',{name:'返回维护',exact:true}).click();
 await dialog.getByRole('button',{name:'取消',exact:true}).click();
 await page.getByRole('button',{name:'更多模型操作',exact:true}).click();await page.getByRole('menuitem',{name:'补充资料',exact:true}).click();
 const extra=page.getByRole('dialog',{name:'补充模型资料',exact:true});await extra.getByRole('textbox',{name:'补充来源1名称',exact:true}).fill('补充官方模型卡');await extra.getByLabel('操作说明',{exact:true}).fill('核对官方模型卡补充说明');
 await extra.getByRole('button',{name:'返回手动补全',exact:true}).click();await dialog.getByRole('button',{name:'保存并留痕',exact:true}).click();await dialog.waitFor({state:'hidden'});
 assert.equal(writes.length,2);assert.equal(writes[1].reason,'核对官方模型卡补充说明');assert.equal(writes[1].sources[1].label,'补充官方模型卡');assert.deepEqual(writes[1].price_items,model.price_items);
 await page.getByRole('button',{name:'维护',exact:true}).click();await page.setViewportSize({width:620,height:760});await dialog.locator('input[aria-label="核验日期"]').scrollIntoViewIfNeeded();assert.ok((await dialog.boundingBox()).width<=620);await page.screenshot({path:`${out}/manual-completion-narrow.png`,fullPage:true});
 assert.deepEqual(errors,[]);await writeFile(`${out}/checks.json`,JSON.stringify({passed:true,uiFixture:true,noDatabaseWrites:true,saveRetainsSources:true,historyAccessible:true,pageErrors:errors},null,2));console.log('manual-completion layout, single save payload, history navigation and narrow viewport passed (UI fixture)');
}finally{await browser?.close();await server?.close();}
