import assert from 'node:assert/strict';
import { createServer } from 'vite';
import { chromium } from 'playwright';

process.env.VITE_AUTH_ENABLED = 'false';
process.env.VITE_MOCK_ENABLED = 'false';
process.env.VITE_API_BASE_URL = '/api';
const entry = `import {createApp,h,ref} from 'vue';import {createPinia} from 'pinia';import ElementPlus from 'element-plus';import 'element-plus/dist/index.css';import Review from '/src/views/internal/models/CatalogCrawlReview.vue';import Schedule from '/src/views/internal/models/CatalogScheduleDialog.vue';import Records from '/src/views/internal/models/StandardModelOperationRecords.vue';createApp({setup(){const view=ref(location.search.includes('records')?'records':location.search.includes('schedule')?'schedule':'review');return()=>h('div',[h('button',{onClick:()=>view.value='schedule'},'计划设置'),view.value==='review'?h(Review,{sources:[{vendor:'deepseek',runId:'run-1'}]}):view.value==='records'?h(Records):h(Schedule)]);}}).use(createPinia()).use(ElementPlus).mount('#app');`;
const plugin = { name: 'catalog-review-fixture', resolveId(id) { if (id === '/@catalog-fixture') return '\0catalog-fixture'; },
  load(id) { if (id === '\0catalog-fixture') return entry; }, configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (!req.url.startsWith('/__catalog-fixture')) return next();
      res.setHeader('Content-Type', 'text/html');
      res.end(await server.transformIndexHtml(req.url, '<html><body><div id="app"></div><script type="module" src="/@catalog-fixture"></script></body></html>'));
    });
  } };
