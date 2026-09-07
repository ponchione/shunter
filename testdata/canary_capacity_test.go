package workflows

// This file is copied into the external canary by scripts/measure-canary-capacity.
// It imports the existing app; it does not define a second application fixture.
import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/opsboard-canary/internal/app"
	canaryclient "github.com/ponchione/opsboard-canary/internal/client"
	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/protocolclient"
)

const capacitySeed = 20260907

func capacityOK(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}

func capacityFixture(rows int) string {
	return filepath.Join(os.Getenv("CANARY_CAPACITY_FIXTURES"), strconv.Itoa(rows))
}

func TestCanaryCapacitySeed(t *testing.T) {
	if os.Getenv("CANARY_CAPACITY_FIXTURES") == "" {
		t.Skip("run with scripts/measure-canary-capacity")
	}
	for _, rows := range []int{1024, 8192} {
		t.Run(strconv.Itoa(rows), func(t *testing.T) {
			dir := capacityFixture(rows)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("fixture must be new: %s (%v)", dir, err)
			}
			rt, err := shunter.Build(app.NewModule(), app.StrictAuthConfig(dir, ""))
			capacityOK(t, err)
			t.Cleanup(func() { capacityOK(t, rt.Close()) })
			capacityOK(t, rt.Start(context.Background()))
			_, identity, err := canaryclient.MintStrictToken("alice")
			capacityOK(t, err)
			call := func(name string, args any) {
				data, err := json.Marshal(args)
				capacityOK(t, err)
				result, err := rt.CallReducer(context.Background(), name, data,
					shunter.WithIdentity(identity), shunter.WithAllowAllPermissions(true))
				capacityOK(t, err)
				capacityOK(t, result.Error)
			}
			call(app.ReducerBootstrapDemo, map[string]any{"now_ns": 1000})
			for project := 2; project <= 4; project++ {
				call(app.ReducerCreateProject, map[string]any{"id": project, "slug": fmt.Sprint(project),
					"name": fmt.Sprint(project), "visibility": "public", "now_ns": 1000})
			}
			rng := rand.New(rand.NewPCG(capacitySeed, 0))
			for i := 0; i < rows; i++ {
				title := make([]byte, 1024)
				for j := range title {
					title[j] = byte('a' + rng.IntN(26))
				}
				call(app.ReducerCreateTicket, map[string]any{"id": 1000 + i, "project_id": i%4 + 1,
					"number": 1000 + i, "title": string(title), "priority": "normal",
					"visibility": "public", "now_ns": 1000 + i})
			}
			_, err = rt.CreateSnapshot()
			capacityOK(t, err)
			capacityOK(t, rt.Close())
			t.Logf("seed=%d tickets=%d bytes=%d", capacitySeed, rows+2, capacityDirBytes(t, dir))
		})
	}
}

type capacityMemory struct {
	RSS, Heap, SnapshotRSS, SnapshotHeap uint64
	Samples, SnapshotSamples             int
}

