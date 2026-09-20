package protocol

import "sort"

// The functions below expose the frozen payload key sets, sorted, so the contract
// test can compare them with the payloads printed in docs/device-protocol.md and
// with the schemas in docs/api/openapi.yaml. A field that exists in the code but
// not in the document, or the other way round, is a contract defect.

// TelemetryFields returns the accepted telemetry key set, sorted.
func TelemetryFields() []string { return sortedKeys(telemetryKeys) }

// CommandAckFields returns the accepted acknowledgement key set, sorted.
func CommandAckFields() []string { return sortedKeys(ackKeys) }

// ControlFields returns the accepted control key set, sorted.
func ControlFields() []string { return sortedKeys(controlKeys) }

// sortedKeys returns the keys of a set in lexicographic order.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
