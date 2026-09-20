package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/BobcGn/final/backend/internal/domain"
)

// Memory is an in-process Store used by unit tests, by the HTTP handler tests
// and by local development runs where standing up PostgreSQL is unnecessary.
//
// It is not a cache in front of PostgreSQL: it is a complete implementation of
// the same contract, and the shared conformance suite in storetest runs against
// both. Its sort and cursor behaviour is deliberately identical to the SQL
// implementation so that a pagination bug cannot hide behind a driver.
type Memory struct {
	mu sync.Mutex

	// samples holds each device's telemetry ordered by (eventTime, bootID,
	// sequence). Keeping it sorted on write makes the frozen cursor pagination a
	// binary search rather than a re-sort per query.
	samples map[string][]domain.Telemetry
	seen    map[domain.TelemetryKey]struct{}

	alerts map[string][]domain.AlertEvent

	devices    map[string]DeviceState
	thresholds map[string]ThresholdRecord

	commands    map[string]map[string]domain.Command
	idempotency map[string]string
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		samples:     make(map[string][]domain.Telemetry),
		seen:        make(map[domain.TelemetryKey]struct{}),
		alerts:      make(map[string][]domain.AlertEvent),
		devices:     make(map[string]DeviceState),
		thresholds:  make(map[string]ThresholdRecord),
		commands:    make(map[string]map[string]domain.Command),
		idempotency: make(map[string]string),
	}
}

// compile-time assertion that Memory satisfies Store.
var _ Store = (*Memory)(nil)

