package workflows

import (
	"math"
	"slices"
	"testing"
	"time"
	"unsafe"
)

// All times are milliseconds from one phase origin. Undispatched operations
// still contribute arrival events; no response wait can erase offered work.
func arrivalReport(r arrivalResult) map[string]float64 {
	m := map[string]float64{"offered-window-s": r.OfferedSeconds, "completion-window-s": r.WorkSeconds, "drain-window-s": r.DrainSeconds}
	type event struct {
		ms             float64
		queued, flight int
	}
	var events []event
	groups := map[string][]float64{}
	lastDispatch, lastCompletion := r.OfferedSeconds*1000, r.OfferedSeconds*1000
	for _, ops := range r.Operations {
		m["client-operation-buffer-B"] += float64(cap(ops)) * float64(unsafe.Sizeof(arrivalOp{}))
		for _, o := range ops {
			m["offered-count"]++
			events = append(events, event{o.DueMS, 1, 0})
			for name, cutoff := range map[string]float64{"window": r.OfferedSeconds * 1000, "deadline": r.DeadlineMS} {
				if o.DueMS > cutoff {
					continue
				}
				if !o.Dispatched || o.DispatchMS > cutoff {
					m[name+"-awaiting-dispatch"]++
				} else if !o.Finished || o.DoneMS > cutoff {
					m[name+"-inflight"]++
				}
			}
			if !o.Dispatched {
				m["undispatched-count"]++
				continue
			}
			m["dispatch-count"]++
			lastDispatch = max(lastDispatch, o.DispatchMS)
			events = append(events, event{o.DispatchMS, -1, 1})
			groups["scheduling-lateness"] = append(groups["scheduling-lateness"], o.DispatchMS-o.DueMS)
			if o.DispatchMS <= r.OfferedSeconds*1000 {
				m["dispatch-in-window-count"]++
			} else {
				m["dispatch-after-window-count"]++
			}
			if o.Finished {
				events = append(events, event{o.DoneMS, 0, -1})
				m["finished-count"]++
			}
			if o.Error != "" {
				m["error-count"]++
			}
			if o.Timeout {
				m["timeout-count"]++
			}
			if o.Rejected {
				m["rejection-count"]++
			}
			if o.Kind == "fast" && !o.Responded && o.Error != "" {
				m["unknown-write-count"]++
			}
			if !o.Responded {
				continue
			}
			m["completion-count"]++
			lastCompletion = max(lastCompletion, o.DoneMS)
			if o.DoneMS <= r.OfferedSeconds*1000 {
				m["completion-in-window-count"]++
			} else {
				m["completion-after-window-count"]++
			}
			if o.Error != "" {
				continue
			}
			m["success-count"]++
			if o.Kind == "read" {
				for name, v := range map[string]float64{
					"read-dispatch": o.DoneMS - o.DispatchMS, "read-scheduled": o.DoneMS - o.DueMS,
					"read-response": o.ReceiptMS - o.DispatchMS, "read-handoff": o.HandoffMS - o.ReceiptMS,
					"read-row-decode": o.DecodeDoneMS - o.DecodeStartMS, "read-validation": o.DoneMS - o.DecodeDoneMS, "read-host": o.HostMS,
				} {
					groups[name] = append(groups[name], v)
				}
			}
		}
	}
	// Equal-time arrivals precede terminal waits, then dispatch, so a burst's
	// full intended cohort is visible. Counts at a cutoff use inclusive timestamps.
	slices.SortStableFunc(events, func(a, b event) int {
		if a.ms < b.ms {
			return -1
		}
		if a.ms > b.ms {
			return 1
		}
		return b.queued - a.queued
	})
	queued, flight := 0, 0
	for _, e := range events {
		queued += e.queued
		flight += e.flight
		m["peak-awaiting-dispatch"] = max(m["peak-awaiting-dispatch"], float64(queued))
		m["peak-inflight"] = max(m["peak-inflight"], float64(flight))
		m["peak-outstanding"] = max(m["peak-outstanding"], float64(queued+flight))
	}
	for _, name := range []string{"window", "deadline"} {
		m[name+"-outstanding"] = m[name+"-awaiting-dispatch"] + m[name+"-inflight"]
	}
	m["offered-ops/s"] = m["offered-count"] / r.OfferedSeconds
	m["dispatch-ops/s"] = m["dispatch-count"] / (lastDispatch / 1000)
	m["completion-ops/s"] = m["completion-count"] / (lastCompletion / 1000)
	m["dispatch-in-window-ops/s"] = m["dispatch-in-window-count"] / r.OfferedSeconds
	m["completion-in-window-ops/s"] = m["completion-in-window-count"] / r.OfferedSeconds
	for name, values := range groups {
		for label, p := range map[string]float64{"p95": .95, "p99": .99} {
			m[name+"-"+label+"-ms"] = capacityPercentile(values, p)
		}
	}
	for _, name := range []string{"error-count", "timeout-count", "rejection-count", "unknown-write-count", "undispatched-count", "window-awaiting-dispatch", "window-inflight", "deadline-awaiting-dispatch", "deadline-inflight", "completion-after-window-count", "dispatch-after-window-count"} {
		if _, ok := m[name]; !ok {
			m[name] = 0
		}
	}
	// A failed memory capture must not wrap an unsigned allocation delta.
	m["server-TotalAlloc-B"] = -1
	if r.Memory.TotalAlloc >= r.Before.TotalAlloc {
		m["server-TotalAlloc-B"] = float64(r.Memory.TotalAlloc - r.Before.TotalAlloc)
	}
	m["server-retained-heap-B"] = float64(r.Retained)
	m["server-sampled-rss-peak-B"] = float64(r.Memory.RSS)
	m["server-sampled-heap-peak-B"] = float64(r.Memory.Heap)
	m["server-memory-samples"] = float64(r.Memory.Samples)
	for _, ds := range r.Deliveries {
		m["client-delivery-buffer-B"] += float64(cap(ds)) * float64(unsafe.Sizeof(arrivalDelivery{}))
	}
	return m
}

