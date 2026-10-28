import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseResourceWorkflow, buildApprovedPrice } from '../src/api/resourceWorkflow.contract.ts';
test('workflow actions use real provider capabilities; price is selected from approved sources',()=>{
 assert.equal(parseResourceWorkflow({can_bind:true,can_admit:false,can_refresh_tests:true,approved_prices:[]}).can_refresh_tests,true);
 const actions=parseResourceWorkflow({can_bind:false,can_admit:true,approved_prices:[{source_row_id:'71',source_revision:'3',coefficient:'0.5000'}]});
 assert.equal(actions.can_bind,false);
 const body=buildApprovedPrice('2',actions.approved_prices[0]!,true,' external receipt ',' assigned application ');
 assert.equal(body.source_revision,'3');assert.equal(body.coefficient,'0.5000');assert.equal(body.completion_note,'external receipt');
 assert.throws(()=>buildApprovedPrice('2',actions.approved_prices[0]!,false,'receipt','reason'));
 assert.throws(()=>parseResourceWorkflow({can_bind:'true',can_admit:true,approved_prices:[]}));
 assert.throws(()=>parseResourceWorkflow({can_bind:true,can_admit:true,approved_prices:[{source_row_id:'71',source_revision:3,coefficient:'0.5'}]}));
});
