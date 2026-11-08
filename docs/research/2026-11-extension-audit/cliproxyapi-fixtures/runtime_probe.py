#!/usr/bin/env python3
"""Loopback-only runtime probe; use /tmp/cpa-audit/cli-proxy-api and audit.so."""
import http.server,threading,socket,json,subprocess,os,time,pathlib,urllib.request,urllib.error
root=pathlib.Path('/tmp/cpa-audit'); captures=[]; results=[]; failed_once=False
class Mock(http.server.BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_POST(self):
  global failed_once
  req=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  captures.append({'path':self.path,'body':req})
  if 'audit-failover' in str(req) and not failed_once:
   failed_once=True;body=b'{"error":{"message":"synthetic quota exhausted"}}'
   self.send_response(429);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body);return
  if req.get('stream'):
   self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
   for row in [{'id':'mock','object':'chat.completion.chunk','model':'audit-model','choices':[{'index':0,'delta':{'role':'assistant','content':'mock text'},'finish_reason':None}]},{'id':'mock','object':'chat.completion.chunk','model':'audit-model','choices':[{'index':0,'delta':{},'finish_reason':'stop'}],'usage':{'prompt_tokens':7,'completion_tokens':3,'total_tokens':10}}]:self.wfile.write(('data: '+json.dumps(row)+'\n\n').encode())
   self.wfile.write(b'data: [DONE]\n\n');self.wfile.flush()
  else:
   answer={'id':'mock','object':'chat.completion','model':'audit-model','choices':[{'index':0,'message':{'role':'assistant','content':'mock text'},'finish_reason':'stop'}],'usage':{'prompt_tokens':7,'completion_tokens':3,'total_tokens':10}}
   if 'audit-missing' in str(req):answer.pop('usage')
   body=json.dumps(answer).encode()
   self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
mock=http.server.ThreadingHTTPServer(('127.0.0.1',0),Mock);threading.Thread(target=mock.serve_forever,daemon=True).start()
s=socket.socket();s.bind(('127.0.0.1',0));port=s.getsockname()[1];s.close()
(root/'config.yaml').write_text(f'''host: 127.0.0.1
port: {port}
auth-dir: /tmp/cpa-audit/auths
api-keys: [native-example-key]
remote-management:
  secret-key: example-management-key
  disable-control-panel: true
request-retry: 0
openai-compatibility:
  - name: audit-upstream
    base-url: http://127.0.0.1:{mock.server_port}/v1
    api-key-entries:
      - api-key: synthetic-upstream-key
      - api-key: synthetic-upstream-key-two
    models:
      - name: audit-model
        alias: audit-model
plugins:
  enabled: true
  dir: /tmp/cpa-audit/plugins
  configs:
    audit:
      enabled: true
      priority: 10
''')
(root/'plugin.log').write_text('')
env=os.environ.copy();env['CPA_AUDIT_LOG']=str(root/'plugin.log')
proc=subprocess.Popen([str(root/'cli-proxy-api'),'--config',str(root/'config.yaml'),'--local-model'],cwd=root,env=env,stdout=(root/'server.log').open('w'),stderr=subprocess.STDOUT)
def request(path,method='GET',body=None,headers=None):
 try:
  r=urllib.request.urlopen(urllib.request.Request(f'http://127.0.0.1:{port}'+path,data=body,method=method,headers=headers or {}),timeout=8)
  return r.status,r.read().decode(),dict(r.headers)
 except urllib.error.HTTPError as e:return e.code,e.read().decode(),dict(e.headers)
def check(name,condition,**observed):
 results.append({'name':name,'pass':bool(condition),**observed});print(json.dumps(results[-1]),flush=True)
try:
 for i in range(100):
  try:request('/');break
  except (OSError,urllib.error.URLError):time.sleep(.1)
 else:raise RuntimeError('server did not start')
 for mode in ['audit-normal','audit-deny','audit-error','audit-stop','audit-rewrite','audit-drop','audit-nested','audit-missing','audit-failover']:
  before=len(captures);stream=mode=='audit-drop'
  status,body,headers=request('/v1/chat/completions','POST',json.dumps({'model':'audit-model','stream':stream,'messages':[{'role':'user','content':mode if mode in ['audit-missing','audit-failover'] else 'Example user message'}]}).encode(),{'Content-Type':'application/json','X-Audit-Mode':mode})
  expected=403 if mode in ['audit-deny','audit-stop'] else 200
  check(mode,status==expected and len(captures)-before==(0 if mode in ['audit-deny','audit-stop'] else 2 if mode in ['audit-nested','audit-failover'] else 1),status=status,body=body,upstream_calls=len(captures)-before)
 for path,method,h in [('/v0/management/audit/start','POST',{}),('/v0/management/audit/start','POST',{'Authorization':'Bearer example-management-key'}),('/v0/resource/plugins/audit/status','GET',{}),('/v0/resource/plugins/audit/status','POST',{}),('/api/private/v1/public-enrollments','POST',{})]:
  status,body,headers=request(path,method,headers=h);expected=401 if 'management' in path and not h else 200 if ('management' in path and h) or ('resource' in path and method=='GET') else 404
  check(method+' '+path,status==expected,status=status,body=body)
 time.sleep(.5)
 calls=[]
 for line in (root/'plugin.log').read_text().splitlines():
  m,q=line.split('\t',1);calls.append({'method':m,'request':json.loads(q) if q else None})
 usage=[x['request'] for x in calls if x['method']=='usage.handle']
 check('usage observed',len(usage)==9 and sum(u['Failed'] for u in usage)==1 and sum(u['Detail']['TotalTokens']==10 for u in usage)==7,records=usage)
 check('stream suppression leaves host terminator',next(r['body'] for r in results if r['name']=='audit-drop')=='data: [DONE]\n\n')
 check('presentation rewrite applied',json.loads(next(r['body'] for r in results if r['name']=='audit-rewrite'))['usage']['total_tokens']==1998)
 (root/'observations.json').write_text(json.dumps({'results':results,'upstream':captures,'rpc':calls},indent=2))
 assert all(r['pass'] for r in results), 'one or more audit assertions failed'
finally:
 proc.terminate()
 try:proc.wait(timeout=5)
 except subprocess.TimeoutExpired:proc.kill();proc.wait()
 mock.shutdown()
