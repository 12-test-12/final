package alert_test

import (
	"testing"
	"time"

	"github.com/BobcGn/final/backend/internal/alert"
	"github.com/BobcGn/final/backend/internal/domain"
)

// start is a fixed instant so every case is reproducible.
var start = time.Date(2026, 9, 18, 11, 20, 0, 0, time.UTC)

// sample builds a telemetry sample at a given offset from start.
func sample(deviceID, bootID string, sequence uint32, offset time.Duration, adc int, temperature float64) domain.Telemetry {
	at := start.Add(offset)
	return domain.Telemetry{
		DeviceID:         deviceID,
		BootID:           bootID,
		Sequence:         sequence,
		Timestamp:        &at,
		UptimeMs:         uint64(offset / time.Millisecond),
		TemperatureC:     temperature,
		HumidityRh:       60,
		GasAdcRaw:        adc,
		GasAdcFiltered:   adc,
		GasCalibrated:    false,
		AlarmCauses:      []domain.AlarmCause{},
		Network:          domain.NetworkOnline,
		ThresholdVersion: 1,
		ReceivedAt:       at,
	}
}

// newEngine builds an engine with the documented defaults.
func newEngine(t *testing.T) *alert.Engine {
	t.Helper()

	engine, err := alert.NewEngine(alert.DefaultConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return engine
}

// feed evaluates a series and returns the last decision.
func feed(engine *alert.Engine, samples []domain.Telemetry, state domain.AlertState, active *domain.AlertEvent) alert.Decision {
	var decision alert.Decision
	for _, current := range samples {
		decision = engine.Evaluate(alert.Input{Sample: current, CurrentState: state, ActiveAlert: active})
	}
	return decision
}

// rising builds a series whose temperature climbs at ratePerMinute and whose
// filtered gas climbs by gasRise in total across the sample count. Samples are
// five seconds apart, matching the device report interval.
func rising(count int, gasRise int, ratePerMinute float64) []domain.Telemetry {
	return risingFrom("boot1", 0, 0, count, gasRise, ratePerMinute)
}

// risingFrom builds a series that starts at a given boot, sequence and time
// offset, so that two series can be fed to one engine without colliding on the
// (bootId, sequence) dedup key.
func risingFrom(bootID string, sequenceBase uint32, offsetBase time.Duration, count int, gasRise int, ratePerMinute float64) []domain.Telemetry {
	samples := make([]domain.Telemetry, 0, count)
	for index := 0; index < count; index++ {
		offset := offsetBase + time.Duration(index)*5*time.Second
		minutes := offset.Minutes()
		adc := 1000
		if index >= count-1 {
			// The rise is concentrated in the newest sample so the baseline stays
			// stable regardless of the window size.
			adc += gasRise
		}
		samples = append(samples, sample("MCU001", bootID, sequenceBase+uint32(index), offset, adc, 25+minutes*ratePerMinute))
	}
	return samples
}

// flat builds a series of identical, quiet samples, used to exercise recovery.
func flat(bootID string, sequenceBase uint32, offsetBase time.Duration, count int) []domain.Telemetry {
	return risingFrom(bootID, sequenceBase, offsetBase, count, 0, 0)
}

// TestSingleSampleNeverFires is the core safety property: one reading, however
// extreme, must not produce a fire warning.
func TestSingleSampleNeverFires(t *testing.T) {
	engine := newEngine(t)

	decision := engine.Evaluate(alert.Input{
		Sample: sample("MCU001", "boot1", 1, 0, 4095, 79),
	})
	if decision.State == domain.AlertFireWarning {
		t.Fatal("a single sample raised a fire warning")
	}
	if decision.Opened {
		t.Fatal("a single sample opened an alert event")
	}
}

// TestInsufficientSamplesStaysBelowWarning verifies that a strong but short
// excursion cannot reach fire_warning: the contract requires a minimum number of
// samples spanning a minimum duration.
func TestInsufficientSamplesStaysBelowWarning(t *testing.T) {
	cases := map[string][]domain.Telemetry{
		"too few samples":      rising(4, 400, 10),
		"too short a duration": rising(3, 400, 12),
	}
	for name, samples := range cases {
		t.Run(name, func(t *testing.T) {
			engine := newEngine(t)
			decision := feed(engine, samples, domain.AlertNormal, nil)
			if decision.State == domain.AlertFireWarning {
				t.Fatalf("%s produced fire_warning", name)
			}
		})
	}
}

// TestCompositeRequiresBothFactors verifies that either signal alone produces
// only a suspect state, which is the whole point of a two-factor rule.
func TestCompositeRequiresBothFactors(t *testing.T) {
	cases := map[string]struct {
		gasRise       int
		ratePerMinute float64
		want          domain.AlertState
	}{
		"gas only":         {gasRise: 400, ratePerMinute: 0, want: domain.AlertSuspect},
		"temperature only": {gasRise: 0, ratePerMinute: 12, want: domain.AlertSuspect},
		"neither":          {gasRise: 0, ratePerMinute: 0, want: domain.AlertNormal},
		"both":             {gasRise: 400, ratePerMinute: 12, want: domain.AlertFireWarning},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			engine := newEngine(t)
			decision := feed(engine, rising(8, testCase.gasRise, testCase.ratePerMinute), domain.AlertNormal, nil)
			if decision.State != testCase.want {
				t.Fatalf("state is %q, want %q (evidence %+v)", decision.State, testCase.want, decision.Evidence)
			}
		})
	}
}

