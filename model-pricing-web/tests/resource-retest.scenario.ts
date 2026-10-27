import test from "node:test";
import assert from "node:assert/strict";
import { setupServer } from "msw/node";
import { http,HttpResponse } from "msw";
import { createPinia,setActivePinia } from "pinia";
import { useSessionStore } from "../src/stores/session";
import { resourceRetestsApi } from "../src/api/resourceRetests";

test("internal retest client sends only category and an idempotency key, never credentials",async()=>{
 setActivePinia(createPinia());const session=useSessionStore();session.user={id:"7",name:"handler",roleLabel:"internal"};session.portal="internal";session.token="test-token";
 const storage=new Map<string,string>();Object.defineProperty(globalThis,"sessionStorage",{configurable:true,value:{getItem:(k:string)=>storage.get(k)??null,setItem:(k:string,v:string)=>storage.set(k,v),removeItem:(k:string)=>storage.delete(k)}});
 let calls=0;
 const server=setupServer(
  http.get("http://localhost/api/internal/resources/11/tests/options",()=>HttpResponse.json({code:0,data:{can_start:true,reason:"",categories:["baseline","stress"]}})),
  http.post("http://localhost/api/internal/resources/11/tests",async({request})=>{calls++;assert.deepEqual(await request.json(),{category:"baseline"});assert.ok(request.headers.get("Idempotency-Key"));return HttpResponse.json({code:0,data:{job_id:"31",external_job_id:"platform31",status:"queued",api_key:"must-not-copy"}});}),
 );server.listen({onUnhandledRequest:"error"});
 try{assert.equal((await resourceRetestsApi.options("11")).can_start,true);assert.deepEqual(await resourceRetestsApi.start("11","baseline","intent"),{job_id:"31",status:"queued"});await assert.rejects(resourceRetestsApi.start("11","invalid" as "baseline","intent"));assert.equal(calls,1);}finally{server.close()}
});
