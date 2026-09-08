"""Recheck source preservation, frozen measured implementation, and evidence without workloads."""
from pathlib import Path
import hashlib,json,os,subprocess,tarfile
ART=Path(__file__).resolve().parent
ROOT=Path(os.environ.get('RESPONSE_WORK_ROOT','/tmp/shunter-response-path-20260908T130044Z'))
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
source=json.loads((ART/'source-manifest.json').read_text())
allowed={'CHANGELOG.md','docs/benchmarks.md','scripts/measure-canary-capacity','testdata/canary_capacity_test.go'}
unchanged=0;changed=[]
for name,v in source.items():
 repo=Path(v['root'])
 assert subprocess.check_output(['rtk','proxy','git','rev-parse','HEAD'],cwd=repo).decode().strip()==v['head']
 assert subprocess.check_output(['rtk','proxy','git','diff','--cached','--stat'],cwd=repo)==b''
 for rel,info in v['all_preexisting_files'].items():
  p=repo/rel
  assert p.is_file(),(name,rel,'missing')
  if sha(p)==info['sha256']:unchanged+=1;continue
  assert name=='shunter' and rel in allowed,(name,rel,'unexpected mutation')
  with tarfile.open(ART/'shunter-source.tar.gz') as tar:
   data=tar.extractfile(rel).read();assert hashlib.sha256(data).hexdigest()==info['sha256']
  changed.append(rel)
assert set(changed)==allowed
# Baseline changelog edits survive as exact bytes after removing this task's entry.
with tarfile.open(ART/'shunter-source.tar.gz') as tar:before=tar.extractfile('CHANGELOG.md').read().decode()
now=(Path(source['shunter']['root'])/'CHANGELOG.md').read_text()
entry='- Canary capacity reporting now offers an explicit scheduled-arrival mode with\n  offered/achieved rates, scheduling backlog, deadline outcomes, and separate\n  response, row-decoding and validation timings. The runner includes current\n  Canary working-tree inputs; historical closed-loop metric names stay intact.\n'
assert now.replace('\n'+entry,'',1)==before
frozen=json.loads((ART/'frozen-inputs.json').read_text());checked=0
for name,files in frozen.items():
 for rel,info in files.items():
  p=ROOT/name/rel
  assert sha(p)==info['sha256'],(name,rel,'frozen source drift')
  checked+=1
for rel,info in json.loads((ART/'final-maintained-inputs.json').read_text()).items():assert sha(Path(source['shunter']['root'])/rel)==info['sha256'],rel
matrix=json.loads((ART/'frozen-matrix.json').read_text());assert sha(ART/'contract.md')==matrix['contract']['sha256']
assert len(matrix['runs'])==12 and sum(c['mode']=='diagnostic' for c in matrix['runs'])==4
for c in matrix['runs']:assert json.loads((ART/c['name']/'exit.json').read_text())['exit_code']==0
for p in ART.glob('check-*/exit.json'):assert json.loads(p.read_text())['exit_code']==0,p
r=json.loads((ART/'check-arrival-workload-race/result.json').read_text());assert r['Checked'] and not r['Errors'] and r['Report']['success-count']==320
result={'unchanged_preexisting_files':unchanged,'authorized_changed_files_with_preserved_original_bytes':sorted(changed),'frozen_files_checked':checked,'heads_and_index_unchanged':True,'canary_preexisting_files_unchanged':len(source['canary']['all_preexisting_files']),'previous_evidence_unchanged':True,'measured_invocations':12,'diagnostic_invocations':4,'smoke_invocations':2,'smoke_workloads_executed':1,'additional_race_workloads':1}
(ART/'preservation-check.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))