// InsertTelemetry implements Store.
func (m *Memory) InsertTelemetry(_ context.Context, sample domain.Telemetry) (bool, error) {
	if err := sample.Validate(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := sample.Key()
	if _, exists := m.seen[key]; exists {
		return false, nil
	}
	m.seen[key] = struct{}{}

	series := m.samples[sample.DeviceID]
	position := sort.Search(len(series), func(i int) bool {
		return compareSamples(series[i], sample) > 0
	})
	series = append(series, domain.Telemetry{})
	copy(series[position+1:], series[position:])
	series[position] = sample
	m.samples[sample.DeviceID] = series
	return true, nil
}

// LatestTelemetry implements Store.
func (m *Memory) LatestTelemetry(_ context.Context, deviceID string) (domain.Telemetry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	series := m.samples[deviceID]
	if len(series) == 0 {
		return domain.Telemetry{}, fmt.Errorf("%w: no telemetry for device %s", ErrNotFound, deviceID)
	}
	return series[len(series)-1], nil
}

// ListTelemetry implements Store.
func (m *Memory) ListTelemetry(_ context.Context, query TelemetryQuery) (TelemetryPage, error) {
	limit := NormalizeLimit(query.Limit)
	order := NormalizeOrder(query.Order)
	if err := query.Range.Validate(); err != nil {
		return TelemetryPage{}, err
	}

	var after *cursorPayload
	if query.Cursor != "" {
		decoded, err := decodeCursor(query.Cursor, queryScope(query.DeviceID, query.Range, order))
		if err != nil {
			return TelemetryPage{}, err
		}
		after = &decoded
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	inRange := make([]domain.Telemetry, 0, len(m.samples[query.DeviceID]))
	for _, sample := range m.samples[query.DeviceID] {
		eventTime := sample.EventTime()
		if eventTime.Before(query.Range.From) || !eventTime.Before(query.Range.To) {
			continue
		}
		if after != nil && !resumesAfter(sample, *after, order) {
			continue
		}
		inRange = append(inRange, sample)
	}
	if order == OrderDesc {
		reverseSamples(inRange)
	}

	page := TelemetryPage{Items: inRange}
	if len(inRange) > limit {
		page.Items = inRange[:limit]
		last := page.Items[limit-1]
		page.NextCursor = encodeCursor(cursorPayload{
			EventTimeMillis: last.EventTime().UnixMilli(),
			BootID:          last.BootID,
			Sequence:        last.Sequence,
			Scope:           queryScope(query.DeviceID, query.Range, order),
		})
	}
	return page, nil
}

// InsertAlert implements Store.
func (m *Memory) InsertAlert(_ context.Context, event domain.AlertEvent) (domain.AlertEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if event.ID == "" {
		event.ID = NewID(event.StartedAt)
	}
	if err := event.Validate(); err != nil {
		return domain.AlertEvent{}, err
	}
	if _, err := eventByID(m.alerts[event.DeviceID], event.ID); err == nil {
		return domain.AlertEvent{}, fmt.Errorf("%w: alert %s already exists", ErrConflict, event.ID)
	}
	// At most one open alert per device, matching the partial unique index in the
	// PostgreSQL schema. Without this the in-memory store would let two episodes
	// run at once and ActiveAlert would become ambiguous.
	if event.EndedAt == nil {
		for _, existing := range m.alerts[event.DeviceID] {
			if existing.EndedAt == nil {
				return domain.AlertEvent{}, fmt.Errorf("%w: device %s already has an open alert", ErrConflict, event.DeviceID)
			}
		}
	}
	m.alerts[event.DeviceID] = append(m.alerts[event.DeviceID], event)
	return event, nil
}

// UpdateAlert implements Store.
func (m *Memory) UpdateAlert(_ context.Context, deviceID, alertID string, state domain.AlertState, evidence domain.AlertEvidence) (domain.AlertEvent, error) {
	if !state.Valid() {
		return domain.AlertEvent{}, fmt.Errorf("%w: unknown state %q", domain.ErrInvalidAlert, state)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	series := m.alerts[deviceID]
	for i := range series {
		if series[i].ID != alertID {
			continue
		}
		if series[i].EndedAt != nil {
			// An ended episode is history; moving it forward would rewrite the
			// evidence a client may already have seen.
			return domain.AlertEvent{}, fmt.Errorf("%w: alert %s already ended", ErrConflict, alertID)
		}
		series[i].State = state
		series[i].Evidence = evidence
		return series[i], nil
	}
	return domain.AlertEvent{}, fmt.Errorf("%w: alert %s", ErrNotFound, alertID)
}

// CloseAlert implements Store.
func (m *Memory) CloseAlert(_ context.Context, deviceID, alertID string, state domain.AlertState, endedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	series := m.alerts[deviceID]
	for i := range series {
		if series[i].ID != alertID {
			continue
		}
		if series[i].EndedAt != nil {
			// Closing an already closed alert is idempotent: a retried recovery
			// must not move the recorded end time.
			return nil
		}
		ended := endedAt.UTC()
		series[i].State = state
		series[i].EndedAt = &ended
		return nil
	}
	return fmt.Errorf("%w: alert %s", ErrNotFound, alertID)
}

// ActiveAlert implements Store.
func (m *Memory) ActiveAlert(_ context.Context, deviceID string) (domain.AlertEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	series := m.alerts[deviceID]
	for i := len(series) - 1; i >= 0; i-- {
		if series[i].EndedAt == nil {
			return series[i], nil
		}
	}
	return domain.AlertEvent{}, fmt.Errorf("%w: no active alert for device %s", ErrNotFound, deviceID)
}

// ListAlerts implements Store.
func (m *Memory) ListAlerts(_ context.Context, query AlertQuery) (AlertPage, error) {
	limit := NormalizeLimit(query.Limit)
	order := NormalizeOrder(query.Order)
	if err := query.Range.Validate(); err != nil {
		return AlertPage{}, err
	}

	var after *cursorPayload
	if query.Cursor != "" {
		decoded, err := decodeCursor(query.Cursor, queryScope(query.DeviceID, query.Range, order))
		if err != nil {
			return AlertPage{}, err
		}
		after = &decoded
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	matched := make([]domain.AlertEvent, 0, len(m.alerts[query.DeviceID]))
	for _, event := range m.alerts[query.DeviceID] {
		if event.StartedAt.Before(query.Range.From) || !event.StartedAt.Before(query.Range.To) {
			continue
		}
		if query.State != "" && event.State != query.State {
			continue
		}
		if query.ActiveOnly && event.EndedAt != nil {
			continue
		}
		if after != nil && !alertResumesAfter(event, *after, order) {
			continue
		}
		matched = append(matched, event)
	}
	if order == OrderDesc {
		for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
			matched[i], matched[j] = matched[j], matched[i]
		}
	}

	page := AlertPage{Items: matched}
	if len(matched) > limit {
		page.Items = matched[:limit]
		last := page.Items[limit-1]
		page.NextCursor = encodeCursor(cursorPayload{
			EventTimeMillis: last.StartedAt.UnixMilli(),
			ID:              last.ID,
			Scope:           queryScope(query.DeviceID, query.Range, order),
		})
	}
	return page, nil
}

// UpsertDevice implements Store.
func (m *Memory) UpsertDevice(_ context.Context, state DeviceState) error {
	if err := domain.ValidateDeviceID(state.DeviceID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.devices[state.DeviceID] = state
	return nil
}

// Device implements Store.
func (m *Memory) Device(_ context.Context, deviceID string) (DeviceState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.devices[deviceID]
	if !ok {
		return DeviceState{}, fmt.Errorf("%w: device %s", ErrNotFound, deviceID)
	}
	return state, nil
}

// ListDevices implements Store.
func (m *Memory) ListDevices(_ context.Context) ([]DeviceState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	devices := make([]DeviceState, 0, len(m.devices))
	for _, state := range m.devices {
		devices = append(devices, state)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].DeviceID < devices[j].DeviceID })
	return devices, nil
}

// Thresholds implements Store.
func (m *Memory) Thresholds(_ context.Context, deviceID string) (ThresholdRecord, error) {
	if err := domain.ValidateDeviceID(deviceID); err != nil {
		return ThresholdRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.thresholdsLocked(deviceID), nil
}

// thresholdsLocked returns the stored record or the compile-time default. A
// device that has never been configured must still answer GET /thresholds, and
// the honest answer is the firmware default at version 1, not an empty object.
func (m *Memory) thresholdsLocked(deviceID string) ThresholdRecord {
	if record, ok := m.thresholds[deviceID]; ok {
		return record
	}
	return ThresholdRecord{
		DeviceID:          deviceID,
		Desired:           domain.DefaultThresholds(),
		DesiredVersion:    domain.InitialThresholdVersion,
		ConfirmationState: ConfirmationPending,
	}
}

// SetDesiredThresholds implements Store.
func (m *Memory) SetDesiredThresholds(_ context.Context, deviceID string, thresholds domain.Thresholds, version int, at time.Time) error {
	if err := domain.ValidateDeviceID(deviceID); err != nil {
		return err
	}
	if err := thresholds.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	record := m.thresholdsLocked(deviceID)
	if version <= record.DesiredVersion {
		return fmt.Errorf("%w: threshold version %d is not greater than the stored version %d", ErrConflict, version, record.DesiredVersion)
	}
	record.Desired = thresholds
	record.DesiredVersion = version
	record.UpdatedAt = at.UTC()
	record.ConfirmationState = ConfirmationPending
	m.thresholds[deviceID] = record
	return nil
}

// ConfirmThresholdVersion implements Store.
func (m *Memory) ConfirmThresholdVersion(_ context.Context, deviceID string, version int, at time.Time) error {
	if err := domain.ValidateDeviceID(deviceID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	record := m.thresholdsLocked(deviceID)
	confirmed := version
	// A device reporting an older version than we already recorded is a stale
	// report, not a rollback; keeping the newest confirmation avoids flapping the
	// confirmation state when acknowledgements arrive out of order.
	if record.ConfirmedVersion == nil || version > *record.ConfirmedVersion {
		record.ConfirmedVersion = &confirmed
	}
	record.ConfirmationState = confirmationState(record)
	record.UpdatedAt = at.UTC()
	m.thresholds[deviceID] = record
	return nil
}

// confirmationState derives the state the API reports.
func confirmationState(record ThresholdRecord) ConfirmationState {
	if record.ConfirmedVersion != nil && *record.ConfirmedVersion >= record.DesiredVersion {
		return ConfirmationConfirmed
	}
	return ConfirmationPending
}

// InsertCommand implements Store.
func (m *Memory) InsertCommand(_ context.Context, command domain.Command) error {
	if err := command.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.commands[command.DeviceID]; !ok {
		m.commands[command.DeviceID] = make(map[string]domain.Command)
	}
	if _, exists := m.commands[command.DeviceID][command.RequestID]; exists {
		return fmt.Errorf("%w: command %s already exists", ErrConflict, command.RequestID)
	}
	if command.IdempotencyKey != "" {
		key := idempotencyIndexKey(command.DeviceID, command.IdempotencyKey)
		if _, exists := m.idempotency[key]; exists {
			return fmt.Errorf("%w: idempotency key already used for device %s", ErrConflict, command.DeviceID)
		}
		m.idempotency[key] = command.RequestID
	}

	// A set_thresholds command and the desired threshold version it carries are
	// one unit: recording them under the same lock keeps GET /thresholds from
	// ever reporting a version no command carried.
	if command.Type == domain.CommandSetThresholds && command.Payload.Thresholds != nil && command.DesiredVersion != nil {
		record := m.thresholdsLocked(command.DeviceID)
		record.Desired = *command.Payload.Thresholds
		record.DesiredVersion = *command.DesiredVersion
		record.ConfirmationState = ConfirmationPending
		record.UpdatedAt = command.AcceptedAt.UTC()
		m.thresholds[command.DeviceID] = record
	}

	m.commands[command.DeviceID][command.RequestID] = command
	return nil
}

// idempotencyIndexKey scopes an idempotency key to one device.
func idempotencyIndexKey(deviceID, key string) string { return deviceID + "\x00" + key }

// Command implements Store.
func (m *Memory) Command(_ context.Context, deviceID, requestID string) (domain.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	command, ok := m.commands[deviceID][requestID]
	if !ok {
		return domain.Command{}, fmt.Errorf("%w: command %s", ErrNotFound, requestID)
	}
	return command, nil
}

// CommandByIdempotencyKey implements Store.
func (m *Memory) CommandByIdempotencyKey(_ context.Context, deviceID, key string) (domain.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	requestID, ok := m.idempotency[idempotencyIndexKey(deviceID, key)]
	if !ok {
		return domain.Command{}, fmt.Errorf("%w: idempotency key for device %s", ErrNotFound, deviceID)
	}
	return m.commands[deviceID][requestID], nil
}

// MarkCommandPublished implements Store.
func (m *Memory) MarkCommandPublished(_ context.Context, deviceID, requestID string, at time.Time) (domain.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	command, ok := m.commands[deviceID][requestID]
	if !ok {
		return domain.Command{}, fmt.Errorf("%w: command %s", ErrNotFound, requestID)
	}
	if !domain.CanTransition(command.State, domain.CommandPublished) {
		return domain.Command{}, fmt.Errorf("%w: cannot move command %s from %s to %s", ErrConflict, requestID, command.State, domain.CommandPublished)
	}
	published := at.UTC()
	command.State = domain.CommandPublished
	command.PublishedAt = &published
	m.commands[deviceID][requestID] = command
	return command, nil
}

// ApplyCommandPatch implements Store.
func (m *Memory) ApplyCommandPatch(_ context.Context, deviceID, requestID string, patch domain.CommandPatch) (domain.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	command, ok := m.commands[deviceID][requestID]
	if !ok {
		return domain.Command{}, fmt.Errorf("%w: command %s", ErrNotFound, requestID)
	}
	if command.State == patch.State {
		// A replayed acknowledgement for a command that already reached this
		// state is idempotent, not a conflict: MQTT QoS 1 redelivers at least
		// once, so identical replays are expected.
		return command, nil
	}
	if !domain.CanTransition(command.State, patch.State) {
		return domain.Command{}, fmt.Errorf("%w: cannot move command %s from %s to %s", ErrConflict, requestID, command.State, patch.State)
	}
	completed := patch.CompletedAt.UTC()
	command.State = patch.State
	command.CompletedAt = &completed
	command.ConfirmedVersion = patch.ConfirmedVersion
	command.ErrorCode = patch.ErrorCode
	m.commands[deviceID][requestID] = command
	return command, nil
}

// ExpireCommands implements Store.
func (m *Memory) ExpireCommands(_ context.Context, now time.Time) ([]domain.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var expired []domain.Command
	for _, commands := range m.commands {
		for requestID, command := range commands {
			if command.State.Terminal() || command.ExpiresAt.After(now) {
				continue
			}
			completed := now.UTC()
			command.State = domain.CommandTimedOut
			command.CompletedAt = &completed
			commands[requestID] = command
			expired = append(expired, command)
		}
	}
	sort.Slice(expired, func(i, j int) bool { return expired[i].RequestID < expired[j].RequestID })
	return expired, nil
}

// PendingCommands implements Store.
func (m *Memory) PendingCommands(_ context.Context) ([]domain.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var pending []domain.Command
	for _, commands := range m.commands {
		for _, command := range commands {
			if !command.State.Terminal() {
				pending = append(pending, command)
			}
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].RequestID < pending[j].RequestID })
	return pending, nil
}

// compareSamples orders two samples by the frozen sort key
// (eventTime, bootID, sequence).
func compareSamples(a, b domain.Telemetry) int {
	left, right := a.EventTime(), b.EventTime()
	switch {
	case left.Before(right):
		return -1
	case left.After(right):
		return 1
	}
	if a.BootID != b.BootID {
		if a.BootID < b.BootID {
			return -1
		}
		return 1
	}
	switch {
	case a.Sequence < b.Sequence:
		return -1
	case a.Sequence > b.Sequence:
		return 1
	default:
		return 0
	}
}

// resumesAfter reports whether sample sorts strictly after the cursor position
// for the given order. It compares the frozen sort key (eventTime, bootID,
// sequence) so that rows sharing a timestamp are neither skipped nor repeated.
func resumesAfter(sample domain.Telemetry, cursor cursorPayload, order Order) bool {
	position := compareKeyToCursor(sample, cursor)
	if order == OrderDesc {
		return position < 0
	}
	return position > 0
}

// compareKeyToCursor orders a sample's sort key against a decoded cursor, using
// the same key definition as compareSamples.
func compareKeyToCursor(sample domain.Telemetry, cursor cursorPayload) int {
	eventTime := sample.EventTime()
	cursorTime := cursor.cursorTime()
	switch {
	case eventTime.Before(cursorTime):
		return -1
	case eventTime.After(cursorTime):
		return 1
	}
	if sample.BootID != cursor.BootID {
		if sample.BootID < cursor.BootID {
			return -1
		}
		return 1
	}
	switch {
	case sample.Sequence < cursor.Sequence:
		return -1
	case sample.Sequence > cursor.Sequence:
		return 1
	default:
		return 0
	}
}

// alertResumesAfter reports whether an alert sorts strictly after the cursor.
func alertResumesAfter(event domain.AlertEvent, cursor cursorPayload, order Order) bool {
	switch order {
	case OrderDesc:
		if event.StartedAt.After(cursor.cursorTime()) {
			return false
		}
		if event.StartedAt.Equal(cursor.cursorTime()) {
			return event.ID < cursor.ID
		}
		return true
	default:
		if event.StartedAt.Before(cursor.cursorTime()) {
			return false
		}
		if event.StartedAt.Equal(cursor.cursorTime()) {
			return event.ID > cursor.ID
		}
		return true
	}
}

// eventByID finds an alert by identifier within a device's series.
func eventByID(series []domain.AlertEvent, id string) (domain.AlertEvent, error) {
	for _, event := range series {
		if event.ID == id {
			return event, nil
		}
	}
	return domain.AlertEvent{}, fmt.Errorf("%w: alert %s", ErrNotFound, id)
}

// reverseSamples reverses a slice of samples in place.
func reverseSamples(samples []domain.Telemetry) {
	for i, j := 0, len(samples)-1; i < j; i, j = i+1, j-1 {
		samples[i], samples[j] = samples[j], samples[i]
	}
}
