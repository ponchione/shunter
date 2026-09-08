package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/opsboard-canary/internal/app"
	cc "github.com/ponchione/opsboard-canary/internal/client"
	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/protocolclient"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Receipt is the benchmark reader receipt after outer protocol decoding and
// internal routing. Frame receipt is available only in the diagnostic overlay.
type arrivalOp struct {
	Client, Ordinal                                                int
	Kind                                                           string
	DueMS, DispatchMS, DoneMS, HostMS                              float64
	Stamp                                                          int64
	Connection                                                     string
	RequestID                                                      uint32
	WriteDoneMS, ReceiptMS, HandoffMS, DecodeStartMS, DecodeDoneMS float64
	Dispatched, Finished, Responded, Timeout, Rejected             bool
	Error                                                          string `json:",omitempty"`
}
type arrivalDelivery struct {
	Ticket uint64
	Stamp  int64
	MS     float64
}
type arrivalReply struct {
	message any
	receipt time.Time
}

type arrivalResult struct {
	Mode, Shape                               string
	OriginUnixNS                              int64
	DeadlineMS                                float64
	Report                                    map[string]float64
	Rate, Rows, PerClient                     int
	OfferedSeconds, WorkSeconds, DrainSeconds float64
	Operations                                [][]arrivalOp
	Deliveries                                [][]arrivalDelivery
	Snapshots                                 [][2]float64
	Before, Memory                            capacityMemory
	Retained                                  uint64
	Errors                                    []string
	ChoiceSHA256                              string
	Checked                                   bool
}

func arrivalInt(name string, fallback int) int {
	if s := os.Getenv(name); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			panic(name)
		}
		return n
	}
	return fallback
}
func arrivalHostMS(micros int64) float64 { return float64(micros) / 1000 }

func arrivalDue(op, client, rate int, burst bool) time.Duration {
	n := op * 32
	if !burst {
		n += client
	}
	return time.Duration(n) * time.Second / time.Duration(rate)
}
func arrivalChoices(client, n int) []bool {
	r := rand.New(rand.NewPCG(capacitySeed, uint64(client+1)))
	out := make([]bool, n)
	for i := range out {
		out[i] = r.IntN(100) < 50
	}
	return out
}
func arrivalSave(tb testing.TB, name string, value any) {
	tb.Helper()
	data, err := json.Marshal(value)
	capacityOK(tb, err)
	if dir := os.Getenv("CANARY_CAPACITY_RESULT_DIR"); dir != "" {
		capacityOK(tb, os.WriteFile(filepath.Join(dir, name+".json"), append(data, '\n'), 0600))
	}
}
func arrivalSchema(tb testing.TB) *schema.TableSchema {
	tb.Helper()
	rt, err := shunter.Build(app.NewModule(), shunter.Config{})
	capacityOK(tb, err)
	table, err := cc.TableSchema(rt.ExportContract(), "tickets")
	capacityOK(tb, err)
	capacityOK(tb, rt.Close())
	return table
}
func arrivalSubscribe(tb testing.TB, c *protocolclient.Client, project, count int, table *schema.TableSchema) []types.ProductValue {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	capacityOK(tb, c.Send(ctx, protocol.SubscribeSingleMsg{RequestID: 1, QueryID: 1, QueryString: fmt.Sprintf("SELECT * FROM tickets WHERE project_id = %d", project)}))
	_, msg, err := c.Read(ctx)
	capacityOK(tb, err)
	a, ok := msg.(protocol.SubscribeSingleApplied)
	if !ok {
		tb.Fatalf("initial: %T %+v", msg, msg)
	}
	rows, err := cc.DecodeRows(a.Rows, table)
	capacityOK(tb, err)
	if len(rows) != count {
		tb.Fatalf("initial count %d != %d", len(rows), count)
	}
	return rows
}
func arrivalRecord(updates []protocol.SubscriptionUpdate, table *schema.TableSchema, out *[]arrivalDelivery) error {
	for _, u := range updates {
		rows, err := cc.DecodeRows(u.Inserts, table)
		if err != nil {
			return err
		}
		deleted, err := cc.DecodeRows(u.Deletes, table)
		if err != nil {
			return err
		}
		if u.QueryID != 1 || u.TableName != "tickets" || len(rows) != 1 || len(deleted) != 1 || rows[0][0].AsUint64() != deleted[0][0].AsUint64() {
			return fmt.Errorf("invalid delta %+v inserts=%d deletes=%d", u, len(rows), len(deleted))
		}
		stamp := rows[0][9].AsInt64()
		*out = append(*out, arrivalDelivery{rows[0][0].AsUint64(), stamp, float64(time.Now().UnixNano()-stamp) / 1e6})
	}
	return nil
}

