"""Apply diagnostic-only response timestamps to an isolated reconstructed root."""
from pathlib import Path
import sys
root=Path(sys.argv[1]); runtime=root/'shunter';canary=root/'canary'
p=runtime/'responseprobe';p.mkdir(exist_ok=True)
(p/'probe.go').write_text('''// Package responseprobe is bounded, diagnostic-only instrumentation.
package responseprobe
import (
 "encoding/binary"
 "encoding/json"
 "os"
 "path/filepath"
 "sync/atomic"
 "time"
 "unsafe"
)
const Limit=65536
var origin=time.Now()
type Event struct {Connection [16]byte; Request uint32; Stage string; UnixNS,MonoNS int64}
type slot struct {event Event; ready atomic.Bool}
var events [Limit]slot
var count, failures atomic.Uint64
func ID(b []byte) uint32 {
 if len(b)==0 || len(b)>9 {failures.Add(1);return 0}
 var n uint32
 for _,v:=range b {if v<'0'||v>'9' {failures.Add(1);return 0};n=n*10+uint32(v-'0')}
 return n
}
// FrameID examines only the uncompressed response tag and request identity.
// This experiment negotiates no compression. It never decodes application rows.
func FrameID(frame []byte) uint32 {
 if len(frame)==0 || frame[0]!=6 {return 0}
 if len(frame)<5 {failures.Add(1);return 0}
 n:=int(binary.LittleEndian.Uint32(frame[1:5]))
 if n>len(frame)-5 {failures.Add(1);return 0}
 return ID(frame[5:5+n])
}
func Mark(conn [16]byte,id uint32,stage string,t time.Time) {
 if id==0 {return}
 i:=count.Add(1)-1
 if i>=Limit {return}
 events[i].event=Event{conn,id,stage,t.UnixNano(),t.Sub(origin).Nanoseconds()}
 events[i].ready.Store(true)
}
func Clock() [2]int64 {t:=time.Now();return [2]int64{t.UnixNano(),t.Sub(origin).Nanoseconds()}}
func Dump(role string) error {
 var out []Event
 n:=count.Load()
 for i:=uint64(0);i<min(n,Limit);i++ {if events[i].ready.Load() {out=append(out,events[i].event)}}
 v:=struct {Role string; OriginUnixNS int64; Clock [2]int64; Events []Event; Dropped,CorrelationFailures uint64; BufferBytes uintptr}{role,origin.UnixNano(),Clock(),out,n-min(n,Limit),failures.Load(),unsafe.Sizeof(events)}
 data,err:=json.Marshal(v);if err!=nil{return err}
 return os.WriteFile(filepath.Join(os.Getenv("CANARY_CAPACITY_RESULT_DIR"),"probe-"+role+".json"),append(data,'\\n'),0600)
}
''')
def edit(rel,fn):
 p=runtime/rel;s=p.read_text();s=s.replace('import (','import (\n "github.com/ponchione/shunter/responseprobe"',1);p.write_text(fn(s))
def declared(s):
 a=s.index('func (r *Runtime) handleProtocolDeclaredQuery(');b=s.index('// HandleSubscribeDeclaredView',a)
 part=s[a:b].replace('receipt := time.Now()', 'receipt := time.Now()\n probeID:=responseprobe.ID(messageID)\n responseprobe.Mark(conn.ID,probeID,"handler-entry",receipt)')
 part=part.replace('result, err := r.CallQuery(ctx, name, opts...)','result, err := r.CallQuery(ctx, name, opts...)\n responseprobe.Mark(conn.ID,probeID,"query-ready",time.Now())')
 part=part.replace('rows, err := encodeDeclaredReadRows(result.Rows, result.Columns)','rows, err := encodeDeclaredReadRows(result.Rows, result.Columns)\n responseprobe.Mark(conn.ID,probeID,"rows-encoded",time.Now())')
 return s[:a]+part+s[b:]
edit('declared_read.go',declared)
def sender(s):
 s=s.replace('frame, err := EncodeServerMessageWithLimit(msg, maxBytes)', 'probeID:=uint32(0)\n if m,ok:=msg.(OneOffQueryResponse);ok {probeID=responseprobe.ID(m.MessageID)}\n responseprobe.Mark(connID,probeID,"envelope-start",time.Now())\n frame, err := EncodeServerMessageWithLimit(msg, maxBytes)')
 s=s.replace('_, procedureResponse := msg.(ProcedureResponse)','responseprobe.Mark(connID,probeID,"envelope-ready",time.Now())\n _, procedureResponse := msg.(ProcedureResponse)')
 # Capture enqueue while holding existing queue lock, before the channel send
 # which makes the frame observable to the writer. No auxiliary lock/map.
 s=s.replace('select {\n\tcase <-c.closed:\n\t\treturn outboundSendClosed\n\tcase c.OutboundCh <- frame:', 'responseprobe.Mark(c.ID,responseprobe.FrameID(frame),"enqueue",time.Now())\n select {\n\tcase <-c.closed:\n\t\treturn outboundSendClosed\n\tcase c.OutboundCh <- frame:')
 return s
