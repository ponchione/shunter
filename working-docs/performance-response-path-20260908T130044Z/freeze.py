from pathlib import Path
import hashlib,json,os,shutil,subprocess,tarfile,datetime
stamp='20260908T130044Z'
root=Path('/home/gernsback/source/shunter')
art=root/'working-docs'/('performance-response-path-'+stamp)
out=Path('/tmp')/('shunter-response-path-'+stamp)
art.mkdir();out.mkdir()
manifest={}
for name,src in [('shunter',root),('canary',Path('/home/gernsback/source/opsboard-canary'))]:
    def git(*args): return subprocess.check_output(['rtk','proxy','git',*args],cwd=src)
    head=git('rev-parse','HEAD').decode().strip()
    (art/(name+'-status.txt')).write_bytes(git('status','--short','--untracked-files=all'))
    (art/(name+'-baseline.patch')).write_bytes(git('diff','--binary','HEAD'))
    names=git('ls-files','--cached','--others','--exclude-standard','-z').decode().split('\0')
    hashes={};inputs=[]
    for rel in sorted(set(names)-{''}):
        p=src/rel
        if not p.is_file() or art in p.parents: continue
        hashes[rel]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'bytes':p.stat().st_size}
        if not rel.startswith('working-docs/') and not rel.startswith('reference/'): inputs.append(rel)
    target=out/name
    subprocess.run(['rtk','git','worktree','add','--detach',str(target),head],cwd=src,check=True)
    with tarfile.open(art/(name+'-source.tar.gz'),'w:gz') as tar:
        for rel in inputs:
            tar.add(src/rel,arcname=rel,recursive=False)
            dst=target/rel;dst.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src/rel,dst)
    # Preserve deletions too.
    for rel in git('ls-files','--deleted','-z').decode().split('\0'):
        if rel and (target/rel).exists(): (target/rel).unlink()
    manifest[name]={'root':str(src),'head':head,'worktree':str(target),'all_preexisting_files':hashes,'source_inputs':inputs}
(art/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
(out/'settings.json').write_text(json.dumps({'art':str(art),'out':str(out)},indent=2)+'\n')
subprocess.run(['rtk','go','mod','edit','-replace=github.com/ponchione/shunter='+str(out/'shunter')],cwd=out/'canary',check=True)
shutil.copy2(__file__,art/'freeze.py')
print(json.dumps({'art':str(art),'out':str(out),'counts':{n:len(v['all_preexisting_files']) for n,v in manifest.items()}}))