// TestThresholdBoundary verifies the inclusive comparison on both thresholds: a
// value exactly at the threshold counts, one unit below does not.
func TestThresholdBoundary(t *testing.T) {
	config := alert.DefaultConfig()

	t.Run("gas rise exactly at threshold fires", func(t *testing.T) {
		engine := newEngine(t)
		decision := feed(engine, rising(8, config.GasAdcRiseThreshold, 12), domain.AlertNormal, nil)
		if decision.State != domain.AlertFireWarning {
			t.Fatalf("state is %q at the exact gas threshold, want fire_warning", decision.State)
		}
	})

	t.Run("gas rise one below threshold does not fire", func(t *testing.T) {
		engine := newEngine(t)
		decision := feed(engine, rising(8, config.GasAdcRiseThreshold-1, 12), domain.AlertNormal, nil)
		if decision.State == domain.AlertFireWarning {
			t.Fatal("a gas rise one ADC code below the threshold fired")
		}
	})

	t.Run("temperature rate exactly at threshold fires", func(t *testing.T) {
		engine := newEngine(t)
		decision := feed(engine, rising(8, 400, config.TemperatureRateThresholdCPerMinute), domain.AlertNormal, nil)
		if decision.State != domain.AlertFireWarning {
			t.Fatalf("state is %q at the exact slope threshold, want fire_warning", decision.State)
		}
	})

	t.Run("temperature rate just below threshold does not fire", func(t *testing.T) {
		engine := newEngine(t)
		decision := feed(engine, rising(8, 400, config.TemperatureRateThresholdCPerMinute*0.9), domain.AlertNormal, nil)
		if decision.State == domain.AlertFireWarning {
			t.Fatal("a slope below the threshold fired")
		}
	})
}

// TestOpenAndEscalate verifies that suspect opens an episode and that a
// confirmed composite warning advances the same episode rather than opening a
// second one.
func TestOpenAndEscalate(t *testing.T) {
	engine := newEngine(t)

	suspect := feed(engine, rising(8, 400, 0), domain.AlertNormal, nil)
	if suspect.State != domain.AlertSuspect || !suspect.Opened {
		t.Fatalf("gas-only series produced %+v, want an opened suspect episode", suspect)
	}

	open := domain.AlertEvent{
		ID: "01ALERT", DeviceID: "MCU001", State: domain.AlertSuspect,
		StartedAt: start, Evidence: suspect.Evidence,
	}
	// The second series continues the same boot and timeline; replaying the first
	// series would be deduplicated and would test nothing.
	escalated := feed(engine, risingFrom("boot1", 100, 40*time.Second, 8, 400, 12), domain.AlertSuspect, &open)
	if escalated.State != domain.AlertFireWarning {
		t.Fatalf("escalated state is %q, want fire_warning", escalated.State)
	}
	if !escalated.Escalated {
		t.Fatal("a state change on an open episode did not ask for an escalation")
	}
	if escalated.Opened {
		t.Fatal("an escalation asked to open a second episode")
	}
}

