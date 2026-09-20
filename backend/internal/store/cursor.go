package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidCursor reports a cursor that is malformed or does not belong to the
// query it was presented with. Callers map it to HTTP 400 invalid_request.
var ErrInvalidCursor = errors.New("store: invalid cursor")

// cursorPayload is the decoded form of an opaque pagination cursor. It carries
// the exact sort key of the last row of the previous page, so the next page can
// resume without skipping or repeating rows whose sort keys are equal.
//
// scope binds the cursor to the query it came from. The frozen contract requires
// that a cursored request repeats the first page's from, to and order; binding
// them into the cursor turns a violation into a 400 instead of silently
// returning a page from a different series.
type cursorPayload struct {
	EventTimeMillis int64  `json:"t"`
	BootID          string `json:"b,omitempty"`
	Sequence        uint32 `json:"s,omitempty"`
	ID              string `json:"i,omitempty"`
	Scope           string `json:"q"`
}

// encodeCursor renders a cursor payload as a URL-safe opaque token.
func encodeCursor(payload cursorPayload) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		// cursorPayload contains only JSON-safe primitives, so marshalling
		// cannot fail; an empty cursor would silently restart pagination, and
		// panicking here would take down a request path for an impossible case.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor parses an opaque cursor and verifies that it belongs to scope.
func decodeCursor(token, scope string) (cursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursorPayload{}, fmt.Errorf("%w: not valid base64: %v", ErrInvalidCursor, err)
	}
	var payload cursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return cursorPayload{}, fmt.Errorf("%w: not valid JSON: %v", ErrInvalidCursor, err)
	}
	if payload.Scope != scope {
		return cursorPayload{}, fmt.Errorf("%w: cursor belongs to a different query; repeat the first page's from, to and order", ErrInvalidCursor)
	}
	return payload, nil
}

// queryScope builds the binding string for a time-range query. It is deliberately
// coarse: it covers the parameters the contract freezes across pages and nothing
// else, so that a client may still change `limit` between pages.
func queryScope(deviceID string, r TimeRange, order Order) string {
	return fmt.Sprintf("%s|%d|%d|%s", deviceID, r.From.UnixMilli(), r.To.UnixMilli(), order)
}

// cursorTime converts the stored millisecond timestamp back into a UTC time.
func (c cursorPayload) cursorTime() time.Time {
	return time.UnixMilli(c.EventTimeMillis).UTC()
}
