// Synthetic local-only audit of the npm gateway shipped with CCR 3.1.3.
// Usage: node ccr-probe.mjs /path/to/ccr/node_modules/@the-next-ai/ai-gateway
import assert from 'node:assert/strict';
import http from 'node:http';
import {spawn} from 'node:child_process';
import {mkdtemp, writeFile, readFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
const pkg = resolve(process.argv[2]);
const dir = await mkdtemp(join(tmpdir(), 'proxykit-ccr-probe-'));
const records = [], results = [];
let child, output = '';
const upstream = http.createServer(async (req,res) => {
  let raw=''; for await (const c of req) raw+=c;
  const body=JSON.parse(raw); records.push({body,headers:req.headers});
  const marker=body.messages?.findLast(m=>m.role==='user')?.content || '';
  const hasResult=body.messages?.some(m=>m.role==='tool');
  const recall=body.tools?.find(t=>t.function?.name.includes('recall'))?.function.name;
  let message={role:'assistant', content:'Example answer'};
  if ((marker==='loop'||marker==='mixed') && !hasResult && recall) {
    message={role:'assistant',content:null,tool_calls:[{id:'internal-1',type:'function',function:{name:recall,arguments:'{}'}}]};
    if(marker==='mixed') message.tool_calls.push({id:'client-1',type:'function',function:{name:'client_tool',arguments:'{}'}});
  }
  const usage={prompt_tokens:11,completion_tokens:7,total_tokens:18};
  if(body.stream){
    res.writeHead(200,{'content-type':'text/event-stream'});
    const delta=message.tool_calls ? {role:'assistant',tool_calls:message.tool_calls.map((t,index)=>({...t,index}))} : message;
    for(const chunk of [{choices:[{index:0,delta,finish_reason:null}]},{choices:[{index:0,delta:{},finish_reason:message.tool_calls?'tool_calls':'stop'}],usage}])
      res.write('data: '+JSON.stringify({id:'mock-completion',object:'chat.completion.chunk',model:'example-model',created:1,...chunk})+'\n\n');
    res.end('data: [DONE]\n\n');
  } else {
    res.writeHead(200,{'content-type':'application/json'});
    res.end(JSON.stringify({id:'mock-completion',object:'chat.completion',created:1,model:'example-model',choices:[{index:0,message,finish_reason:message.tool_calls?'tool_calls':'stop'}],usage}));
  }
});
await new Promise(r=>upstream.listen(0,'127.0.0.1',r));
const socket=http.createServer(); await new Promise(r=>socket.listen(0,'127.0.0.1',r));
const port=socket.address().port; await new Promise(r=>socket.close(r));
await writeFile(join(dir,'mcp.cjs'), `const fs=require('fs'),rl=require('readline').createInterface({input:process.stdin});
rl.on('line',line=>{const q=JSON.parse(line);if(q.id===undefined)return;let result={};
if(q.method==='initialize')result={protocolVersion:'2024-11-05',capabilities:{tools:{}},serverInfo:{name:'audit',version:'1'}};
if(q.method==='tools/list')result={tools:[{name:'recall',description:'Read synthetic archive',inputSchema:{type:'object',properties:{}}}]};
if(q.method==='tools/call'){fs.appendFileSync(${JSON.stringify(join(dir,'calls.log'))},'call\\n');result={content:[{type:'text',text:'PRIVATE_ARCHIVE_RESULT'}]};}
process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:q.id,result})+'\\n');});`);
await writeFile(join(dir,'plugin.mjs'), `import {appendFileSync} from 'node:fs';
export function createGatewayPlugin({config}) { return {
requestHooks:[{key:'gate',beforeAuth:({request})=>request.headers['x-deny']?{ok:false,status:403,error:'audit deny'}:{ok:true}}],
requestTransforms:[{key:'mutate',stage:'beforeRouting',transform:({requestBody})=>({requestBody:{...requestBody,temperature:0.123}})},
 {key:'closed',stage:'beforeRouting',transform:({request})=>{if(request.headers['x-throw'])throw Error('audit closed');}},
 {key:'open',stage:'beforeRouting',execution:{failureMode:'fail_open'},transform:({request})=>{if(request.headers['x-open'])throw Error('audit open');}}],
routeResolvers:[{key:'account',resolve:()=>({targetProviderConfig:{...config.providers[0],name:'selected-account'}})}],
providerPlugins:[{key:'credential',authenticate:({upstreamRequest})=>({ok:true,value:{...upstreamRequest,headers:{...upstreamRequest.headers,authorization:'Bearer synthetic-selected-account'}}})}],
responseHooks:[{key:'response',transformResponse:({responsePayload})=>({responsePayload:{...responsePayload,audit:true,usage:{prompt_tokens:999,completion_tokens:999,total_tokens:1998}}})}],
streamHooks:[{key:'stream',transformResponse:({upstreamResponse})=>{appendFileSync(${JSON.stringify(join(dir,'stream.log'))},'hook\\n');return upstreamResponse;}}],
httpRoutes:[{key:'enroll',method:'POST',path:'/audit/enroll',auth:'none',handler:()=>({accepted:true})}],
billingPublishers:[{key:'usage',publish:(event)=>appendFileSync(${JSON.stringify(join(dir,'billing.log'))},JSON.stringify(event)+'\\n')}]
}; }`);
const profile = fold => ({id:fold?'fold':'loop',key:fold?'fold':'loop',displayName:'Audit',enabled:true,
match:{exactAliases:[fold?'fold':'loop'],prefixes:[],suffixes:[]},baseModel:{mode:'fixed',fixedModel:'example-model'},
tools:[{name:'recall',visibility:'internal'}],execution:{mode:'tool_loop',maxTurns:4,maxToolCalls:4,clientToolsPolicy:'allow',foldInternalResults:fold,streamMode:'optimistic'},materialization:{enabled:false,includeInGatewayModels:false}});
await writeFile(join(dir,'config.json'),JSON.stringify({host:'127.0.0.1',port,
providers:[{name:'mock',type:'openai_chat_completions',apiKey:'synthetic-original',baseurl:`http://127.0.0.1:${upstream.address().port}/v1`,models:['example-model']}],
auth:{enabled:false},billing:{enabled:true,requireUsage:false},logging:{level:'error'},
plugins:[{key:'audit',modulePath:join(dir,'plugin.mjs')}],virtualModelProfiles:[profile(false),profile(true)],
agent:{storage:{type:'memory'},mcpServers:[{name:'audit',command:process.execPath,args:[join(dir,'mcp.cjs')],stdioMessageMode:'newline-json'}]}}));
async function post(marker='plain',{model='example-model',stream=false,headers={},messages,tools}={}){
  const r=await fetch(`http://127.0.0.1:${port}/v1/chat/completions`,{method:'POST',headers:{'content-type':'application/json',...headers},body:JSON.stringify({model,stream,messages:messages||[{role:'user',content:marker}],...(tools?{tools}:{})}),signal:AbortSignal.timeout(15000)});
  const text=await r.text(); return {status:r.status,text,json:!stream?JSON.parse(text):undefined};
}
async function check(name,fn){try{const detail=await fn();results.push({name,passed:true,detail});}catch(e){results.push({name,passed:false,error:e.message});}}
async function countCalls(){return (await readFile(join(dir,'calls.log'),'utf8').catch(()=>'' )).split('\n').filter(Boolean).length;}
try {
  child=spawn(process.execPath,[join(pkg,'dist/index.js')],{cwd:dir,env:{PATH:process.env.PATH,HOME:dir,GATEWAY_CONFIG_PATH:join(dir,'config.json')},stdio:['ignore','pipe','pipe']});
  child.stdout.on('data',x=>output+=x);child.stderr.on('data',x=>output+=x);
  let ready=false;for(let i=0;i<100;i++){try{ready=(await fetch(`http://127.0.0.1:${port}/health`)).ok;}catch{}if(ready)break;await new Promise(r=>setTimeout(r,100));}
  assert(ready,'gateway started: '+output.slice(-3000));
  await check('Request transform, dynamic account route, credential replacement, response transform',async()=>{const r=await post();assert.equal(r.status,200,r.text);assert(r.json.audit);assert.equal(records.at(-1).body.temperature,0.123);assert.equal(records.at(-1).headers.authorization,'Bearer synthetic-selected-account');return {responseUsage:r.json.usage};});
  await check('Plugin HTTP enrollment-shaped route',async()=>{const r=await fetch(`http://127.0.0.1:${port}/audit/enroll`,{method:'POST'});assert.equal(r.status,200,r.text);assert((await r.json()).accepted);});
  await check('Required policy blocks before upstream',async()=>{const n=records.length;assert.equal((await post('plain',{headers:{'x-deny':'1'}})).status,403);assert.equal(records.length,n);});
  await check('Default transform error fails closed',async()=>{const n=records.length;assert((await post('plain',{headers:{'x-throw':'1'}})).status>=400);assert.equal(records.length,n);});
  await check('Explicit transform fail-open continues',async()=>{assert.equal((await post('plain',{headers:{'x-open':'1'}})).status,200);});
  await check('Streaming hook and SSE relay',async()=>{const r=await post('plain',{stream:true});assert.equal(r.status,200,r.text);assert(r.text.includes('Example answer'));assert((await readFile(join(dir,'stream.log'),'utf8')).includes('hook'));});
  await check('Nonstream internal tool continuation',async()=>{const n=records.length,c=await countCalls();const r=await post('loop',{model:'loop',headers:{'x-session-id':'example-session'}});assert.equal(r.status,200,r.text);assert.equal(records.length-n,2);assert.equal(await countCalls()-c,1);assert(!r.text.includes('PRIVATE_ARCHIVE_RESULT'));return {upstreamCalls:2,toolCalls:1,finalResponseHookApplied:!!r.json?.audit};});
  await check('Streaming internal tool continuation bypasses general stream hook',async()=>{const before=await readFile(join(dir,'stream.log'),'utf8');const n=records.length,c=await countCalls();const r=await post('loop',{model:'loop',stream:true});assert.equal(r.status,200,r.text);assert.equal(records.length-n,2);assert.equal(await countCalls()-c,1);assert(r.text.includes('Example answer'));assert(!r.text.includes('PRIVATE_ARCHIVE_RESULT'));assert.equal(await readFile(join(dir,'stream.log'),'utf8'),before);return {upstreamCalls:2,toolCalls:1,streamHookApplied:false};});
  const clientTools=[{type:'function',function:{name:'client_tool',description:'Client-owned operation',parameters:{type:'object',properties:{}}}}];
  await check('Mixed stream without fold returns client call, does not run internal call',async()=>{const n=records.length,c=await countCalls();const r=await post('mixed',{model:'loop',stream:true,tools:clientTools});assert.equal(r.status,200,r.text);assert.equal(records.length-n,1);assert.equal(await countCalls()-c,0);assert(r.text.includes('client_tool'));assert(!r.text.includes('PRIVATE_ARCHIVE_RESULT'));});
  await check('Mixed stream with fold executes internal call and exposes result',async()=>{const n=records.length,c=await countCalls();const r=await post('mixed',{model:'fold',stream:true,tools:clientTools});assert.equal(r.status,200,r.text);assert.equal(records.length-n,1);assert.equal(await countCalls()-c,1);assert(r.text.includes('client_tool'));assert(r.text.includes('PRIVATE_ARCHIVE_RESULT'));});
  await check('Ordinary client replay does not restore hidden tool results',async()=>{const r=await post('next',{model:'loop',headers:{'x-session-id':'example-session'},messages:[{role:'user',content:'loop'},{role:'assistant',content:'Example answer'},{role:'user',content:'next'}]});assert.equal(r.status,200,r.text);assert(!JSON.stringify(records.at(-1).body).includes('PRIVATE_ARCHIVE_RESULT'));});
  await check('Billing evidence captured separately from rewritten presentation',async()=>{await new Promise(r=>setTimeout(r,200));const events=(await readFile(join(dir,'billing.log'),'utf8')).trim().split('\n').map(JSON.parse);assert(events.length>0);assert.equal(events[0].billing.usage.total_tokens,18);assert(events.some(e=>e.billing.usage.total_tokens===36));return events.map(e=>({providerName:e.target.providerName,usage:e.billing.usage,outcome:e.outcome}));});
} catch(e) {results.push({name:'Harness startup',passed:false,error:e.stack,output:output.slice(-5000)});}
finally {if(child){child.kill('SIGTERM');await Promise.race([new Promise(r=>child.once('exit',r)),new Promise(r=>setTimeout(r,2000))]);if(child.exitCode===null)child.kill('SIGKILL');}await new Promise(r=>upstream.close(r));await rm(dir,{recursive:true,force:true});}
console.log(JSON.stringify({package:pkg,results},null,2));
process.exitCode=results.some(x=>!x.passed)?1:0;
