"""Run the frozen bounded matrix serially. Existing invocation directories are never rerun."""
from pathlib import Path
import datetime,hashlib,json,os,signal,subprocess,time,sys
ART=Path(__file__).resolve().parent
ROOT=Path(os.environ.get('RESPONSE_WORK_ROOT','/tmp/shunter-response-path-20260908T130044Z'))
def now():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def host():
 return {'utc':now(),'load':os.getloadavg(),'meminfo':Path('/proc/meminfo').read_text(),'processes':subprocess.check_output(['rtk','proxy','ps','-eo','pid,comm,pcpu,rss','--sort=-pcpu']).decode()}
def verify():
 frozen=json.loads((ART/'frozen-inputs.json').read_text())
 for name,files in frozen.items():
  for rel,info in files.items():
   p=ROOT/name/rel
   data=p.read_bytes()
   if rel=='go.mod' and name in ['canary','diagnostic/canary'] and os.environ.get('RESPONSE_WORK_ROOT'):
    data=data.replace(str(ROOT).encode(),b'/tmp/shunter-response-path-20260908T130044Z')
   assert hashlib.sha256(data).hexdigest()==info['sha256'],(name,rel)
 matrix=json.loads((ART/'frozen-matrix.json').read_text())
 assert hashlib.sha256((ART/'contract.md').read_bytes()).hexdigest()==matrix['contract']['sha256']
 return matrix['runs']
for cell in verify():
 dest=ART/cell['name']
 if dest.exists():
  status=dest/'exit.json'
  if not status.exists() or json.loads(status.read_text())['exit_code']!=0:raise SystemExit('Stopped at incomplete/failed invocation '+str(dest))
  print('Already complete:',cell['name'],flush=True);continue
 verify()
 dest.mkdir()
 diag=cell['mode']=='diagnostic';cwd=ROOT/('diagnostic/canary' if diag else 'canary')
 overrides={'GOTOOLCHAIN':'go1.27.1','CANARY_CAPACITY_MODE':'scheduled-arrival','CANARY_CAPACITY_FIXTURES':str(ROOT/'fixtures'),'CANARY_CAPACITY_RATE':str(cell['rate']),'CANARY_CAPACITY_SHAPE':cell['shape'],'CANARY_CAPACITY_PER_CLIENT':str(cell['per_client']),'CANARY_CAPACITY_RESULT_DIR':str(dest)}
 env=dict(os.environ,**overrides)
 cmd=['go','test','./internal/workflows','-run','^$','-bench','^BenchmarkCanaryScheduledArrival$','-benchtime=1x','-count=1','-timeout=170s','-benchmem']
 (dest/'command.json').write_text(json.dumps({'command':cmd,'cwd':str(cwd),'environment':overrides,'outer_timeout_seconds':180,'started':now(),'host_before':host()},indent=2)+'\n')
 print('Starting',cell['name'],flush=True)
 with (dest/'raw.txt').open('w') as log:
  process=subprocess.Popen(cmd,cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
  (dest/'process.json').write_text(json.dumps({'pid':process.pid,'started':now()})+'\n')
  try:code=process.wait(timeout=180)
  except subprocess.TimeoutExpired:
   os.killpg(process.pid,signal.SIGTERM)
   try:process.wait(timeout=5)
   except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
   code=124
 (dest/'host-after.json').write_text(json.dumps(host(),indent=2)+'\n')
 errors=[]
 if code==0:
  try:
   r=json.loads((dest/'result.json').read_text());m=r['Report']
   assert r['Checked'] and not r['Errors'] and len(r['Snapshots'])==2
   assert m['success-count']==m['completion-count']==m['dispatch-count']==m['offered-count']==3200
   assert not any(m[k] for k in ['error-count','timeout-count','rejection-count','unknown-write-count','undispatched-count'])
   if diag:
    for role in ['client','server']:
     v=json.loads((dest/f'probe-{role}.json').read_text())
     assert v['Dropped']==v['CorrelationFailures']==0
  except Exception as e:code=1;errors.append(repr(e))
 (dest/'exit.json').write_text(json.dumps({'exit_code':code,'errors':errors,'ended':now()},indent=2)+'\n')
 print('Finished',cell['name'],code,flush=True)
 if code:raise SystemExit('Failure preserved; dependent cells stopped.')
print('All 8 ordinary + 4 diagnostic invocations complete.',flush=True)