func TestCanaryArrivalSchedule(t *testing.T) {
	for _, rate := range []int{100, 1000, 4000, 8000} {
		for i := range 32 {
			for j := range 100 {
				even, burst := arrivalDue(j, i, rate, false), arrivalDue(j, i, rate, true)
				if even < burst || even-burst >= 32*time.Second/time.Duration(rate) {
					t.Fatal("shape boundary")
				}
				if j > 0 && burst <= arrivalDue(j-1, i, rate, true) {
					t.Fatal("order")
				}
			}
		}
	}
	// Responses cannot change the predetermined offered offsets or choices.
	if !slices.Equal(arrivalChoices(3, 100), arrivalChoices(3, 100)) || arrivalDue(99, 31, 100, false) != 31990*time.Millisecond {
		t.Fatal("schedule")
	}
}

func BenchmarkCanaryScheduledArrival(b *testing.B) {
	if os.Getenv("CANARY_CAPACITY_MODE") != "scheduled-arrival" {
		b.Skip("run with scripts/measure-canary-capacity")
	}
	if b.N != 1 {
		b.Fatal("-benchtime=1x")
	}
	b.StopTimer()
	shape := os.Getenv("CANARY_CAPACITY_SHAPE")
	rate := arrivalInt("CANARY_CAPACITY_RATE", 1000)
	if shape != "even" && shape != "burst" {
		b.Fatal("shape must be even or burst")
	}
	n := arrivalInt("CANARY_CAPACITY_PER_CLIENT", 100)
	if n > 10000 || rate > 1000000 || float64(32*n)/float64(rate) > 60 {
		b.Fatal("schedule exceeds bounded 10,000 operations/client, 1,000,000/s or 60s window")
	}
	rows := 8192
	table := arrivalSchema(b)
	dir := filepath.Join(b.TempDir(), "data")
	capacityOK(b, shunter.RestoreDataDir(capacityFixture(rows), dir))
	url, stop := capacityServer(b, dir)
	result := arrivalResult{Mode: "scheduled-arrival", Shape: shape, Rate: rate, Rows: rows, PerClient: n, OfferedSeconds: float64(32*n) / float64(rate), Operations: make([][]arrivalOp, 32), Deliveries: make([][]arrivalDelivery, 32)}
	defer func() { result.Report = arrivalReport(result); arrivalSave(b, "result", result) }()
	cs := make([]*protocolclient.Client, 32)
	replies := make([]chan arrivalReply, 32)
	for i := range cs {
		cs[i] = capacityDial(b, url)
		count := rows / 4
		if i%4 == 0 {
			count += 2
		}
		arrivalSubscribe(b, cs[i], i%4+1, count, table)
		replies[i] = make(chan arrivalReply, 1)
	}
	// Freeze all intended operations before the phase origin; waits never alter them.
	choices := sha256.New()
	for i := range cs {
		result.Operations[i] = make([]arrivalOp, n)
		for j, write := range arrivalChoices(i, n) {
			kind := "read"
			if write {
				kind = "fast"
			}
			fmt.Fprintf(choices, "%d:%t;", i, write)
			result.Operations[i][j] = arrivalOp{Client: i, Ordinal: j, Kind: kind, DueMS: float64(arrivalDue(j, i, rate, shape == "burst")) / 1e6, Connection: fmt.Sprintf("%x", cs[i].IdentityToken().ConnectionID), RequestID: uint32(j + 100)}
		}
	}
	result.ChoiceSHA256 = fmt.Sprintf("%x", choices.Sum(nil))
	// Ticket 100 is untouched by writers. Compare every column outside row decoding.
	seed := filepath.Join(b.TempDir(), "expected")
	capacityOK(b, shunter.RestoreDataDir(capacityFixture(rows), seed))
	var wantDetail []types.ProductValue
	for _, row := range finalCapture(b, seed)["tickets"].Rows {
		if row[0].AsUint64() == 100 {
			wantDetail = append(wantDetail, row)
		}
	}
	if len(wantDetail) != 1 {
		b.Fatal("expected detail fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var readers, workers, background sync.WaitGroup
	var delivered atomic.Int64
	errs := make(chan error, 100)
	sent := make([][]capacityEvent, 32)
	readTimes := make([][]float64, 32)
	for i, c := range cs {
		readers.Go(func() {
			for {
				_, msg, err := c.Read(ctx)
				received := time.Now()
				if err != nil {
					if ctx.Err() == nil {
						errs <- fmt.Errorf("reader %d: %w", i, err)
					}
					return
				}
				switch m := msg.(type) {
				case protocol.TransactionUpdateLight:
					before := len(result.Deliveries[i])
					if err := arrivalRecord(m.Update, table, &result.Deliveries[i]); err != nil {
						errs <- err
						return
					}
					delivered.Add(int64(len(result.Deliveries[i]) - before))
				case protocol.TransactionUpdate:
					if status, ok := m.Status.(protocol.StatusCommitted); ok {
						before := len(result.Deliveries[i])
						if err := arrivalRecord(status.Update, table, &result.Deliveries[i]); err != nil {
							errs <- err
							return
						}
						delivered.Add(int64(len(result.Deliveries[i]) - before))
					}
					select {
					case replies[i] <- arrivalReply{m, received}:
					case <-ctx.Done():
						return
					}
				case protocol.OneOffQueryResponse:
					select {
					case replies[i] <- arrivalReply{m, received}:
					case <-ctx.Done():
						return
					}
				default:
					errs <- fmt.Errorf("unexpected client %d %T", i, msg)
					return
				}
			}
		})
	}
	capacityOK(b, capacityControl(url, http.MethodPost, "memory", &result.Before))
	b.ResetTimer()
	b.StartTimer()
	start := time.Now()
	result.OriginUnixNS = start.UnixNano()
	result.DeadlineMS = 90000
	deadlineTimer := time.AfterFunc(90*time.Second, cancel)
	defer deadlineTimer.Stop()
	background.Go(func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var memory capacityMemory
				if err := capacityControl(url, http.MethodGet, "memory", &memory); err != nil {
					errs <- err
					cancel()
					return
				}
				if memory.ResourceStop {
					errs <- fmt.Errorf("resource stop: server RSS >1GiB")
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	})
	var snapshotWG sync.WaitGroup
	snapshotWG.Go(func() {
		for _, f := range []float64{1.0 / 3, 2.0 / 3} {
			due := start.Add(time.Duration(result.OfferedSeconds * f * 1e9))
			select {
			case <-time.After(time.Until(due)):
			case <-ctx.Done():
				return
			}
			a := float64(time.Since(start)) / 1e6
			if err := capacityControl(url, http.MethodPost, "snapshot", nil); err != nil {
				errs <- err
				return
			}
			result.Snapshots = append(result.Snapshots, [2]float64{a, float64(time.Since(start)) / 1e6})
		}
	})
	for i, c := range cs {
		workers.Go(func() {
			ticket := 1000 + i*4 + i%4
			writeOrdinal := 0
			for j := range result.Operations[i] {
				op := &result.Operations[i][j]
				write := op.Kind == "fast"
				due := arrivalDue(j, i, rate, shape == "burst")
				select {
				case <-time.After(time.Until(start.Add(due))):
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					return
				}
				now := time.Now()
				op.Dispatched = true
				op.DispatchMS = float64(now.Sub(start)) / 1e6
				op.Stamp = now.UnixNano()
				reqctx, reqcancel := context.WithTimeout(ctx, 30*time.Second)
				id := uint32(j + 100)
				var err error
				name := ""
				flags := protocol.CallReducerFlagsFullUpdate
				if write {
					name = app.ReducerCloseTicket
					if writeOrdinal%2 == 1 {
						name = app.ReducerReopenTicket
					}
					op.Kind = "fast"
					args := []byte(fmt.Sprintf(`{"ticket_id":%d,"now_ns":%d}`, ticket, op.Stamp))
					err = c.Send(reqctx, protocol.CallReducerMsg{ReducerName: name, RequestID: id, Flags: flags, Args: args})
				} else {
					err = c.Send(reqctx, protocol.DeclaredQueryMsg{Name: app.QueryTicket100Detail, MessageID: []byte(strconv.Itoa(int(id)))})
				}
				op.WriteDoneMS = float64(time.Since(start)) / 1e6
				if err == nil {
					var msg any
					select {
					case reply := <-replies[i]:
						msg = reply.message
						op.Responded = true
						op.ReceiptMS = float64(reply.receipt.Sub(start)) / 1e6
						op.HandoffMS = float64(time.Since(start)) / 1e6
					case <-reqctx.Done():
						err = reqctx.Err()
					}
					if err == nil {
						if write {
							m, ok := msg.(protocol.TransactionUpdate)
							if !ok {
								err = fmt.Errorf("write reply %T", msg)
							} else if m.ReducerCall.RequestID != id || m.ReducerCall.ReducerName != name {
								err = fmt.Errorf("write correlation")
							} else if _, ok := m.Status.(protocol.StatusCommitted); !ok {
								op.Rejected = true
								err = fmt.Errorf("write status %+v", m.Status)
							} else {
								op.HostMS = arrivalHostMS(int64(m.TotalHostExecutionDuration))
								sent[i] = append(sent[i], capacityEvent{uint64(ticket), op.Stamp})
								writeOrdinal++
							}
						} else {
							m, ok := msg.(protocol.OneOffQueryResponse)
							if !ok {
								err = fmt.Errorf("read reply %T", msg)
							} else if string(m.MessageID) != strconv.Itoa(int(id)) {
								err = fmt.Errorf("read correlation")
							} else if m.Error != nil {
								op.Rejected = true
								err = fmt.Errorf("read: %s", *m.Error)
							} else {
								var decoded []types.ProductValue
								op.DecodeStartMS = float64(time.Since(start)) / 1e6
								decoded, err = cc.DecodeOneOffRows(m, "tickets", table)
								op.DecodeDoneMS = float64(time.Since(start)) / 1e6
								if err == nil && !reflect.DeepEqual(decoded, wantDetail) {
									err = fmt.Errorf("detail result")
								}
								op.HostMS = arrivalHostMS(int64(m.TotalHostExecutionDuration))
							}
						}
					}
				}
				op.DoneMS = float64(time.Since(start)) / 1e6
				op.Finished = true
				op.Timeout = errors.Is(err, context.DeadlineExceeded) || errors.Is(err, protocolclient.ErrTimeout) || (ctx.Err() != nil && !op.Responded && op.DoneMS >= result.DeadlineMS)
				reqcancel()
				if err != nil {
					op.Error = err.Error()
					errs <- fmt.Errorf("client %d op %d: %w", i, j, err)
					return
				}
				if !write {
					readTimes[i] = append(readTimes[i], op.DoneMS-op.DispatchMS)
				}
			}
		})
	}
	workers.Wait()
	result.WorkSeconds = max(time.Since(start).Seconds(), result.OfferedSeconds)
	snapshotWG.Wait()
	totalWrites := 0
	for _, s := range sent {
		totalWrites += len(s)
	}
	expected := int64(totalWrites * 8)
	for delivered.Load() < expected && ctx.Err() == nil && len(errs) == 0 {
		time.Sleep(time.Millisecond)
	}
	if len(result.Snapshots) != 2 {
		errs <- fmt.Errorf("snapshot count %d/2", len(result.Snapshots))
	}
	if ctx.Err() != nil {
		errs <- fmt.Errorf("completion/drain deadline or resource cancellation: %w", ctx.Err())
	}
	result.DrainSeconds = time.Since(start).Seconds()
	b.StopTimer()
	cancel()
	readers.Wait()
	background.Wait()
	for len(errs) > 0 {
		result.Errors = append(result.Errors, (<-errs).Error())
	}
	capacityOK(b, capacityControl(url, http.MethodGet, "memory", &result.Memory))
	if result.Memory.ResourceStop {
		result.Errors = append(result.Errors, "resource stop: server RSS >1GiB")
	}
	capacityOK(b, capacityControl(url, http.MethodGet, "retained", &result.Retained))
	for i := range cs {
		if len(result.Operations[i]) != n {
			result.Errors = append(result.Errors, fmt.Sprintf("client %d completed %d/%d", i, len(result.Operations[i]), n))
		}
		observed := map[uint64][]capacityEvent{}
		for _, d := range result.Deliveries[i] {
			observed[d.Ticket] = append(observed[d.Ticket], capacityEvent{d.Ticket, d.Stamp})
		}
		for j, writes := range sent {
			if j%4 != i%4 || len(writes) == 0 {
				continue
			}
			if !slices.Equal(observed[writes[0].ticket], writes) {
				result.Errors = append(result.Errors, fmt.Sprintf("ordered drain client %d writer %d", i, j))
			}
			delete(observed, writes[0].ticket)
		}
		if len(observed) != 0 {
			result.Errors = append(result.Errors, "unexpected healthy delivery")
		}
	}
	for i := 4; i < len(cs); i++ {
		a, c := result.Deliveries[i%4], result.Deliveries[i]
		if len(a) != len(c) {
			result.Errors = append(result.Errors, "complete stream length")
			continue
		}
		for j := range a {
			if a[j].Ticket != c[j].Ticket || a[j].Stamp != c[j].Stamp {
				result.Errors = append(result.Errors, "complete stream order")
				break
			}
		}
	}
	if delivered.Load() != expected {
		result.Errors = append(result.Errors, fmt.Sprintf("delivery %d/%d", delivered.Load(), expected))
	}
	for _, c := range cs {
		ctx, done := context.WithTimeout(context.Background(), time.Second)
		_ = c.Close(ctx)
		done()
	}
	stop()
	defer func() {
		if b.Failed() {
			if out := os.Getenv("CANARY_CAPACITY_RESULT_DIR"); out != "" {
				if _, err := os.Stat(filepath.Join(out, "failed-data")); os.IsNotExist(err) {
					capacityOK(b, shunter.BackupDataDir(dir, filepath.Join(out, "failed-data")))
				}
			}
		}
	}()
	for _, ops := range result.Operations {
		for _, o := range ops {
			if !o.Finished || !o.Responded {
				result.Errors = append(result.Errors, "unfinished scheduled operation")
				break
			}
		}
	}
	if len(result.Errors) > 0 {
		if out := os.Getenv("CANARY_CAPACITY_RESULT_DIR"); out != "" {
			capacityOK(b, shunter.BackupDataDir(dir, filepath.Join(out, "failed-data")))
			audits := finalCapture(b, dir)["audit_log"].Rows
			type outcome struct {
				Client, Ordinal int
				Accepted        bool
			}
			var outcomes []outcome
			for i, ops := range result.Operations {
				for _, o := range ops {
					if o.Kind == "fast" && o.Dispatched {
						accepted := false
						for _, a := range audits {
							if a[4].AsUint64() == uint64(1000+i*4+i%4) && a[6].AsInt64() == o.Stamp {
								accepted = true
							}
						}
						outcomes = append(outcomes, outcome{i, o.Ordinal, accepted})
					}
				}
			}
			arrivalSave(b, "write-reconciliation", outcomes)
		}
	}
	arrivalSave(b, "result", result)
	if len(result.Errors) != 0 {
		b.Fatalf("workload errors (raw preserved): %v", result.Errors)
	}
	capacityVerifyComplete(b, dir, capacityFixture(rows), sent, readTimes)
	result.Checked = true
	result.Report = arrivalReport(result)
	for name, value := range result.Report {
		b.ReportMetric(value, name)
	}
}
