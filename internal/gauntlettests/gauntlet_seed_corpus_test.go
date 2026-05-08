package shunter_test

import (
	"fmt"
	"testing"
)

// Keep gauntlet regression seeds named so new corpus entries are visible and replayable.
var (
	gauntletCoreRegressionSeeds                = []int64{1, 17, 20260427}
	gauntletScheduledRegressionSeeds           = []int64{41, 20260428}
	gauntletRuntimeScheduledRegressionSeeds    = []int64{73, 20260429}
	gauntletMixedProtocolClientRegressionSeeds = []int64{5, 29, 20260427}
	gauntletConcurrentReadShortSoakSeeds       = []int64{20260430, 20260501}
	gauntletProtocolRestartLoopShortSoakSeeds  = []int64{20260502, 20260503}
	gauntletFixedTraceCorpusRegressionSeeds    = []int64{20260508, 20260509, 20260510}
)

func TestRuntimeGauntletFixedSeedCorpusReplay(t *testing.T) {
	const steps = 28

	for _, seed := range gauntletFixedTraceCorpusRegressionSeeds {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			trace := buildGauntletTrace(seed, steps)
			assertGauntletTraceDeterministic(t, seed, steps, trace)

			dataDir := t.TempDir()
			rt := buildGauntletRuntime(t, dataDir)
			model := gauntletModel{players: map[uint64]string{}}
			runGauntletTrace(t, rt, &model, trace[:steps/2], 0, fmt.Sprintf("seed %d fixed corpus prefix", seed))
			assertGauntletSubscribeInitialMatchesOneOff(t, rt, model, fmt.Sprintf("seed %d fixed corpus prefix", seed))
			if err := rt.Close(); err != nil {
				t.Fatalf("seed %d fixed corpus Close before restart returned error: %v", seed, err)
			}

			rt = buildGauntletRuntime(t, dataDir)
			defer rt.Close()
			assertGauntletReadMatchesModel(t, rt, model, fmt.Sprintf("seed %d fixed corpus after restart", seed))
			runGauntletTrace(t, rt, &model, trace[steps/2:], steps/2, fmt.Sprintf("seed %d fixed corpus suffix", seed))
			assertGauntletSubscribeInitialMatchesOneOff(t, rt, model, fmt.Sprintf("seed %d fixed corpus final", seed))
		})
	}
}

func assertGauntletTraceDeterministic(t *testing.T, seed int64, steps int, trace []gauntletOp) {
	t.Helper()
	replayed := buildGauntletTrace(seed, steps)
	if len(replayed) != len(trace) {
		t.Fatalf("seed %d replayed trace length = %d, want %d", seed, len(replayed), len(trace))
	}
	for i := range trace {
		if trace[i].kind != replayed[i].kind ||
			trace[i].reducer != replayed[i].reducer ||
			trace[i].args != replayed[i].args ||
			trace[i].wantStatus != replayed[i].wantStatus {
			t.Fatalf("seed %d step %d replayed op = %s/%s/%s/%v, want %s/%s/%s/%v",
				seed,
				i,
				replayed[i].kind,
				replayed[i].reducer,
				replayed[i].args,
				replayed[i].wantStatus,
				trace[i].kind,
				trace[i].reducer,
				trace[i].args,
				trace[i].wantStatus,
			)
		}
	}
}