// TestRecoveryRequiresHold verifies that a single clear sample cannot end an
// episode and that the episode closes only after the hold elapses.
//
// The hold timer starts at the first sample the engine can actually evaluate,
// not at the first sample that happens to look quiet: before the window holds
// enough readings the engine cannot claim the conditions are clear, and starting
// the timer on an unmeasurable window would let an episode end on less evidence
// than it took to raise it.
func TestRecoveryRequiresHold(t *testing.T) {
	engine := newEngine(t)
	config := alert.DefaultConfig()

	open := domain.AlertEvent{
		ID: "01ALERT", DeviceID: "MCU001", State: domain.AlertFireWarning, StartedAt: start,
	}

	const interval = 5 * time.Second
	// A generous bound: the interesting assertion is where the close happens, not
	// that the loop is tight.
	const maxSamples = 20

	var (
		firstEvaluated time.Duration
		evaluatedSeen  bool
		closedAt       time.Duration
		closed         bool
	)
	for index := 0; index < maxSamples; index++ {
		offset := time.Duration(index) * interval
		decision := engine.Evaluate(alert.Input{
			Sample:       sample("MCU001", "boot1", uint32(index), offset, 1000, 25),
			CurrentState: domain.AlertFireWarning,
			ActiveAlert:  &open,
		})
		if decision.Closed {
			if !evaluatedSeen {
				t.Fatal("an alert closed on a window too small to evaluate")
			}
			closedAt = offset
			closed = true
			break
		}
		if decision.Evaluated {
			if !evaluatedSeen {
				evaluatedSeen = true
				firstEvaluated = offset
			}
			if decision.State != domain.AlertFireWarning {
				t.Fatalf("state at %s is %q, want fire_warning while the hold runs", offset, decision.State)
			}
			continue
		}
		if decision.State != domain.AlertFireWarning {
			t.Fatalf("state at %s is %q on an unevaluated window, want the episode held", offset, decision.State)
		}
	}

	if !closed {
		t.Fatal("the alert never closed after the hold elapsed")
	}
	held := closedAt - firstEvaluated
	if held < config.RecoveryHold {
		t.Fatalf("the alert closed %s after the hold began, before the %s hold", held, config.RecoveryHold)
	}
	if held >= config.RecoveryHold+interval {
		t.Fatalf("the alert closed %s after the hold began, more than one report interval late", held)
	}
}

// TestFlappingDoesNotClose verifies that a value crossing back over the
// threshold resets the recovery timer instead of accumulating clear time.
func TestFlappingDoesNotClose(t *testing.T) {
	engine := newEngine(t)
	config := alert.DefaultConfig()

	open := domain.AlertEvent{ID: "01ALERT", DeviceID: "MCU001", State: domain.AlertFireWarning, StartedAt: start}
	engine.Evaluate(alert.Input{
		Sample:       sample("MCU001", "boot1", 1, 0, 1000, 25),
		CurrentState: domain.AlertFireWarning,
		ActiveAlert:  &open,
	})
	// One gas spike mid-hold must restart the clear timer.
	engine.Evaluate(alert.Input{
		Sample:       sample("MCU001", "boot1", 2, config.RecoveryHold/2, 1400, 25),
		CurrentState: domain.AlertFireWarning,
		ActiveAlert:  &open,
	})
	decision := engine.Evaluate(alert.Input{
		Sample:       sample("MCU001", "boot1", 3, config.RecoveryHold, 1000, 25),
		CurrentState: domain.AlertFireWarning,
		ActiveAlert:  &open,
	})
	if decision.Closed {
		t.Fatal("a gas spike during recovery did not restart the clear timer")
	}
}

// TestRecoveredIsSticky verifies that a quiet device whose episode already ended
// keeps reporting recovered rather than decaying to normal, so the API can still
// tell an operator that the device has alarmed.
func TestRecoveredIsSticky(t *testing.T) {
	engine := newEngine(t)

	decision := feed(engine, flat("boot1", 1, 0, 6), domain.AlertRecovered, nil)
	if decision.State != domain.AlertRecovered {
		t.Fatalf("state after a recovered episode is %q, want recovered", decision.State)
	}
	if decision.Changed {
		t.Fatal("a quiet device reported a state change on every sample")
	}
}

