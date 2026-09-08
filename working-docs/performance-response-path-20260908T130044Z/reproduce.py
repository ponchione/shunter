"""Reconstruct sources/fixtures without workloads: reproduce.py NEW_ROOT SHUNTER_REPO CANARY_REPO."""
from pathlib import Path
import hashlib,json,shutil,subprocess,sys,tarfile
ART=Path(__file__).resolve().parent
root,shunter,canary=map(lambda s:Path(s).resolve(),sys.argv[1:4]);root.mkdir()
source=json.loads((ART/'source-manifest.json').read_text());frozen=json.loads((ART/'frozen-inputs.json').read_text())
for name,repo in [('shunter',shunter),('canary',canary)]:
 dst=root/name
 subprocess.run(['rtk','git','worktree','add','--detach',str(dst),source[name]['head']],cwd=repo,check=True)
 with tarfile.open(ART/(name+'-source.tar.gz')) as tar:tar.extractall(dst,filter='data')
 tracked=subprocess.check_output(['rtk','proxy','git','ls-files','-z'],cwd=dst).decode().split('\0')
 for rel in tracked:
  if rel and not rel.startswith('working-docs/') and rel not in source[name]['source_inputs']:
   p=dst/rel
   if p.is_file():p.unlink()
with tarfile.open(ART/'experiment-overlays.tar.gz') as tar:
 tar.extractall(root,members=[m for m in tar.getmembers() if not m.name.startswith('diagnostic/')],filter='data')
for name,repo in [('shunter',shunter),('canary',canary)]:
 dst=root/'diagnostic'/name
 subprocess.run(['rtk','git','worktree','add','--detach',str(dst),source[name]['head']],cwd=repo,check=True)
 tracked=subprocess.check_output(['rtk','proxy','git','ls-files','-z'],cwd=dst).decode().split('\0')
 for rel in tracked:
  if rel and not rel.startswith('working-docs/') and rel not in frozen[name]:
   p=dst/rel
   if p.is_file():p.unlink()
 for rel in frozen[name]:
  p=dst/rel;p.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(root/name/rel,p)
with tarfile.open(ART/'experiment-overlays.tar.gz') as tar:
 tar.extractall(root,members=[m for m in tar.getmembers() if m.name.startswith('diagnostic/')],filter='data')
fixture=json.loads((ART/'fixture-manifest.json').read_text())
assert hashlib.sha256((ART/'fixtures.tar.gz').read_bytes()).hexdigest()==fixture['archive_sha256']
with tarfile.open(ART/'fixtures.tar.gz') as tar:tar.extractall(root,filter='data')
checked={}
for name,files in frozen.items():
 for rel,info in files.items():assert hashlib.sha256((root/name/rel).read_bytes()).hexdigest()==info['sha256'],(name,rel)
 checked[name]=len(files)
# Only after byte verification, relocate the two module replacement paths.
for prefix in ['', 'diagnostic/']:
 subprocess.run(['rtk','go','mod','edit','-replace=github.com/ponchione/shunter='+str(root/(prefix+'shunter'))],cwd=root/(prefix+'canary'),check=True)
results=root/'results';results.mkdir()
for name in ['run.py','analyze.py','validate.py','contract.md','frozen-matrix.json','frozen-inputs.json']:
 shutil.copy2(ART/name,results/name)
(root/'reconstruction-check.json').write_text(json.dumps({'checked_files':checked,'relocations':['canary/go.mod','diagnostic/canary/go.mod'],'workloads_run':0},indent=2)+'\n')
print('Reconstructed and verified',checked)
print('Optional full repeat: RESPONSE_WORK_ROOT='+str(root)+' rtk proxy python3 '+str(results/'run.py'))
