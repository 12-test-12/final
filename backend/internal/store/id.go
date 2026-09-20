package store

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// crockford is the Crockford base32 alphabet used by ULID. It omits I, L, O and
// U so that identifiers stay unambiguous when read aloud or copied by hand.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// idState holds the per-process monotonic state that makes identifiers created
// in the same millisecond still sort in creation order.
//
// Without it, two alerts raised in the same millisecond would carry independent
// random suffixes and could sort in either order, which would make cursor
// pagination over alert events skip or repeat rows.
var idState = struct {
	mu      sync.Mutex
	ms      int64
	entropy [10]byte
}{}

// newID returns a 26-character, time-ordered identifier: 48 bits of Unix
// milliseconds followed by 80 bits that increase monotonically within each
// millisecond. Sorting the identifiers as strings reproduces creation order.
func newID(now time.Time) string {
	ms := now.UnixMilli()

	idState.mu.Lock()
	switch {
	case ms > idState.ms:
		// A new millisecond: start a fresh random suffix.
		idState.ms = ms
		refillEntropy()
	default:
		// Same millisecond, or a clock that moved backwards. Keeping the larger
		// recorded millisecond and bumping the suffix preserves ordering either
		// way, which matters because ordering is what the cursor relies on.
		if !incrementEntropy() {
			// The 80-bit suffix wrapped. Re-randomising is the honest response:
			// the alternative would be reusing an identifier.
			refillEntropy()
		}
	}
	entropy := idState.entropy
	ms = idState.ms
	idState.mu.Unlock()

	var raw [16]byte
	binary.BigEndian.PutUint64(raw[0:8], uint64(ms)<<16)
	copy(raw[6:16], entropy[:])
	return encodeBase32(raw)
}

// refillEntropy draws a fresh random suffix. Callers must hold idState.mu.
func refillEntropy() {
	if _, err := rand.Read(idState.entropy[:]); err != nil {
		// crypto/rand is documented never to fail on a supported platform. If it
		// somehow does, falling back to a counter-derived value keeps identifiers
		// unique and ordered, which is the property the rest of the system needs;
		// unpredictability is not a requirement for an alert identifier.
		next := nextMonotonic()
		binary.BigEndian.PutUint64(idState.entropy[2:10], uint64(next))
	}
}

// incrementEntropy adds one to the suffix as an 80-bit big-endian integer. It
// reports false when the value wrapped. Callers must hold idState.mu.
func incrementEntropy() bool {
	for index := len(idState.entropy) - 1; index >= 0; index-- {
		idState.entropy[index]++
		if idState.entropy[index] != 0 {
			return true
		}
	}
	return false
}

// nextMonotonic is the last-resort fallback sequence for entropy refills.
var monotonicCounter struct {
	sync.Mutex
	value uint64
}

// nextMonotonic returns the next value of the fallback sequence.
func nextMonotonic() uint64 {
	monotonicCounter.Lock()
	defer monotonicCounter.Unlock()

	monotonicCounter.value++
	return monotonicCounter.value
}

// encodeBase32 renders 128 bits as 26 Crockford base32 characters, most
// significant first, so that lexicographic order matches numeric order.
func encodeBase32(raw [16]byte) string {
	const encodedLen = 26
	out := make([]byte, encodedLen)
	for index := encodedLen - 1; index >= 0; index-- {
		out[index] = crockford[raw[15]&0x1F]
		shiftRight5(&raw)
	}
	return string(out)
}

// shiftRight5 shifts a 128-bit big-endian value right by five bits, which is one
// base32 digit.
func shiftRight5(raw *[16]byte) {
	var carry byte
	for index := 0; index < 16; index++ {
		shiftedOut := raw[index] & 0x1F
		raw[index] = raw[index]>>5 | carry<<3
		carry = shiftedOut
	}
}