// TestNormalForAQuietDevice verifies that a device with no episode stays normal
// and never opens an event.
func TestNormalForAQuietDevice(t *testing.T) {
	engine := newEngine(t)

	decision := feed(engine, flat("boot1", 1, 0, 6), domain.AlertNormal, nil)
	if decision.State != domain.AlertNormal {
		t.Fatalf("state for a quiet device is %q, want normal", decision.State)
	}
	if decision.Opened {
		t.Fatal("a quiet device opened an alert event")
	}
}

// TestRestartResetsWindow verifies that a new bootId discards the previous trend,
// because the sequence counter and the sensor warm-up state both restart.
func TestRestartResetsWindow(t *testing.T) {
	engine := newEngine(t)

	// Build a confirmed warning on the first boot.
	first := feed(engine, rising(8, 400, 12), domain.AlertNormal, nil)
	if first.State != domain.AlertFireWarning {
		t.Fatalf("precondition failed: state is %q", first.State)
	}

	// The first sample after a restart must not inherit the previous window: with
	// only one sample in the new boot there is no trend to evaluate.
	afterRestart := engine.Evaluate(alert.Input{
		Sample:       sample("MCU001", "boot2", 0, 5*time.Minute, 4095, 79),
		CurrentState: domain.AlertFireWarning,
	})
	if afterRestart.Evaluated {
		t.Fatalf("a restarted device was evaluated from the previous boot's window: %+v", afterRestart)
	}
}

// TestOutOfOrderSampleJoinsItsOwnPlace verifies that a delayed sample is folded
// into the window at its event-time position rather than appended at the end.
func TestOutOfOrderSampleJoinsItsOwnPlace(t *testing.T) {
	engine := newEngine(t)

	for index := 0; index < 4; index++ {
		engine.Ingest(sample("MCU001", "boot1", uint32(index), time.Duration(index)*5*time.Second, 1000, 25))
	}
	// A sample that arrives late but belongs in the middle of the window.
	engine.Ingest(sample("MCU001", "boot1", 9, 10*time.Second, 1010, 25))

	decision := engine.Evaluate(alert.Input{
		Sample: sample("MCU001", "boot1", 4, 20*time.Second, 1000, 25),
	})
	if decision.Evidence.SampleCount != 6 {
		t.Fatalf("window holds %d samples, want 6 including the late arrival", decision.Evidence.SampleCount)
	}
}

// TestDuplicateSampleIsCountedOnce verifies that re-ingesting the same
// (bootId, sequence) does not inflate the sample count.
func TestDuplicateSampleIsCountedOnce(t *testing.T) {
	engine := newEngine(t)

	for index := 0; index < 4; index++ {
		engine.Ingest(sample("MCU001", "boot1", uint32(index), time.Duration(index)*5*time.Second, 1000, 25))
	}
	engine.Ingest(sample("MCU001", "boot1", 3, 15*time.Second, 2000, 30))

	decision := engine.Evaluate(alert.Input{
		Sample: sample("MCU001", "boot1", 4, 20*time.Second, 1000, 25),
	})
	if decision.Evidence.SampleCount != 5 {
		t.Fatalf("window holds %d samples, want 5", decision.Evidence.SampleCount)
	}
}

// TestBaselineResistsASingleSpike verifies that the gas baseline is a median, so
// one noisy early reading cannot manufacture a rise.
func TestBaselineResistsASingleSpike(t *testing.T) {
	engine := newEngine(t)

	series := []domain.Telemetry{
		sample("MCU001", "boot1", 0, 0, 1020, 25),
		sample("MCU001", "boot1", 1, 5*time.Second, 1000, 25),
		// One early spike that a mean would carry into the baseline.
		sample("MCU001", "boot1", 2, 10*time.Second, 3000, 25),
		sample("MCU001", "boot1", 3, 15*time.Second, 1000, 25),
		sample("MCU001", "boot1", 4, 20*time.Second, 1000, 25),
		sample("MCU001", "boot1", 5, 25*time.Second, 1030, 25),
	}
	decision := feed(engine, series, domain.AlertNormal, nil)

	// The baseline is the median of the first three samples, which is 1020 even
	// though one of them is 3000. A mean would be 1673 and would make the rise
	// negative; the median keeps the measurement attached to the typical reading.
	if decision.Evidence.GasAdcRise != 10 {
		t.Fatalf("gas rise is %d, want 10 from a median baseline of 1020", decision.Evidence.GasAdcRise)
	}
	if decision.State != domain.AlertNormal {
		t.Fatalf("state is %q, want normal; a single early spike must not simulate a surge", decision.State)
	}
}