func TestCanaryArrivalAccounting(t *testing.T) {
	if arrivalHostMS(1234) != 1.234 {
		t.Fatal("wire microseconds to milliseconds")
	}
	// A slow response leaves the next two intended arrivals queued, including one
	// never dispatched. The timeout has no reply and is an unknown write outcome.
	r := arrivalResult{OfferedSeconds: .003, WorkSeconds: .012, DeadlineMS: 10, Operations: [][]arrivalOp{{
		{DueMS: 0, Dispatched: true, DispatchMS: 0, Finished: true, Responded: true, DoneMS: 8, Kind: "read", ReceiptMS: 6, HandoffMS: 6.5, DecodeStartMS: 6.5, DecodeDoneMS: 7, HostMS: .1},
		{DueMS: 1, Dispatched: true, DispatchMS: 8, Finished: true, DoneMS: 12, Kind: "fast", Error: "deadline", Timeout: true},
		{DueMS: 2},
	}}}
	m := arrivalReport(r)
	for k, want := range map[string]float64{"offered-count": 3, "dispatch-count": 2, "completion-count": 1, "timeout-count": 1, "unknown-write-count": 1, "rejection-count": 0, "undispatched-count": 1, "peak-awaiting-dispatch": 2, "peak-outstanding": 3, "peak-inflight": 1, "window-awaiting-dispatch": 2, "window-inflight": 1, "deadline-awaiting-dispatch": 1, "deadline-inflight": 1, "completion-after-window-count": 1, "offered-ops/s": 1000, "completion-ops/s": 125, "read-dispatch-p99-ms": 8, "read-row-decode-p95-ms": .5, "read-validation-p99-ms": 1} {
		if math.Abs(m[k]-want) > 1e-9 {
			t.Errorf("%s=%g want %g", k, m[k], want)
		}
	}
	r.Before.TotalAlloc = 100
	if arrivalReport(r)["server-TotalAlloc-B"] != -1 {
		t.Fatal("missing memory capture wrapped allocation delta")
	}
	// Frozen offsets survive an arbitrarily late preceding completion.
	if arrivalDue(2, 0, 1000, false) != 64*time.Millisecond {
		t.Fatal("schedule drift")
	}
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(100-i) / 1000
	}
	if capacityPercentile(values, .95) != .095 || capacityPercentile(values, .99) != .099 {
		t.Fatal("nearest-rank percentile or milliseconds")
	}
}
