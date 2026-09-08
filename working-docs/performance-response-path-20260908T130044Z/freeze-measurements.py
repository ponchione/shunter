from pathlib import Path
import datetime,difflib,hashlib,json,os,shutil,subprocess,tarfile
ART=Path(__file__).resolve().parent;ROOT=Path('/tmp/shunter-response-path-20260908T130044Z')
def digest(p):return {'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'bytes':p.stat().st_size}
frozen={}
for name in ['shunter','canary','diagnostic/shunter','diagnostic/canary','fixtures']:
 base=ROOT/name
 frozen[name]={str(p.relative_to(base)):digest(p) for p in sorted(base.rglob('*')) if p.is_file() and p.relative_to(base).parts[0] not in ['.git','working-docs','reference']}
(ART/'frozen-inputs.json').write_text(json.dumps(frozen,indent=2)+'\n')
for name in ['shunter','canary']:
 ordinary=ROOT/name;diag=ROOT/'diagnostic'/name
 patch=''
 for rel in sorted(set(frozen[name])|set(frozen['diagnostic/'+name])):
  a,b=ordinary/rel,diag/rel
  if a.is_file() and b.is_file() and a.read_bytes()==b.read_bytes():continue
  # Ignore stale copied docs/testdata: only runtime + actual Canary harness affect implementation.
  if name=='shunter' and rel.startswith(('testdata/','docs/','scripts/')):continue
  if name=='shunter' and rel=='CHANGELOG.md':continue
  if rel=='go.mod':continue
  aa=a.read_text().splitlines(True) if a.is_file() else []
  bb=b.read_text().splitlines(True) if b.is_file() else []
  patch+=''.join(difflib.unified_diff(aa,bb,fromfile='a/'+rel,tofile='b/'+rel))
 (ART/('diagnostic-'+name+'.patch')).write_text(patch)
# All overlay bytes required for reconstruction, including relevant untracked harness/probes.
with tarfile.open(ART/'experiment-overlays.tar.gz','w:gz') as tar:
 source=json.loads((ART/'source-manifest.json').read_text())
 for name in ['shunter','canary']:
  for rel,info in frozen[name].items():
   old=source[name]['all_preexisting_files'].get(rel)
   if old==info:continue
   tar.add(ROOT/name/rel,arcname=name+'/'+rel,recursive=False)
 for name in ['shunter','canary']:
  for rel in frozen['diagnostic/'+name]:
   p=ROOT/'diagnostic'/name/rel;a=ROOT/name/rel
   if not a.exists() or a.read_bytes()!=p.read_bytes():tar.add(p,arcname='diagnostic/'+name+'/'+rel,recursive=False)
maintained=['scripts/measure-canary-capacity','testdata/canary_capacity_test.go','testdata/canary_arrival_test.go','testdata/canary_arrival_report_test.go','testdata/canary_capacity_check_test.go','docs/benchmarks.md','CHANGELOG.md']
# Diff against preserved working-tree inputs, not HEAD (which has unrelated PERF edits).
with tarfile.open(ART/'shunter-source.tar.gz') as tar:
 patch=''
 for rel in maintained:
  try:before=tar.extractfile(rel).read().decode().splitlines(True)
  except KeyError:before=[]
  patch+=''.join(difflib.unified_diff(before,(ROOT/'shunter'/rel).read_text().splitlines(True),fromfile='a/'+rel,tofile='b/'+rel))
 (ART/'maintained-harness.patch').write_text(patch)
matrix=[]
for rate,shape in [(1000,'even'),(4000,'burst'),(4000,'even'),(1000,'burst')]:
 for mode in ['ordinary-a','diagnostic','ordinary-b']:
  matrix.append({'name':f'{rate}-{shape}-{mode}','rate':rate,'shape':shape,'mode':mode,'per_client':100})
(ART/'frozen-matrix.json').write_text(json.dumps({'frozen_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'runs':matrix,'caps':{'ordinary':8,'diagnostic':4},'contract':digest(ART/'contract.md')},indent=2)+'\n')
print('Frozen',len(matrix),'invocations',sum(map(len,frozen.values())),'source/fixture files')