// TestSamplesOutsideWindowAreDropped verifies the window trim: a long silence
// leaves too little data to decide anything, rather than making a stale trend
// look current.
func TestSamplesOutsideWindowAreDropped(t *testing.T) {
	engine := newEngine(t)

	decision := feed(engine, flat("boot1", 0, 0, 6), domain.AlertNormal, nil)
	if !decision.Evaluated || decision.Evidence.SampleCount != 6 {
		t.Fatalf("first window reported %+v, want six evaluated samples", decision)
	}

	// The next sample arrives well outside the window, so every earlier sample is
	// trimmed and nothing can be decided.
	afterGap := engine.Evaluate(alert.Input{
		Sample: sample("MCU001", "boot1", 100, 10*time.Minute, 1000, 25),
	})
	if afterGap.Evaluated {
		t.Fatalf("a window holding only the newest sample was evaluated: %+v", afterGap)
	}
	if afterGap.State == domain.AlertFireWarning {
		t.Fatal("a single sample after a gap raised a fire warning")
	}
}

// TestEvidenceIsRecordedAtTriggerTime verifies that the evidence carries the
// thresholds in force and the window that produced the decision, so a historical
// alert can be explained without recomputation.
func TestEvidenceIsRecordedAtTriggerTime(t *testing.T) {
	engine := newEngine(t)
	config := alert.DefaultConfig()

	decision := feed(engine, rising(8, 400, 12), domain.AlertNormal, nil)
	if decision.State != domain.AlertFireWarning {
		t.Fatalf("precondition failed: state is %q", decision.State)
	}
	if decision.Evidence.GasAdcRiseThreshold != config.GasAdcRiseThreshold {
		t.Fatalf("evidence gas threshold is %d, want %d", decision.Evidence.GasAdcRiseThreshold, config.GasAdcRiseThreshold)
	}
	if decision.Evidence.TemperatureRateThresholdCPerMinute != config.TemperatureRateThresholdCPerMinute {
		t.Fatalf("evidence slope threshold is %v, want %v",
			decision.Evidence.TemperatureRateThresholdCPerMinute, config.TemperatureRateThresholdCPerMinute)
	}
	if decision.Evidence.WindowSeconds != 35 {
		t.Fatalf("evidence window is %ds, want 35", decision.Evidence.WindowSeconds)
	}
	if decision.Evidence.SampleCount != 8 {
		t.Fatalf("evidence sample count is %d, want 8", decision.Evidence.SampleCount)
	}
}

// TestConfigValidation covers the values that would make the rule unsound.
func TestConfigValidation(t *testing.T) {
	cases := map[string]func(*alert.Config){
		"zero window":              func(c *alert.Config) { c.Window = 0 },
		"single sample allowed":    func(c *alert.Config) { c.MinSamples = 1 },
		"zero min duration":        func(c *alert.Config) { c.MinDuration = 0 },
		"min duration over window": func(c *alert.Config) { c.MinDuration = c.Window + time.Second },
		"zero gas threshold":       func(c *alert.Config) { c.GasAdcRiseThreshold = 0 },
		"gas threshold above ADC":  func(c *alert.Config) { c.GasAdcRiseThreshold = domain.MaxGasAdc + 1 },
		"negative slope":           func(c *alert.Config) { c.TemperatureRateThresholdCPerMinute = -1 },
		"negative hold":            func(c *alert.Config) { c.RecoveryHold = -time.Second },
		"zero baseline divisor":    func(c *alert.Config) { c.BaselineDivisor = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			config := alert.DefaultConfig()
			mutate(&config)
			if _, err := alert.NewEngine(config); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}

	if _, err := alert.NewEngine(alert.DefaultConfig()); err != nil {
		t.Fatalf("the default configuration was rejected: %v", err)
	}
}
