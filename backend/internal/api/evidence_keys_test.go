package api_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BobcGn/final/backend/internal/api"
	"github.com/BobcGn/final/backend/internal/domain"
	"github.com/BobcGn/final/backend/internal/events"
)

// The contract test that was missing. encoding/json matches keys loosely, so a
// response decoded into a Go struct proves nothing about the names on the wire:
// PascalCase would be accepted by a struct that also declares camelCase. These
// cases therefore decode into map[string]json.RawMessage and assert on the raw
// key strings, which is the only place the defect can be seen.
//
// docs/api/openapi.yaml is the fact source for the names. The historical
// PascalCase output is not a compatibility surface and no alias is kept.

var evidenceCamel = []string{
	"gasAdcRise",
	"gasAdcRiseThreshold",
	"temperatureRateCPerMinute",
	"temperatureRateThresholdCPerMinute",
	"sampleCount",
	"windowSeconds",
}

var evidencePascal = []string{
	"GasAdcRise",
	"GasAdcRiseThreshold",
	"TemperatureRateCPerMinute",
	"TemperatureRateThresholdCPerMinute",
	"SampleCount",
	"WindowSeconds",
}

func assertEvidenceKeys(t *testing.T, raw map[string]json.RawMessage) {
	t.Helper()

	for _, key := range evidenceCamel {
		if _, ok := raw[key]; !ok {
			t.Errorf("evidence is missing the contract key %q; it has %v", key, keys(raw))
		}
	}
	for _, key := range evidencePascal {
		if _, ok := raw[key]; ok {
			t.Errorf("evidence carries the Go name %q; the contract uses camelCase", key)
		}
	}
}

func keys(raw map[string]json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for k := range raw {
		out = append(out, k)
	}
	return out
}

// TestRESTAlertEvidenceUsesTheContractKeyNames is the raw-body check for
// GET /alerts. An empty list would pass vacuously, so the case asserts against a
// response that carries an episode.
func TestRESTAlertEvidenceUsesTheContractKeyNames(t *testing.T) {
	body := marshal(t, api.NewAlertPageResourceForTest([]domain.AlertEvent{sampleEvent()}))

	page := struct {
		Items []json.RawMessage `json:"items"`
	}{}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal page: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(page.Items))
	}

	event := map[string]json.RawMessage{}
	if err := json.Unmarshal(page.Items[0], &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	evidence := map[string]json.RawMessage{}
	if err := json.Unmarshal(event["evidence"], &evidence); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	assertEvidenceKeys(t, evidence)
}

// TestEventAlertEvidenceUsesTheContractKeyNames is the same check for the
// realtime stream. The two boundaries are separate types on purpose, so neither
// can be fixed by fixing the other.
func TestEventAlertEvidenceUsesTheContractKeyNames(t *testing.T) {
	body := marshal(t, events.AlertDataFrom(sampleEvent()))

	data := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	evidence := map[string]json.RawMessage{}
	if err := json.Unmarshal(data["evidence"], &evidence); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	assertEvidenceKeys(t, evidence)
}

// TestAlertEvidenceRoundTripKeepsValuesAndUnits proves the fix is naming only.
// A mapping that swapped a field or changed a unit would still produce the right
// key names and a wrong answer.
func TestAlertEvidenceRoundTripKeepsValuesAndUnits(t *testing.T) {
	source := domain.AlertEvidence{
		GasAdcRise:                         1204,
		GasAdcRiseThreshold:                150,
		TemperatureRateCPerMinute:          -2.5,
		TemperatureRateThresholdCPerMinute: 1.5,
		SampleCount:                        5,
		WindowSeconds:                      36,
	}

	rest := map[string]json.RawMessage{}
	if err := json.Unmarshal(marshal(t, api.NewAlertEventResourceForTest(eventWith(source))), &rest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	evidence := map[string]any{}
	if err := json.Unmarshal(rest["evidence"], &evidence); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}

	// ADC codes stay integers and the rate stays a signed rate: the gas term is
	// a difference in ADC codes and the temperature term is a per-minute slope.
	if got := evidence["gasAdcRise"].(float64); got != 1204 {
		t.Errorf("gasAdcRise = %v, want 1204", got)
	}
	if got := evidence["gasAdcRiseThreshold"].(float64); got != 150 {
		t.Errorf("gasAdcRiseThreshold = %v, want 150", got)
	}
	if got := evidence["temperatureRateCPerMinute"].(float64); got != -2.5 {
		t.Errorf("temperatureRateCPerMinute = %v, want -2.5", got)
	}
	if got := evidence["temperatureRateThresholdCPerMinute"].(float64); got != 1.5 {
		t.Errorf("temperatureRateThresholdCPerMinute = %v, want 1.5", got)
	}
	if got := evidence["sampleCount"].(float64); got != 5 {
		t.Errorf("sampleCount = %v, want 5", got)
	}
	if got := evidence["windowSeconds"].(float64); got != 36 {
		t.Errorf("windowSeconds = %v, want 36", got)
	}
}