// A separate test process owns the server so client allocations never enter
// server RSS/heap metrics. The control routes only exist on httptest loopback.
func TestCanaryCapacityServer(t *testing.T) {
	dir := os.Getenv("CANARY_CAPACITY_SERVER")
	if dir == "" {
		t.Skip("measurement subprocess only")
	}
	rt, err := shunter.Build(app.NewModule(), app.StrictAuthConfig(dir, ""))
	capacityOK(t, err)
	capacityOK(t, rt.Start(context.Background()))
	defer func() { capacityOK(t, rt.Close()) }()
	var mu sync.Mutex
	var memory capacityMemory
	var snapshot atomic.Bool
	sample := func() {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		stat, err := os.ReadFile("/proc/self/statm")
		capacityOK(t, err)
		fields := strings.Fields(string(stat))
		if len(fields) < 2 {
			t.Fatal("invalid /proc/self/statm")
		}
		pages, err := strconv.ParseUint(fields[1], 10, 64)
		capacityOK(t, err)
		rss := pages * uint64(os.Getpagesize())
		mu.Lock()
		defer mu.Unlock()
		memory.RSS = max(memory.RSS, rss)
		memory.Heap = max(memory.Heap, mem.HeapAlloc)
		memory.Samples++
		if snapshot.Load() {
			memory.SnapshotRSS = max(memory.SnapshotRSS, rss)
			memory.SnapshotHeap = max(memory.SnapshotHeap, mem.HeapAlloc)
			memory.SnapshotSamples++
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var sampler sync.WaitGroup
	sampler.Go(func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sample()
			case <-ctx.Done():
				return
			}
		}
	})
	defer func() { cancel(); sampler.Wait() }()
	mux := http.NewServeMux()
	mux.HandleFunc("/capacity/memory", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Method == http.MethodPost {
			memory = capacityMemory{}
		}
		mu.Unlock()
		sample()
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(memory)
	})
	mux.HandleFunc("/capacity/snapshot", func(w http.ResponseWriter, r *http.Request) {
		snapshot.Store(true)
		sample()
		_, err := rt.CreateSnapshot()
		sample()
		snapshot.Store(false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.Handle("/", rt.HTTPHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	fmt.Println("CAPACITY_URL=" + srv.URL)
	_, err = io.Copy(io.Discard, os.Stdin)
	capacityOK(t, err)
}

func capacityServer(tb testing.TB, dir string) (string, func()) {
	tb.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCanaryCapacityServer$", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), "CANARY_CAPACITY_SERVER="+dir)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	capacityOK(tb, err)
	stdout, err := cmd.StdoutPipe()
	capacityOK(tb, err)
	capacityOK(tb, cmd.Start())
	var once sync.Once
	stop := func() {
		once.Do(func() {
			capacityOK(tb, stdin.Close())
			capacityOK(tb, cmd.Wait())
		})
	}
	tb.Cleanup(stop)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if url, ok := strings.CutPrefix(scanner.Text(), "CAPACITY_URL="); ok {
			go func() { _, _ = io.Copy(io.Discard, stdout) }()
			return url, stop
		}
	}
	tb.Fatalf("server did not start: %v", scanner.Err())
	return "", stop
}

func capacityControl(url, method, path string, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url+"/capacity/"+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s: %s", path, resp.Status, body)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func capacityDial(tb testing.TB, url string) *protocolclient.Client {
	tb.Helper()
	token, _, err := canaryclient.MintStrictToken("alice", app.PermTicketWrite)
	capacityOK(tb, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := protocolclient.Dial(ctx, protocolclient.Options{URL: url + "/subscribe", Token: token})
	capacityOK(tb, err)
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	return c
}

func capacityDirBytes(tb testing.TB, dir string) int64 {
	tb.Helper()
	var size int64
	capacityOK(tb, filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	}))
	return size
}

func capacityPercentile(values []float64, p float64) float64 {
	slices.Sort(values)
	return values[int(math.Ceil(p*float64(len(values))))-1]
}

func BenchmarkCanaryCapacity(b *testing.B) {
	if os.Getenv("CANARY_CAPACITY_FIXTURES") == "" {
		b.Skip("run with scripts/measure-canary-capacity")
	}
	for _, rows := range []int{1024, 8192} {
		for _, clients := range []int{8, 32} {
			for _, skew := range []bool{false, true} {
				shape := "balanced50w"
				if skew {
					shape = "hot75pct80w"
				}
				b.Run(fmt.Sprintf("rows%d/clients%d/%s", rows, clients, shape), func(b *testing.B) {
					if b.N != 1 {
						b.Fatal("use -benchtime=1x; each operation is a sustained workload")
					}
					capacityRun(b, rows, clients, skew)
				})
			}
		}
	}
}

func capacityRun(b *testing.B, rows, clients int, skew bool) {
	b.StopTimer()
	duration := 10 * time.Second
	if value := os.Getenv("CANARY_CAPACITY_DURATION"); value != "" {
		var err error
		duration, err = time.ParseDuration(value)
		capacityOK(b, err)
		if duration < time.Second {
			b.Fatal("duration must be at least 1s")
		}
	}
	dir := filepath.Join(b.TempDir(), "data")
	capacityOK(b, shunter.RestoreDataDir(capacityFixture(rows), dir))
	url, stop := capacityServer(b, dir)
	contract := app.NewModule()
	// Build solely to obtain the app's table schema, outside measured time and
	// outside the server process. No runtime is started here.
	meta, err := shunter.Build(contract, shunter.Config{})
	capacityOK(b, err)
	table, err := canaryclient.TableSchema(meta.ExportContract(), "tickets")
	capacityOK(b, err)
	capacityOK(b, meta.Close())
	connections := make([]*protocolclient.Client, clients)
	projects := make([]int, clients)
	fanout := [5]int{}
	for i := range connections {
		project := i%4 + 1
		if skew {
			project = 1
			if i >= clients*3/4 {
				project = 2 + (i-clients*3/4)%3
			}
		}
		projects[i] = project
		fanout[project]++
		c := capacityDial(b, url)
		connections[i] = c
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		capacityOK(b, c.Send(ctx, protocol.SubscribeSingleMsg{RequestID: 1, QueryID: 1,
			QueryString: fmt.Sprintf("SELECT * FROM tickets WHERE project_id = %d", project)}))
		tag, message, err := c.Read(ctx)
		cancel()
		capacityOK(b, err)
		if tag != protocol.TagSubscribeSingleApplied {
			b.Fatalf("subscribe: %T %+v", message, message)
		}
		initial, err := canaryclient.DecodeRows(message.(protocol.SubscribeSingleApplied).Rows, table)
		capacityOK(b, err)
		want := rows / 4
		if project == 1 {
			want += 2
		}
		if len(initial) != want {
			b.Fatalf("initial rows=%d want=%d", len(initial), want)
		}
	}
	var before capacityMemory
	capacityOK(b, capacityControl(url, http.MethodPost, "memory", &before))
	var delivered atomic.Int64
	var expected atomic.Int64
	var readers, workers sync.WaitGroup
	errors := make(chan error, 2*clients+2)
	readCtx, cancelRead := context.WithCancel(context.Background())
	defer cancelRead()
	latencies := make([][]float64, clients)
	callerLatencies := make([][]float64, clients)
	writes := make([][]float64, clients)
	reads := make([][]float64, clients)
	record := func(updates []protocol.SubscriptionUpdate, values *[]float64) error {
		for _, change := range updates {
			inserted, err := canaryclient.DecodeRows(change.Inserts, table)
			if err != nil || change.TableName != "tickets" || change.QueryID != 1 || len(inserted) != 1 {
				return fmt.Errorf("invalid delta: %s query=%d rows=%d: %v", change.TableName, change.QueryID, len(inserted), err)
			}
			*values = append(*values, float64(time.Now().UnixNano()-inserted[0][9].AsInt64())/1e6)
			delivered.Add(1)
		}
		return nil
	}
	for i, c := range connections {
		readers.Go(func() {
			for {
				_, msg, err := c.Read(readCtx)
				if err != nil {
					if readCtx.Err() == nil {
						errors <- err
					}
					return
				}
				update, ok := msg.(protocol.TransactionUpdateLight)
				if !ok {
					errors <- fmt.Errorf("unexpected async message: %T", msg)
					return
				}
				if err := record(update.Update, &latencies[i]); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	b.ResetTimer()
	b.StartTimer()
	start := time.Now()
	deadline := start.Add(duration)
	var snapshots []float64
	var snapshotWorker sync.WaitGroup
	snapshotWorker.Go(func() {
		for _, fraction := range []int{1, 2} {
			time.Sleep(time.Until(start.Add(duration * time.Duration(fraction) / 3)))
			started := time.Now()
			if err := capacityControl(url, http.MethodPost, "snapshot", nil); err != nil {
				errors <- err
				return
			}
			snapshots = append(snapshots, float64(time.Since(started))/1e6)
		}
	})
	for i, c := range connections {
		workers.Go(func() {
			rng := rand.New(rand.NewPCG(capacitySeed, uint64(i+1)))
			ticket := 1000 + i*4 + projects[i] - 1
			writePercent := 50
			if skew {
				writePercent = 80
			}
			closed := false
			for time.Now().Before(deadline) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				started := time.Now()
				var err error
				if rng.IntN(100) < writePercent {
					name := app.ReducerCloseTicket
					if closed {
						name = app.ReducerReopenTicket
					}
					args := []byte(fmt.Sprintf(`{"ticket_id":%d,"now_ns":%d}`, ticket, started.UnixNano()))
					var result protocol.TransactionUpdate
					result, err = c.CallReducer(ctx, name, args)
					if err == nil {
						if committed, ok := result.Status.(protocol.StatusCommitted); !ok {
							err = fmt.Errorf("reducer status: %+v", result.Status)
						} else {
							err = record(committed.Update, &callerLatencies[i])
						}
					}
					if err == nil {
						expected.Add(int64(fanout[projects[i]]))
						writes[i] = append(writes[i], float64(time.Since(started))/1e6)
						closed = !closed
					}
				} else {
					var result protocol.OneOffQueryResponse
					result, err = c.DeclaredQuery(ctx, app.QueryTicket100Detail)
					if err == nil && result.Error != nil {
						err = fmt.Errorf("read: %s", *result.Error)
					}
					if err == nil {
						decoded, decodeErr := canaryclient.DecodeOneOffRows(result, "tickets", table)
						err = decodeErr
						if err == nil && (len(decoded) != 1 || decoded[0][0].AsUint64() != 100) {
							err = fmt.Errorf("detail query did not return ticket 100")
						}
					}
					if err == nil {
						reads[i] = append(reads[i], float64(time.Since(started))/1e6)
					}
				}
				cancel()
				if err != nil {
					errors <- err
					return
				}
			}
		})
	}
	workers.Wait()
	elapsed := time.Since(start).Seconds()
	snapshotWorker.Wait()
	drainDeadline := time.Now().Add(30 * time.Second)
	for delivered.Load() < expected.Load() && time.Now().Before(drainDeadline) && len(errors) == 0 {
		time.Sleep(time.Millisecond)
	}
	b.StopTimer()
	cancelRead()
	readers.Wait()
	close(errors)
	for err := range errors {
		capacityOK(b, err)
	}
	if delivered.Load() != expected.Load() || expected.Load() == 0 || len(snapshots) != 2 {
		b.Fatalf("delivered=%d expected=%d snapshots=%d", delivered.Load(), expected.Load(), len(snapshots))
	}
	var memory capacityMemory
	capacityOK(b, capacityControl(url, http.MethodGet, "memory", &memory))
	for i := range latencies {
		latencies[i] = append(latencies[i], callerLatencies[i]...)
	}
	for i, group := range [][][]float64{writes, reads, latencies} {
		var values []float64
		for _, part := range group {
			values = append(values, part...)
		}
		if len(values) == 0 {
			b.Fatal("empty measurement")
		}
		name := []string{"reducer", "read", "delta"}[i]
		b.ReportMetric(float64(len(values))/elapsed, name+"/s")
		b.ReportMetric(capacityPercentile(values, .50), name+"-p50-ms")
		b.ReportMetric(capacityPercentile(values, .95), name+"-p95-ms")
		b.ReportMetric(float64(len(values)), name+"-count")
	}
	b.ReportMetric(float64(before.RSS)/(1<<20), "rss-start-MiB")
	b.ReportMetric(float64(before.Heap)/(1<<20), "heap-start-MiB")
	b.ReportMetric(float64(memory.RSS)/(1<<20), "rss-peak-MiB")
	b.ReportMetric(float64(memory.Heap)/(1<<20), "heap-peak-MiB")
	b.ReportMetric(float64(memory.SnapshotRSS)/(1<<20), "snapshot-rss-MiB")
	b.ReportMetric(float64(memory.SnapshotHeap)/(1<<20), "snapshot-heap-MiB")
	b.ReportMetric(float64(memory.Samples), "memory-samples")
	b.ReportMetric(float64(memory.SnapshotSamples), "snapshot-memory-samples")
	b.ReportMetric(capacityPercentile(snapshots, .50), "snapshot-p50-ms")
	b.ReportMetric(capacityPercentile(snapshots, 1), "snapshot-max-ms")
	b.ReportMetric(elapsed, "workload-s")
	for _, c := range connections {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		capacityOK(b, c.Close(ctx))
		cancel()
	}
	stop()
}

func BenchmarkCanaryCapacityBackupRestore(b *testing.B) {
	if os.Getenv("CANARY_CAPACITY_FIXTURES") == "" {
		b.Skip("run with scripts/measure-canary-capacity")
	}
	for _, rows := range []int{1024, 8192} {
		b.Run(strconv.Itoa(rows), func(b *testing.B) {
			if b.N != 1 {
				b.Fatal("use -benchtime=1x")
			}
			b.StopTimer()
			root := b.TempDir()
			source := capacityFixture(rows)
			size := capacityDirBytes(b, source)
			backup, restored := filepath.Join(root, "backup"), filepath.Join(root, "restored")
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			start := time.Now()
			capacityOK(b, shunter.BackupDataDir(source, backup))
			backupDuration := time.Since(start)
			start = time.Now()
			capacityOK(b, shunter.RestoreDataDir(backup, restored))
			restoreDuration := time.Since(start)
			b.StopTimer()
			b.ReportMetric(float64(size), "fixture-bytes")
			b.ReportMetric(float64(backupDuration)/1e6, "backup-ms")
			b.ReportMetric(float64(restoreDuration)/1e6, "restore-ms")
			if capacityDirBytes(b, restored) != size {
				b.Fatal("restored size differs")
			}
			rt, err := shunter.Build(app.NewModule(), shunter.Config{DataDir: restored})
			capacityOK(b, err)
			defer func() { capacityOK(b, rt.Close()) }()
			capacityOK(b, rt.Start(context.Background()))
			capacityOK(b, rt.Read(context.Background(), func(view shunter.LocalReadView) error {
				if view.RowCount(app.TableTickets) != rows+2 || view.RowCount(app.TableAuditLog) != rows+7 {
					return fmt.Errorf("restored ticket/audit counts differ")
				}
				return nil
			}))
		})
	}
}
