from pathlib import Path
import subprocess,json,os,time,sys
ART=Path(__file__).resolve().parent
ROOT=Path(os.environ.get('RESPONSE_WORK_ROOT','/tmp/shunter-response-path-20260908T130044Z'))
checks=[
 ('arrival-tests','canary',['rtk','go','test','./internal/workflows','-run','^TestCanaryArrival','-count=1']),
 ('workflow-tests','canary',['rtk','go','test','./internal/workflows','-run','TestDeclaredReadWorkflow|TestPermissionDeniedDeclared|TestOrderedTicketDeliveryAcrossSnapshot|Test.*UnsubscribeWorkflow|TestSubscriptionUnsubscribeLifecycleWorkflow','-count=1']),
 ('workflow-race','canary',['rtk','go','test','-race','./internal/workflows','-run','TestCanaryArrival|TestDeclaredReadWorkflow|TestOrderedTicketDeliveryAcrossSnapshot','-count=1']),
 ('diag-format-runtime','diagnostic/shunter',['rtk','go','fmt','./protocol','./protocolclient','./responseprobe','.']),
 ('diag-format-canary','diagnostic/canary',['rtk','go','fmt','./internal/workflows']),
 ('diag-probe-race','diagnostic/shunter',['rtk','go','test','-race','./responseprobe','-count=1']),
 ('diag-protocol-race','diagnostic/shunter',['rtk','go','test','-race','./protocol','./protocolclient','-count=1']),
 ('diag-query-tests','diagnostic/shunter',['rtk','go','test','.','-run','TestProtocolDeclaredQuery|TestProtocolDeclaredReadsApplyVisibility','-count=1']),
 ('canary-vet','canary',['rtk','go','vet','./internal/workflows']),
 ('diag-canary-vet','diagnostic/canary',['rtk','go','vet','./internal/workflows']),
 ('diag-runtime-vet','diagnostic/shunter',['rtk','go','vet','.','./protocol','./protocolclient','./responseprobe']),
 ('runtime-staticcheck','shunter',['rtk','go','tool','staticcheck','./...']),
 ('diag-runtime-staticcheck','diagnostic/shunter',['rtk','go','tool','staticcheck','./...']),
]
for name,rel,cmd in checks:
 p=ART/('check-'+name);p.mkdir(exist_ok=False)
 (p/'command.json').write_text(json.dumps({'cwd':str(ROOT/rel),'command':cmd,'started':time.time()},indent=2)+'\n')
 with (p/'raw.txt').open('w') as log:r=subprocess.run(cmd,cwd=ROOT/rel,stdout=log,stderr=subprocess.STDOUT,timeout=300)
 print(name,r.returncode,flush=True)
 (p/'exit.json').write_text(json.dumps({'exit_code':r.returncode,'ended':time.time()})+'\n')
 if r.returncode:print((p/'raw.txt').read_text());sys.exit(r.returncode)
# Canary has no tool directive: use the exact Shunter-pinned executable.
static=subprocess.check_output(['rtk','proxy','go','tool','-n','staticcheck'],cwd=ROOT/'shunter').decode().strip()
for name,rel in [('canary-staticcheck','canary'),('diag-canary-staticcheck','diagnostic/canary')]:
 p=ART/('check-'+name);p.mkdir()
 cmd=['rtk','proxy',static,'./...']
 (p/'command.json').write_text(json.dumps({'cwd':str(ROOT/rel),'command':cmd},indent=2)+'\n')
 with (p/'raw.txt').open('w') as log:r=subprocess.run(cmd,cwd=ROOT/rel,stdout=log,stderr=subprocess.STDOUT,timeout=300)
 print(name,r.returncode,flush=True)
 (p/'exit.json').write_text(json.dumps({'exit_code':r.returncode})+'\n')
 if r.returncode:sys.exit(r.returncode)
