import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {createServer} from 'vite';
import {chromium} from 'playwright';
process.env.VITE_AUTH_ENABLED='false';process.env.VITE_MOCK_ENABLED='false';process.env.VITE_API_BASE_URL='/api';
const entry=`import {createApp} from 'vue';import {createPinia} from 'pinia';import ElementPlus from 'element-plus';import 'element-plus/dist/index.css';import List from '/src/views/internal/models/StandardModelList.vue';import {useSessionStore} from '/src/stores/session';const pinia=createPinia(),app=createApp(List);app.use(pinia);const session=useSessionStore();session.user={id:'5',name:'测试员工',roleLabel:'模型运营'};session.permissionCodes=['M1:E'];sessionStorage.setItem('bss-catalog-crawl:5',JSON.stringify({requestId:'old-request',vendors:['deepseek'],sessionId:'old-task'}));app.use(ElementPlus).mount('#app');`;
const plugin={name:'catalog-entry-fixture',resolveId(id){if(id==='/@entry-fixture')return '\0entry-fixture'},load(id){if(id==='\0entry-fixture')return entry},configureServer(server){server.middlewares.use(async(req,res,next)=>{if(!req.url.startsWith('/__entry-fixture'))return next();res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,'<html><body><div id="app"></div><script type="module" src="/@entry-fixture"></script></body></html>'))})}};
let server,browser;
try{
 server=await createServer({plugins:[plugin],server:{host:'127.0.0.1',port:0},logLevel:'error'});await server.listen();
 browser=await chromium.launch({channel:'msedge',headless:true});const page=await browser.newPage({viewport:{width:1800,height:1000}});const errors=[];page.on('pageerror',e=>errors.push(e.message));let starts=0;
 await page.route('**/api/internal/**',async route=>{
  const url=new URL(route.request().url());let data;
  if(url.pathname.endsWith('/tasks')){
   if(route.request().method()==='POST'){starts++;data={sessionId:'new-task'}}
   else data={task:{status:'SUCCEEDED',result:{runs:[{vendor:'deepseek',crawl:{runId:'run',status:'SUCCEEDED',pendingModelIds:['flash'],run:{startedAt:'2026-11-24T06:34:00Z',finishedAt:'2026-11-24T06:35:00Z'}}}]}}};
  }else if(url.pathname.endsWith('/review'))data={rows:[]};
  else if(url.pathname.endsWith('/filter-options'))data={companies:['DeepSeek'],suppliers:[],has_more:false};
  else data={page:1,size:100,total:0,list:[]};
  await route.fulfill({json:{code:0,data}});
 });
 await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__entry-fixture`);
 await page.getByRole('button',{name:'查看变更与审核',exact:true}).waitFor();
 assert.equal(await page.getByRole('dialog').count(),0,'Restoring an old crawl must not open review');assert.equal(starts,0);
 const names=await page.locator('.crawl-actions button').allTextContents();assert.deepEqual(names.map(x=>x.trim()),['定时抓取','模型抓取','查看变更与审核']);
 const status=await page.locator('.crawl-status>span').last().boundingBox(),buttons=await page.locator('.crawl-actions').boundingBox();assert.ok(buttons.x-status.x-status.width<25,'Actions should follow the status text');
 await mkdir('E:/Desktop/Dongxin/.tmp/review-ui-20261124',{recursive:true});await page.screenshot({path:'E:/Desktop/Dongxin/.tmp/review-ui-20261124/bss-entry.png',fullPage:true});
 await page.getByRole('button',{name:'模型抓取',exact:true}).click();
 await page.getByRole('button',{name:/确认.*抓取|开始抓取/}).click();
 await page.getByRole('dialog',{name:'官方模型采集 · 变更审核',exact:true}).waitFor();assert.equal(starts,1,'Only the explicitly started crawl may open review');
 assert.deepEqual(errors,[]);console.log('PASS restored task stays closed, ordered adjacent actions, fresh explicit crawl opens review (API fixtures)');
}finally{await browser?.close();await server?.close()}