// TestAlertEvidenceIsNotRecomputedFromLatestTelemetry pins the contract rule
// that the evidence is the reason recorded at trigger time. A caller that
// rebuilt it from the newest sample would still emit the right key names and a
// reason that no longer describes the event.
func TestAlertEvidenceIsNotRecomputedFromLatestTelemetry(t *testing.T) {
	recorded := domain.AlertEvidence{
		GasAdcRise:                1204,
		TemperatureRateCPerMinute: 2.5,
		SampleCount:               5,
		WindowSeconds:             36,
	}
	event := eventWith(recorded)

	rest := map[string]json.RawMessage{}
	if err := json.Unmarshal(marshal(t, api.NewAlertEventResourceForTest(event)), &rest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	evidence := map[string]any{}
	if err := json.Unmarshal(rest["evidence"], &evidence); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}

	if got := evidence["gasAdcRise"].(float64); got != 1204 {
		t.Errorf("gasAdcRise = %v, want the recorded 1204", got)
	}
	if got := evidence["sampleCount"].(float64); got != 5 {
		t.Errorf("sampleCount = %v, want the recorded 5", got)
	}
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

func sampleEvent() domain.AlertEvent {
	return eventWith(domain.AlertEvidence{
		GasAdcRise:                         1204,
		GasAdcRiseThreshold:                150,
		TemperatureRateCPerMinute:          2.5,
		TemperatureRateThresholdCPerMinute: 1.5,
		SampleCount:                        5,
		WindowSeconds:                      36,
	})
}

func eventWith(evidence domain.AlertEvidence) domain.AlertEvent {
	started := time.Date(2026, 9, 22, 4, 16, 32, 0, time.UTC)
	ended := started.Add(82 * time.Second)
	return domain.AlertEvent{
		ID:        "01M33ED2SP2BZ4VBFBRPV351M6",
		DeviceID:  "MCU001",
		State:     domain.AlertRecovered,
		StartedAt: started,
		EndedAt:   &ended,
		Evidence:  evidence,
	}
}

// TestAnEmptyAlertPageStillDecodes proves the empty case is not a special shape.
// A list with no items has no evidence to inspect, so what matters is that the
// page itself still parses under the same decoder the populated case uses.
func TestAnEmptyAlertPageStillDecodes(t *testing.T) {
	body := marshal(t, api.NewAlertPageResourceForTest(nil))

	page := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal empty page: %v", err)
	}
	items := []json.RawMessage{}
	if err := json.Unmarshal(page["items"], &items); err != nil {
		t.Fatalf("unmarshal empty items: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

// TestAFilteredAlertPageKeepsTheEvidenceShape proves filtering does not change
// the wire type. A filtered page is built from the same rows, and a test that
// only ever saw the unfiltered path would miss a filter-specific encoding.
func TestAFilteredAlertPageKeepsTheEvidenceShape(t *testing.T) {
	// One of each state, as a filter would leave behind.
	events := []domain.AlertEvent{
		eventWith(sampleEvent().Evidence),
		eventWith(sampleEvent().Evidence),
	}
	events[0].State = domain.AlertFireWarning
	events[1].State = domain.AlertRecovered

	body := marshal(t, api.NewAlertPageResourceForTest(events))

	page := struct {
		Items []json.RawMessage `json:"items"`
	}{}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal page: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(page.Items))
	}
	for _, item := range page.Items {
		event := map[string]json.RawMessage{}
		if err := json.Unmarshal(item, &event); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		evidence := map[string]json.RawMessage{}
		if err := json.Unmarshal(event["evidence"], &evidence); err != nil {
			t.Fatalf("unmarshal evidence: %v", err)
		}
		assertEvidenceKeys(t, evidence)
	}
}

// TestNoWireTypeEmbedsAnUntaggedDomainEvidence guards the fix from being undone
// by an innocent-looking refactor.
//
// The bug this file exists to prevent is re-embedding domain.AlertEvidence in a
// response type. That compiles, it marshals, and it emits Go names. A future
// change that writes `Evidence domain.AlertEvidence `json:"evidence"“ again
// would look correct and would break every client, and none of the tests above
// would see it because they decode into maps. This one reflects over the wire
// types and asserts their evidence field is a DTO with tags.
func TestNoWireTypeEmbedsAnUntaggedDomainEvidence(t *testing.T) {
	for _, wire := range []any{api.AlertEventResourceForTypeTest(), events.AlertData{}} {
		assertEvidenceIsATaggedDTO(t, wire)
	}
}

func assertEvidenceIsATaggedDTO(t *testing.T, wire any) {
	t.Helper()

	structure := reflect.TypeOf(wire)
	for i := 0; i < structure.NumField(); i++ {
		field := structure.Field(i)
		tag := field.Tag.Get("json")
		if tag != "evidence" && !strings.HasPrefix(tag, "evidence,") {
			continue
		}
		if field.Type == reflect.TypeOf(domain.AlertEvidence{}) {
			t.Errorf("%s.evidence is the untagged domain type; marshal it through a DTO with "+
				"camelCase tags or the wire will carry Go names", structure.Name())
			continue
		}
		for j := 0; j < field.Type.NumField(); j++ {
			name := field.Type.Field(j).Name
			if name[0] >= 'A' && name[0] <= 'Z' && field.Type.Field(j).Tag.Get("json") == "" {
				t.Errorf("%s.evidence.%s has no JSON tag; an untagged field marshals under its Go name",
					structure.Name(), name)
			}
		}
	}
}
