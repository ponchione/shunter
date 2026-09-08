from pathlib import Path
from collections import defaultdict,Counter
import json,math,statistics
ART=Path(__file__).resolve().parent
matrix=json.loads((ART/'frozen-matrix.json').read_text())['runs']
def percentile(values,p):return sorted(values)[math.ceil(p*len(values))-1]
def dist(v):return {'n':len(v),'min':min(v),'p50':percentile(v,.5),'p95':percentile(v,.95),'p99':percentile(v,.99),'max':max(v),'sum':sum(v)}
client=['request-write-start','request-write-done','frame-received','outer-decoded','route-entry','pending-enqueue','pending-dequeue']
server=['handler-entry','query-ready','rows-encoded','envelope-start','envelope-ready','enqueue','dequeue','socket-write-start','socket-write-done']
sequence=['dispatch','request-write-start','handler-entry','query-ready','rows-encoded','envelope-start','envelope-ready','enqueue','dequeue','socket-write-start','frame-received','outer-decoded','route-entry','pending-enqueue','pending-dequeue','benchmark-receipt','handoff','decode-start','decode-done','validation-done']
summary={};correlations={};checks={};overheads={}
for cell in matrix:
 name=cell['name'];p=ART/name;r=json.loads((p/'result.json').read_text());m=r['Report']
 ops=[o for os in r['Operations'] for o in os];reads=[o for o in ops if o['Kind']=='read'];writes=[o for o in ops if o['Kind']=='fast']
 assert len(ops)==3200 and len(reads)==1563 and len(writes)==1637
 assert all(o['Finished'] and o['Responded'] and not o.get('Error') for o in ops)
 # Independent raw percentile and count checks; don't trust Report alone.
 for metric,field in [('read-dispatch','DispatchMS'),('read-scheduled','DueMS')]:
  for label,q in [('p95',.95),('p99',.99)]:assert abs(m[f'{metric}-{label}-ms']-percentile([o['DoneMS']-o[field] for o in reads],q))<1e-9
 assert r['Checked'] and not r['Errors'] and len(r['Snapshots'])==2
 for i,os in enumerate(r['Operations']):
  assert [o['Ordinal'] for o in os]==list(range(100))
  for j,o in enumerate(os):
   due=(j*32+(0 if cell['shape']=='burst' else i))*1000/cell['rate']
   assert abs(o['DueMS']-due)<1e-8 and o['DispatchMS']>=due
   if j:assert o['DispatchMS']>=os[j-1]['DoneMS']
   assert o['RequestID']==j+100
 # Complete per-project streams plus exact per-writer membership and ordering.
 deliveries=r['Deliveries'];assert sum(map(len,deliveries))==1637*8
 for i,stream in enumerate(deliveries):
  key=lambda d:(d['Ticket'],d['Stamp'])
  assert list(map(key,stream))==list(map(key,deliveries[i%4]))
  expected={(1000+j*4+j%4,o['Stamp']) for j,os in enumerate(r['Operations']) if j%4==i%4 for o in os if o['Kind']=='fast'}
  assert len(stream)==len(expected) and set(map(key,stream))==expected
 raw=(p/'raw.txt').read_text();assert 'FINAL_CHECK ' in raw and 'PASS' in raw
 checks[name]={'operations':len(ops),'reads':len(reads),'accepted_writes':len(writes),'ordered_deliveries':sum(map(len,deliveries)),'choices':r['ChoiceSHA256'],'exact_recovery_check':True}
 summary[name]=m
 if cell['mode']!='diagnostic':continue
 probes={role:json.loads((p/f'probe-{role}.json').read_text()) for role in ['client','server']}
 bykey=defaultdict(dict);failures=[];clock={};phase=None
 for role,probe in probes.items():
  assert probe['Dropped']==probe['CorrelationFailures']==0
  skew=[]
  for e in probe['Events']:
   skew.append(e['UnixNS']-probe['OriginUnixNS']-e['MonoNS'])
   if e['Stage']=='phase-origin':phase=e;continue
   key=(bytes(e['Connection']).hex(),e['Request'])
   if e['Stage'] in bykey[key]:failures.append([key,'duplicate',e['Stage']])
   bykey[key][e['Stage']]=e
  clock[role]={'wall_monotonic_skew_ns':dist(skew),'buffer_bytes':probe['BufferBytes'],'event_count':len(probe['Events']),'dropped':probe['Dropped'],'parser_failures':probe['CorrelationFailures']}
 assert phase is not None and phase['UnixNS']==r['OriginUnixNS']
 brackets=[]
 for phase_name in ['before','after']:
  vals=json.loads((p/f'clock-{phase_name}.json').read_text())
  # Server event occurs between local request/response: server-local offset lies
  # in [server - local_after, server - local_before]. Intersection retains bound.
  low=max(b[0]-c[0] for a,b,c in vals);high=min(b[0]-a[0] for a,b,c in vals)
  clock[phase_name]={'offset_compatible_interval_ns':[low,high],'minimum_rtt_ns':min(c[1]-a[1] for a,b,c in vals)}
  assert low<=0<=high
 timelines=[]; numerical=[]
 uncertainty_ms=sum(max(abs(clock[role]['wall_monotonic_skew_ns']['min']),abs(clock[role]['wall_monotonic_skew_ns']['max'])) for role in ['client','server'])/1e6
 for o in reads:
  key=(o['Connection'],o['RequestID']);stages=bykey.get(key,{})
  missing=set(client+server)-set(stages)
  if missing:failures.append([key,'missing',sorted(missing)]);continue
  times={k:dict(v) for k,v in stages.items()}
  for label,field in [('intended','DueMS'),('dispatch','DispatchMS'),('request-write-return','WriteDoneMS'),('benchmark-receipt','ReceiptMS'),('handoff','HandoffMS'),('decode-start','DecodeStartMS'),('decode-done','DecodeDoneMS'),('validation-done','DoneMS')]:
   offset=round(o[field]*1e6)
   times[label]={'UnixNS':phase['UnixNS']+offset,'MonoNS':phase['MonoNS']+offset}
  components={}
  for a,b in zip(sequence,sequence[1:]):
   same=(a not in server)==(b not in server)
   basis='MonoNS' if same else 'UnixNS'
   value=(times[b][basis]-times[a][basis])/1e6
   components[a+' -> '+b]=value
   if value<0: numerical.append([key,'negative-disjoint',a,b,value])
   if value < -uncertainty_ms-.000001:failures.append([key,'negative-beyond-clock-uncertainty',a,b,value])
  additional={
   'server-socket-write':(times['socket-write-done']['MonoNS']-times['socket-write-start']['MonoNS'])/1e6,
   'client-request-write':(times['request-write-done']['MonoNS']-times['request-write-start']['MonoNS'])/1e6,
   'write-done-to-client-receipt-signed':(times['frame-received']['UnixNS']-times['socket-write-done']['UnixNS'])/1e6,
  }
  total=o['DoneMS']-o['DispatchMS'];closure_error=sum(components.values())-total
  assert abs(closure_error)<=2*uncertainty_ms+.000001,(key,closure_error,uncertainty_ms)
  timelines.append({'client':o['Client'],'ordinal':o['Ordinal'],'connection':o['Connection'],'request':o['RequestID'],'dispatch_ms':total,'closure_error_ms':closure_error,'scheduled_ms':o['DoneMS']-o['DueMS'],'timestamps':{stage:{'from_dispatch_ms':(t['UnixNS']-times['dispatch']['UnixNS'])/1e6,**t} for stage,t in times.items()},'components_ms':components,'overlapping_ms':additional})
 assert len(bykey)==1563
 p99=percentile([o['DoneMS']-o['DispatchMS'] for o in reads],.99);p95=percentile([o['DoneMS']-o['DispatchMS'] for o in reads],.95)
 cohorts={};representatives=[]
 for label,threshold in [('all',0),('p95',p95),('p99',p99)]:
  selected=sorted((t for t in timelines if t['dispatch_ms']>=threshold),key=lambda t:t['dispatch_ms'])
  groups={k:dist([t['components_ms'][k] for t in selected]) for k in selected[0]['components_ms']}
  additional={k:dist([t['overlapping_ms'][k] for t in selected]) for k in selected[0]['overlapping_ms']}
  dominant=Counter(max(t['components_ms'],key=t['components_ms'].get) for t in selected)
  cohorts[label]={'n':len(selected),'threshold_ms':threshold,'dispatch':dist([t['dispatch_ms'] for t in selected]),'components_ms':groups,'overlapping_ms':additional,'dominant_component_counts':dict(dominant),'component_time_shares':{k:d['sum']/sum(t['dispatch_ms'] for t in selected) for k,d in groups.items()}}
  if label=='p99':representatives=[selected[0],selected[len(selected)//2],selected[-1]]
 correlations[name]={'failures':failures,'numerical_uncertainties':numerical,'observed_mapping_uncertainty_ms':uncertainty_ms,'closure_residual_ms':dist([t['closure_error_ms'] for t in timelines]),'reads_matched':len(timelines),'clock':clock,'cohorts':cohorts,'representative_timelines':representatives}
 (p/'timelines.json').write_text(json.dumps(timelines,separators=(',',':'))+'\n')
 assert not failures,failures
assert len(set(v['choices'] for v in checks.values()))==1
for rate,shape in [(1000,'even'),(4000,'burst'),(4000,'even'),(1000,'burst')]:
 a=summary[f'{rate}-{shape}-ordinary-a'];b=summary[f'{rate}-{shape}-ordinary-b'];d=summary[f'{rate}-{shape}-diagnostic']
 keys=['read-dispatch-p95-ms','read-dispatch-p99-ms','read-scheduled-p95-ms','read-scheduled-p99-ms','completion-ops/s','peak-outstanding','server-TotalAlloc-B','server-retained-heap-B','server-sampled-rss-peak-B']
 overheads[f'{rate}-{shape}']={k:{'ordinary':[a[k],b[k]],'diagnostic':d[k],'vs_ordinary_percent':[100*(d[k]/a[k]-1),100*(d[k]/b[k]-1)]} for k in keys}
for name,v in [('metrics.json',summary),('correlation.json',correlations),('overhead.json',overheads),('analysis-check.json',checks)]:
 (ART/name).write_text(json.dumps(v,indent=2)+'\n')
lines=['# Per-run results (milliseconds)','','| Run | Dispatch p95 | p99 | Scheduled p95 | p99 | Completed/s | Peak queued | In flight | Outstanding | Window backlog |','|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
for name,m in summary.items():lines.append('| '+name+' | '+' | '.join(f'{m[k]:.3f}' for k in ['read-dispatch-p95-ms','read-dispatch-p99-ms','read-scheduled-p95-ms','read-scheduled-p99-ms','completion-ops/s','peak-awaiting-dispatch','peak-inflight','peak-outstanding','window-outstanding'])+' |')
(ART/'metrics.md').write_text('\n'.join(lines)+'\n')
for name,d in correlations.items():print(name,'p99',d['cohorts']['p99']['threshold_ms'],'cohort',d['cohorts']['p99']['n'],'dominant',d['cohorts']['p99']['dominant_component_counts'])
print('Verified 12 runs, 38400 operations, 157152 ordered deliveries, 6252 fully correlated diagnostic reads.')
