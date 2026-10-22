package persistence

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

// Interleaved ordinary-traffic measurement for the soak's browse/write gate.
// Periods alternate loaded (live clients running) and no-live states in a fixed
// ABBA order. Adjacent periods 2k and 2k+1 form a pair holding one period of
// each state, so linear drift (for example recommendation growth) cancels within
// the design and the pair is the bootstrap's outer resampling unit.
const (
	soakLoaded = "loaded"
	soakNoLive = "no-live"
	// soakBlock is the circular block length, in consecutive requests of one
	// operation (about 1.5–2.5 s of probe time), for the inner resample.
	soakBlock     = 20
	soakBootstrap = 10000
	soakSeed      = 20261021
)

// soakOrder is ABBA×4: eight pairs for the bootstrap's outer stage.
var soakOrder = slices.Repeat([]string{soakLoaded, soakNoLive, soakNoLive, soakLoaded}, 4)

// soakSample is one retained probe observation.
type soakSample struct {
	Phase     string // baseline (pre-soak), soak (sustained) or interleaved
	Period    int    // 1-based interleaved period; 0 otherwise
	Label     string
	State     string
	Operation string // browse or write
	Started   time.Time
	Latency   time.Duration
	Error     string
}

var soakCSVHeader = []string{"phase", "period", "label", "state", "operation", "started_utc", "latency_ns", "error"}

func soakSamples(phase string, period int, label, state string, r loadProbeResult) []soakSample {
	out := []soakSample{}
	for operation, timed := range map[string][]probeSample{"browse": r.BrowseTimed, "write": r.WriteTimed} {
		for _, s := range timed {
			out = append(out, soakSample{phase, period, label, state, operation, s.Started.UTC(), s.Latency, s.Error})
		}
	}
	slices.SortStableFunc(out, func(a, b soakSample) int { return a.Started.Compare(b.Started) })
	return out
}