let server, browser;
try {
  server = await createServer({ plugins: [plugin], server: { host: '127.0.0.1', port: 0 }, logLevel: 'error' });
  await server.listen();
  browser = await chromium.launch({ channel: 'msedge', headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  const writes = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/internal/agent-catalog/**', async route => {
    const url = new URL(route.request().url());
    let data;
    if (route.request().method() === 'POST' && url.pathname.endsWith('/review')) {
      writes.push(route.request().postDataJSON());
      return route.fulfill({json:{code:0,data:{bssSync:[{model_id:'deepseek-flash',status:'WRITTEN'}]}}});
    }
    if (url.pathname.endsWith('/schedules')) data = { canManage: true, plans: [{ id: 'plan-1', recurrence: 'weekly:2', localTime: '10:30', active: true, validFrom: null, validUntil: null, history: [] }] };
    else if (url.searchParams.get('view') === 'index') data = { rows: [{ id: 'deepseek-flash', name: 'DeepSeek Flash', pending: true, change: 'CHANGED' }] };
    else data = { pendingModelIds: ['deepseek-flash'], canReview: true, liveBss: { status: 'FOUND', id: '1', revision: '2', official: {}, current: {}, fields: [] }, latestAttempt: { id: 'run-1', result: { models: [{ id: 'deepseek-flash', name: 'DeepSeek Flash', bss: {}, reviewDiff: { baseline: { kind: 'PERSONAL' }, previous_batch: { status: 'CHANGED', fields: [{ field: 'details', old: { ...(page.url().includes('hashOnly') ? {} : {concurrencyLimit: '10'}), metadataSha256: 'old-digest' }, new: { ...(page.url().includes('hashOnly') ? {} : {concurrencyLimit: '20'}), metadataSha256: 'new-digest' } }] } } }] } } };
    if (url.searchParams.get('view') === 'model' && page.url().includes('limitHistory')) {
      const before=[{field:'maxInput',value:'65536',unit:'TOKEN',semantics:'UNSPECIFIED',sha256:'old-limit-digest',evidenceId:'ev-old',rawLabel:'最大输入长度 65,536',rawValue:'65,536'}];
      const after=before.map(row=>({...row,sha256:'new-limit-digest',evidenceId:'ev-new',value:page.url().includes('limitChanged')?'32768':row.value}));
      data.latestAttempt.result.models[0].reviewDiff.previous_batch.fields=[{field:'limits',old:before,new:after}];
    }
    if (url.searchParams.get('view') === 'model' && page.url().includes('reviewFields')) {
      const current={name:'DeepSeek Flash',model_type:'TEXT',input_modalities:['TEXT'],output_modalities:['TEXT'],
        'capacity.context_length':{key:'context_length',value:'128000',disclosure:'DISCLOSED'}};
      data.liveBss={status:'UNCHANGED',id:'1',revision:'2',current,official:{...current},fields:[]};
      if (page.url().includes('partial')) data.liveBss={...data.liveBss,status:'CHANGED',
        official:{...current,name:'DeepSeek Flash New','capacity.context_length':{...current['capacity.context_length'],value:'256000'}},
        fields:[{field:'name',old:current.name,new:'DeepSeek Flash New',kind:'CHANGED'},
          {field:'capacity.context_length',old:current['capacity.context_length'],new:{...current['capacity.context_length'],value:'256000'},kind:'CHANGED'}]};
    }
    if (page.url().includes('ranges') && url.searchParams.get('view') === 'index') data = {rows:[{id:'deepseek-flash',name:'DeepSeek Flash',pending:true,change:'UNCHANGED'},{id:'retired',name:'Historical model',pending:false,change:'UNCHANGED'}]};
    if (page.url().includes('pages')) {
      if (url.searchParams.get('view') === 'index') { const all=Array.from({length:21},(_,i)=>({id:'model-'+i,name:i===20?'Tail model':'Model '+i,pending:true,change:'UNCHANGED'}));data={rows:all.slice((Number(url.searchParams.get('page'))-1)*20,Number(url.searchParams.get('page'))*20),pagination:{total:21,pageSize:20}}; }
      else if (url.searchParams.get('view') === 'model') {data.latestAttempt.result.models[0].id=url.searchParams.get('modelId');data.latestAttempt.result.models[0].name=url.searchParams.get('modelId')==='model-20'?'Tail model':'Model 0';data.pendingModelIds=[url.searchParams.get('modelId')];}
    }
    if (page.url().includes('selection')) {
      if (url.searchParams.get('view') === 'index') data.rows.push({id:'deepseek-v4-pro',name:'DeepSeek Pro',pending:true,change:'CHANGED'});
      if (url.searchParams.get('view') === 'model') {
        const id=url.searchParams.get('modelId');data.pendingModelIds=['deepseek-flash','deepseek-v4-pro'];
        data.latestAttempt.result.models[0].id=id;data.latestAttempt.result.models[0].name=id==='deepseek-flash'?'DeepSeek Flash':'DeepSeek Pro';
        // Conflicting evidence must not be included by select-all.
        data.latestAttempt.result.models[0].fieldEvidence={contextLength:{state:'CONFLICT'}};
      }
    }
    await route.fulfill({ json: { code: 0, data } });
  });
  await page.route('**/api/internal/standard-models/operations?*',route=>route.fulfill({json:{code:0,data:{page:1,size:100,total:1,list:[
    {id:'10',entity_id:'1',entity_type:'standard_model',action:'UPDATE',model_name:'DeepSeek Flash',operator_name:'运营人员',created_at:'2026-11-24T01:00:00Z',reason:'审核采纳'}]}}}));
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture`);
  await page.getByRole('cell', { name: '访问与规格详情 / 并发上限', exact: true }).waitFor();
  assert.equal(await page.getByRole('cell', { name: '10', exact: true }).count(), 1);
  assert.equal(await page.getByRole('cell', { name: '20', exact: true }).count(), 1);
  assert.equal(await page.getByRole('cell', { name: /校验摘要/ }).count(), 0);
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?hashOnly`);
  await page.getByText('与本人上次采集比较 · 无业务变化', { exact: true }).waitFor();
  assert.equal(await page.getByRole('cell').count(), 0);
  assert.equal(await page.getByText('待审 · 无变化', { exact: true }).count(), 1);
  assert.equal(await page.getByRole('button', {name:/批准当前模型/}).count(), 0);
  assert.equal(await page.getByText('上次 old-digest', {exact:true}).isVisible(), false);
  await page.locator('summary').filter({hasText:'采集技术记录'}).click();
  await page.getByText('上次 old-digest', {exact:true}).waitFor();
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?limitHistory`);
  await page.getByText('与本人上次采集比较 · 无业务变化',{exact:true}).waitFor();
  assert.equal(await page.getByRole('cell').count(),0);
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?limitHistory&limitChanged`);
  await page.getByRole('cell',{name:'限制条件 / 最大输入',exact:true}).waitFor();
  await page.getByRole('cell',{name:'65,536 Token',exact:true}).waitFor();
  await page.getByRole('cell',{name:'32,768 Token',exact:true}).waitFor();
  assert.doesNotMatch(await page.locator('.history-comparison').count()?await page.locator('.history-comparison').innerText():await page.locator('body').innerText(),/ev-old|new-limit-digest|rawLabel|UNSPECIFIED/);
  await page.locator('.el-loading-mask').waitFor({state:'hidden'});
  await page.screenshot({path:'E:/Desktop/Dongxin/.tmp/catalog-review-layout-20261124/limit-difference.png',fullPage:true,animations:'disabled'});
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?reviewFields`);
  await page.getByRole('cell',{name:'128,000',exact:true}).first().waitFor();
  assert.equal(await page.locator('.field-group h4').filter({hasText:'容量规格'}).count(),1);
  assert.equal(await page.getByRole('checkbox',{name:/^采纳/}).count(),0);
  assert.ok(await page.getByText('一致',{exact:true}).count()>=5);
  await page.locator('.el-loading-mask').waitFor({state:'hidden'});
  await page.screenshot({path:'E:/Desktop/Dongxin/.tmp/catalog-review-layout-20261124/review-fields.png',fullPage:true,animations:'disabled'});
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?reviewFields&partial`);
  await page.getByRole('checkbox',{name:'采纳模型名称',exact:true}).check();
  await page.getByRole('button',{name:'批准当前模型（1 项）',exact:true}).click();
  await page.getByText('BSS 已确认写入。',{exact:true}).waitFor();
  assert.equal(writes.length,1);
  assert.equal(writes[0].modelId,'deepseek-flash');
  assert.equal(writes[0].overrides.name,'DeepSeek Flash New');
  assert.equal(writes[0].overrides.capacity[0].value,'128000');
  assert.deepEqual(writes[0].expected,{id:'1',revision:'2'});
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?reviewFields&partial&selection`);
  await page.getByRole('button',{name:'全选当前模型字段',exact:true}).click();
  assert.equal(await page.getByRole('checkbox',{name:'采纳模型名称',exact:true}).isChecked(),true);
  assert.equal(await page.getByRole('checkbox',{name:'采纳上下文',exact:true}).count(),0);
  await page.locator('aside button').filter({hasText:'DeepSeek Pro'}).click();
  await page.getByRole('heading',{name:'DeepSeek Pro',exact:true}).waitFor();
  assert.equal(await page.getByRole('checkbox',{name:'采纳模型名称',exact:true}).isChecked(),false);
  await page.getByRole('button',{name:'全选当前模型字段',exact:true}).click();
  await page.getByRole('button',{name:'待提交 2 个模型 · 2 个字段',exact:true}).click();
  await page.locator('.selection-list button').filter({hasText:'DeepSeek Flash'}).click();
  await page.getByRole('heading',{name:'DeepSeek Flash',exact:true}).waitFor();
  assert.equal(await page.getByRole('checkbox',{name:'采纳模型名称',exact:true}).isChecked(),true);
  assert.equal(await page.locator('.picked-badge').count(),2);
  await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'清空选择',exact:true}).click();
  assert.equal(await page.getByRole('button',{name:'待提交 1 个模型 · 1 个字段',exact:true}).count(),1);
  await page.screenshot({path:'E:/Desktop/Dongxin/.tmp/catalog-review-layout-20261124/selection-overview.png',fullPage:true});
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?reviewFields&partial&ranges`);
  await page.getByRole('checkbox',{name:'勾选模型 DeepSeek Flash',exact:true}).check();
  await page.getByRole('button',{name:'待提交 1 个模型 · 2 个字段'}).waitFor();
  assert.equal(await page.getByRole('button',{name:'待提交 1 个模型 · 2 个字段'}).count(),1);
  await page.getByRole('checkbox',{name:'勾选模型 DeepSeek Flash',exact:true}).uncheck();
  await page.getByRole('combobox',{name:'型号范围',exact:true}).selectOption('changed');
  await page.getByText('本次没有较上次新增或变更的型号。',{exact:true}).waitFor();
  await page.getByRole('button',{name:'查看全部待审（1）',exact:true}).click();
  await page.getByRole('searchbox',{name:'搜索型号',exact:true}).fill('not-found');
  await page.getByText('未找到匹配型号，请调整搜索条件。',{exact:true}).waitFor();
  await page.getByRole('button',{name:'清空搜索',exact:true}).click();
  await page.getByRole('combobox',{name:'型号范围',exact:true}).selectOption('all');
  await page.locator('aside button').filter({hasText:'Historical model'}).waitFor();
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?reviewFields&pages`);
  await page.getByText('本批 21 个型号 · 待审 21 个 · 当前显示 21 个',{exact:true}).waitFor();
  await page.getByRole('searchbox',{name:'搜索型号',exact:true}).fill('Tail model');
  await page.getByRole('heading',{name:'Tail model',exact:true}).waitFor();
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?records`);
  await page.getByRole('cell',{name:'DeepSeek Flash',exact:true}).waitFor();
  const recordBounds=await page.locator('.model-records').boundingBox();
  const filterBounds=await page.locator('.record-filters .el-select').first().boundingBox();
  assert.ok(filterBounds.x-recordBounds.x>=22);
  assert.equal(await page.locator('.record-table td').first().evaluate(node=>getComputedStyle(node).paddingLeft),'22px');
  await page.getByText('操作时间',{exact:true}).waitFor();
  await page.screenshot({path:'E:/Desktop/Dongxin/.tmp/catalog-review-layout-20261124/operation-records.png',fullPage:true});
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__catalog-fixture?mode=schedule`);
  await page.getByRole('button', { name: '编辑', exact: true }).waitFor({timeout: 5000}).catch(async error => { console.log(await page.locator('body').innerText()); throw error; });
  await page.getByRole('button', { name: '编辑', exact: true }).click();
  await page.getByRole('dialog', { name: '编辑抓取计划', exact: true }).waitFor();
  assert.equal(await page.getByRole('button', { name: '新建计划', exact: true }).count(), 0);
  assert.equal(await page.locator('select').first().inputValue(), 'weekly');
  assert.equal(await page.locator('input[type=time]').inputValue(), '10:30');
  await page.getByRole('button', { name: '保存修改', exact: true }).waitFor();
  await page.setViewportSize({ width: 620, height: 760 });
  assert.ok((await page.getByRole('dialog').boundingBox()).width <= 620);
  assert.deepEqual(errors, []);
  console.log('PASS nested history diff, edit plan prefill/actions, narrow dialog (API fixtures)');
} finally { await browser?.close(); await server?.close(); }