edit('protocol/sender.go',sender)
def outbound(s):
 s=s.replace('c.releaseOutboundBytes(len(frame))','responseprobe.Mark(c.ID,responseprobe.FrameID(frame),"dequeue",time.Now())\n c.releaseOutboundBytes(len(frame))')
 s=s.replace('return c.ws.Write(writeCtx, websocket.MessageBinary, frame)','id:=responseprobe.FrameID(frame)\n responseprobe.Mark(c.ID,id,"socket-write-start",time.Now())\n err:=c.ws.Write(writeCtx, websocket.MessageBinary, frame)\n responseprobe.Mark(c.ID,id,"socket-write-done",time.Now())\n return err')
 return s
edit('protocol/outbound.go',outbound)
def client(s):
 s=s.replace('"sync/atomic"','"sync/atomic"\n "time"')
 s=s.replace('if err := c.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {','probeID:=uint32(0)\n if m,ok:=msg.(protocol.DeclaredQueryMsg);ok {probeID=responseprobe.ID(m.MessageID)}\n responseprobe.Mark(c.identity.ConnectionID,probeID,"request-write-start",time.Now())\n writeErr:=c.conn.Write(ctx, websocket.MessageBinary, frame)\n responseprobe.Mark(c.identity.ConnectionID,probeID,"request-write-done",time.Now())\n if err := writeErr; err != nil {')
 s=s.replace('typ, frame, err := c.conn.Read(ctx)','typ, frame, err := c.conn.Read(ctx)\n received:=time.Now()\n probeID:=responseprobe.FrameID(frame)\n responseprobe.Mark(c.identity.ConnectionID,probeID,"frame-received",received)')
 s=s.replace('tag, msg, err := protocol.DecodeServerMessage(frame)','tag, msg, err := protocol.DecodeServerMessage(frame)\n responseprobe.Mark(c.identity.ConnectionID,probeID,"outer-decoded",time.Now())')
 s=s.replace('func (c *Client) routeServerMessage(next queuedServerMessage) bool {','func (c *Client) routeServerMessage(next queuedServerMessage) bool {\n probeID:=uint32(0);if m,ok:=next.msg.(protocol.OneOffQueryResponse);ok {probeID=responseprobe.ID(m.MessageID)}\n responseprobe.Mark(c.identity.ConnectionID,probeID,"route-entry",time.Now())')
 s=s.replace('c.pending = append(c.pending, next)','responseprobe.Mark(c.identity.ConnectionID,probeID,"pending-enqueue",time.Now())\n c.pending = append(c.pending, next)')
 s=s.replace('next := c.popPendingLocked()','next := c.popPendingLocked()\n if m,ok:=next.msg.(protocol.OneOffQueryResponse);ok {responseprobe.Mark(c.identity.ConnectionID,responseprobe.ID(m.MessageID),"pending-dequeue",time.Now())}')
 return s
edit('protocolclient/client.go',client)
p=canary/'internal/workflows/canary_capacity_test.go';s=p.read_text().replace('import (','import (\n "github.com/ponchione/shunter/responseprobe"',1)
s=s.replace('rt, err := shunter.Build(app.NewModule(), app.StrictAuthConfig(dir, ""))\n\tcapacityOK(t, err)', 'defer func(){capacityOK(t,responseprobe.Dump("server"))}()\n rt, err := shunter.Build(app.NewModule(), app.StrictAuthConfig(dir, ""))\n\tcapacityOK(t, err)',1)
s=s.replace('mux := http.NewServeMux()','mux := http.NewServeMux()\n mux.HandleFunc("/capacity/clock",func(w http.ResponseWriter,r *http.Request){_ = json.NewEncoder(w).Encode(responseprobe.Clock())})',1)
p.write_text(s)
p=canary/'internal/workflows/canary_arrival_test.go';s=p.read_text().replace('import (','import (\n "github.com/ponchione/shunter/responseprobe"',1)
s=s.replace('b.StopTimer()\n\tshape :=','b.StopTimer()\n defer func(){capacityOK(b,responseprobe.Dump("client"))}()\n shape :=',1)
s=s.replace('capacityOK(b, capacityControl(url, http.MethodPost, "memory", &result.Before))','arrivalClock(b,url,"before")\n capacityOK(b, capacityControl(url, http.MethodPost, "memory", &result.Before))')
s=s.replace('capacityOK(b, capacityControl(url, http.MethodGet, "retained", &result.Retained))','arrivalClock(b,url,"after")\n capacityOK(b, capacityControl(url, http.MethodGet, "retained", &result.Retained))')
s=s.replace('result.OriginUnixNS = start.UnixNano()', 'result.OriginUnixNS = start.UnixNano()\n responseprobe.Mark([16]byte{},1,"phase-origin",start)')
s+='''
func arrivalClock(tb testing.TB,url,phase string) {
 var samples [][3][2]int64
 for range 20 {a:=responseprobe.Clock();var remote [2]int64;capacityOK(tb,capacityControl(url,http.MethodGet,"clock",&remote));samples=append(samples,[3][2]int64{a,remote,responseprobe.Clock()})}
 arrivalSave(tb,"clock-"+phase,samples)
}
'''
p.write_text(s)