// writeSoakCSV writes samples and returns the file's SHA-256.
func writeSoakCSV(path string, samples []soakSample) (string, error) {
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	w := csv.NewWriter(file)
	_ = w.Write(soakCSVHeader)
	for _, s := range samples {
		_ = w.Write([]string{s.Phase, strconv.Itoa(s.Period), s.Label, s.State, s.Operation, s.Started.Format(time.RFC3339Nano), strconv.FormatInt(int64(s.Latency), 10), s.Error})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// soakEvent is a retained diagnostic observation: an r0 count-frame arrival
// bucket (value = frames) or a CPU rate over the preceding interval (cores).
type soakEvent struct {
	Kind  string
	At    time.Time
	Value float64
}

func writeSoakEvents(path string, events []soakEvent) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(file)
	_ = w.Write([]string{"kind", "at_utc", "value"})
	for _, e := range events {
		_ = w.Write([]string{e.Kind, e.At.UTC().Format(time.RFC3339Nano), strconv.FormatFloat(e.Value, 'f', -1, 64)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func readSoakCSV(path string) ([]soakSample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || !slices.Equal(rows[0], soakCSVHeader) {
		return nil, fmt.Errorf("unexpected soak sample header")
	}
	out := []soakSample{}
	for _, row := range rows[1:] {
		period, err1 := strconv.Atoi(row[1])
		started, err2 := time.Parse(time.RFC3339Nano, row[5])
		latency, err3 := strconv.ParseInt(row[6], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("invalid soak sample row %v", row)
		}
		out = append(out, soakSample{row[0], period, row[2], row[3], row[4], started, time.Duration(latency), row[7]})
	}
	return out, nil
}

// soakPeriod holds one interleaved period's successful latencies per
// operation, in request order, plus its error count.
type soakPeriod struct {
	index  int
	state  string
	op     map[string][]time.Duration
	errors map[string]int
}

func soakPeriods(samples []soakSample) ([]soakPeriod, error) {
	byIndex := map[int]*soakPeriod{}
	for _, s := range samples {
		if s.Phase != "interleaved" {
			continue
		}
		p := byIndex[s.Period]
		if p == nil {
			p = &soakPeriod{index: s.Period, state: s.State, op: map[string][]time.Duration{}, errors: map[string]int{}}
			byIndex[s.Period] = p
		}
		if p.state != s.State {
			return nil, fmt.Errorf("period %d has mixed states", s.Period)
		}
		if s.Error != "" {
			p.errors[s.Operation]++
			continue
		}
		p.op[s.Operation] = append(p.op[s.Operation], s.Latency)
	}
	periods := []soakPeriod{}
	for i := 1; i <= len(byIndex); i++ {
		p, ok := byIndex[i]
		if !ok {
			return nil, fmt.Errorf("missing interleaved period %d", i)
		}
		periods = append(periods, *p)
	}
	if len(periods) == 0 || len(periods)%2 != 0 {
		return nil, fmt.Errorf("%d interleaved periods; need complete pairs", len(periods))
	}
	for k := 0; k < len(periods); k += 2 {
		if periods[k].state == periods[k+1].state {
			return nil, fmt.Errorf("periods %d and %d do not pair loaded with no-live", k+1, k+2)
		}
	}
	return periods, nil
}

// soakRatio is the pooled loaded/no-live p95 ratio for one operation with two
// percentile-bootstrap intervals: paired periods plus blocks (primary), and
// blocks within fixed periods (the within-period sampling floor).
type soakRatio struct {
	operation             string
	loaded, noLive        time.Duration
	loadedN, noLiveN      int
	ratio                 float64
	pairedLow, pairedHigh float64
	blockLow, blockHigh   float64
}

func p95Of(values []time.Duration, buffer []time.Duration) time.Duration {
	buffer = append(buffer[:0], values...)
	slices.Sort(buffer)
	return buffer[(len(buffer)*95-1)/100]
}

// blockResample appends a circular block bootstrap resample of values with the
// same length: random starting offsets, soakBlock consecutive requests each.
func blockResample(r *rand.Rand, dst, values []time.Duration) []time.Duration {
	n := len(values)
	for len(dst) < n {
		start := r.IntN(n)
		for j := 0; j < soakBlock && len(dst) < n; j++ {
			dst = append(dst, values[(start+j)%n])
		}
	}
	return dst
}

// analyzeSoak computes each operation's ratio and seeded bootstrap intervals.
// Outer unit: an adjacent loaded/no-live pair, drawn with replacement (so each
// replicate keeps equal loaded and no-live periods). Inner unit: a block of
// consecutive requests within each drawn period. Requests are never resampled
// individually, because neighbouring requests share load and GC state.
func analyzeSoak(periods []soakPeriod, operation string) soakRatio {
	pooled := func(state string, pick func(p soakPeriod) []time.Duration, chosen []int) []time.Duration {
		out := []time.Duration{}
		for _, k := range chosen {
			for _, p := range periods[2*k : 2*k+2] {
				if p.state == state {
					out = append(out, pick(p)...)
				}
			}
		}
		return out
	}
	pairs := len(periods) / 2
	all := make([]int, pairs)
	for k := range all {
		all[k] = k
	}
	raw := func(p soakPeriod) []time.Duration { return p.op[operation] }
	loaded, noLive := pooled(soakLoaded, raw, all), pooled(soakNoLive, raw, all)
	result := soakRatio{operation: operation, loadedN: len(loaded), noLiveN: len(noLive)}
	if len(loaded) == 0 || len(noLive) == 0 {
		return result
	}
	var buffer []time.Duration
	result.loaded, result.noLive = p95Of(loaded, buffer), p95Of(noLive, buffer)
	result.ratio = float64(result.loaded) / float64(result.noLive)
	interval := func(seed uint64, resamplePairs bool) (float64, float64) {
		r := rand.New(rand.NewPCG(soakSeed, seed))
		ratios := make([]float64, 0, soakBootstrap)
		chosen := make([]int, pairs)
		// A pair drawn twice receives independent block resamples.
		draw := func(p soakPeriod) []time.Duration { return blockResample(r, nil, p.op[operation]) }
		for range soakBootstrap {
			for k := range chosen {
				chosen[k] = k
				if resamplePairs {
					chosen[k] = r.IntN(pairs)
				}
			}
			a, b := pooled(soakLoaded, draw, chosen), pooled(soakNoLive, draw, chosen)
			ratios = append(ratios, float64(p95Of(a, buffer))/float64(p95Of(b, buffer)))
		}
		slices.Sort(ratios)
		return ratios[soakBootstrap*25/1000], ratios[soakBootstrap*975/1000-1]
	}
	seed := uint64(1)
	if operation == "write" {
		seed = 2
	}
	result.pairedLow, result.pairedHigh = interval(seed, true)
	result.blockLow, result.blockHigh = interval(seed+10, false)
	return result
}

// soakVerdict separates the observed gate result from statistical evidence.
func soakVerdict(r soakRatio, limit float64) (gate, evidence string) {
	gate = "pass"
	if r.ratio > limit {
		gate = "fail"
	}
	switch {
	case r.pairedHigh <= limit:
		evidence = "interval below limit"
	case r.pairedLow > limit:
		evidence = "interval above limit"
	default:
		evidence = "inconclusive: interval crosses limit"
	}
	return gate, evidence
}

func logSoakAnalysis(t *testing.T, samples []soakSample) []soakRatio {
	t.Helper()
	periods, err := soakPeriods(samples)
	if err != nil {
		t.Fatal(err)
	}
	var buffer []time.Duration
	for _, p := range periods {
		t.Logf("interleaved period %d %-7s browse n=%d errors=%d p95=%s | write n=%d errors=%d p95=%s", p.index, p.state,
			len(p.op["browse"]), p.errors["browse"], p95Of(p.op["browse"], buffer), len(p.op["write"]), p.errors["write"], p95Of(p.op["write"], buffer))
	}
	for k := 0; k < len(periods); k += 2 {
		a, b := periods[k], periods[k+1]
		if a.state != soakLoaded {
			a, b = b, a
		}
		t.Logf("pair %d (periods %d/%d): browse ratio=%.3f write ratio=%.3f", k/2+1, periods[k].index, periods[k+1].index,
			float64(p95Of(a.op["browse"], buffer))/float64(p95Of(b.op["browse"], buffer)), float64(p95Of(a.op["write"], buffer))/float64(p95Of(b.op["write"], buffer)))
	}
	results := []soakRatio{}
	for _, operation := range []string{"browse", "write"} {
		r := analyzeSoak(periods, operation)
		gate, evidence := soakVerdict(r, 1.10)
		t.Logf("pooled %s p95: loaded=%s (n=%d) no-live=%s (n=%d) ratio=%.3f gate(≤1.10)=%s; 95%% CI paired-period+block [%.3f, %.3f] (%s); block-only [%.3f, %.3f]",
			operation, r.loaded, r.loadedN, r.noLive, r.noLiveN, r.ratio, gate, r.pairedLow, r.pairedHigh, evidence, r.blockLow, r.blockHigh)
		results = append(results, r)
	}
	t.Logf("bootstrap: %d replicates, seed %d, block %d requests, %d pairs", soakBootstrap, soakSeed, soakBlock, len(periods)/2)
	return results
}

// Reanalyzes retained soak samples: LIVE_ACTIVITY_SOAK_ANALYZE=path.csv.
func TestActivitySoakSampleAnalysis(t *testing.T) {
	path := os.Getenv("LIVE_ACTIVITY_SOAK_ANALYZE")
	if path == "" {
		t.Skip("set LIVE_ACTIVITY_SOAK_ANALYZE to a retained soak sample CSV")
	}
	samples, err := readSoakCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	logSoakAnalysis(t, samples)
}

func TestActivitySoakBootstrap(t *testing.T) {
	// Synthetic ABBA×2 periods with autocorrelated latencies; loaded periods
	// are exactly 1.5× their pair's no-live sequence.
	r := rand.New(rand.NewPCG(1, 2))
	samples := []soakSample{}
	base := []time.Duration{}
	for i, state := range soakOrder {
		if i%2 == 0 {
			base = base[:0]
			level := 5 * time.Millisecond
			for j := range 60 {
				if j%25 == 0 {
					level = time.Duration(4+r.IntN(4)) * time.Millisecond
				}
				base = append(base, level+time.Duration(r.IntN(1000))*time.Microsecond)
			}
		}
		for _, latency := range base {
			if state == soakLoaded {
				latency = latency * 3 / 2
			}
			samples = append(samples, soakSample{Phase: "interleaved", Period: i + 1, State: state, Operation: "write", Latency: latency})
		}
	}
	samples = append(samples, soakSample{Phase: "interleaved", Period: 2, State: soakNoLive, Operation: "write", Error: "timeout"})
	periods, err := soakPeriods(samples)
	if err != nil {
		t.Fatal(err)
	}
	if periods[1].errors["write"] != 1 || len(periods[1].op["write"]) != 60 {
		t.Fatal("errors must be counted, not sampled")
	}
	first, second := analyzeSoak(periods, "write"), analyzeSoak(periods, "write")
	if first != second {
		t.Fatal("bootstrap is not reproducible")
	}
	if first.pairedLow > first.ratio || first.pairedHigh < first.ratio || first.pairedLow < 1.2 || first.pairedHigh > 1.8 {
		t.Fatalf("implausible interval %+v", first)
	}
	if gate, evidence := soakVerdict(soakRatio{ratio: 1.05, pairedLow: 0.95, pairedHigh: 1.15}, 1.10); gate != "pass" || evidence != "inconclusive: interval crosses limit" {
		t.Fatal("a crossing interval must be inconclusive, separately from the gate", gate, evidence)
	}
	// Period 1 now pairs no-live with no-live.
	for i := range samples {
		if samples[i].Period == 1 {
			samples[i].State = soakNoLive
		}
	}
	if _, err := soakPeriods(samples); err == nil {
		t.Fatal("unpaired periods accepted")
	}
}
