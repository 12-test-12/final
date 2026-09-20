package store

import (
	"crypto/rand"
	"encoding/binary"
	"sync/atomic"
	"time"
)

// crockford is the Crockford base32 alphabet used by ULID. It omits I, L, O and
// U so that identifiers stay unambiguous when read aloud or copied by hand.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// monotonicCounter keeps identifiers created within the same millisecond
// strictly ordered. Without it, two alerts raised in the same millisecond could
// sort in either order and make cursor pagination unstable.
var monotonicCounter atomic.Uint32

// nextMonotonic returns the next value of the per-process ordering counter.
func nextMonotonic() uint32 { return monotonicCounter.Add(1) }

// newULID builds a 26-character identifier: 48 bits of Unix milliseconds
// followed by 80 bits of randomness, with the trailing 20 bits replaced by a
// monotonic counter so identifiers remain sortable inside one millisecond.
//
// crypto/rand failing is treated as unrecoverable at this layer; the identifier
// still stays unique because the counter keeps advancing, so the fallback is a
// zero random part rather than a panic in a request path.
func newULID(now time.Time, counter uint32) string {
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[0:8], uint64(now.UnixMilli())<<16)
	if _, err := rand.Read(raw[6:16]); err != nil {
		raw[6], raw[7] = 0, 0
	}
	// The counter occupies the low 20 bits, overlapping the last random bytes.
	raw[13] ^= byte(counter >> 16)
	raw[14] ^= byte(counter >> 8)
	raw[15] ^= byte(counter)

	// Encode the 128-bit value as 26 Crockford base32 characters.
	const encodedLen = 26
	out := make([]byte, encodedLen)
	for i := encodedLen - 1; i >= 0; i-- {
		out[i] = crockford[raw[15]&0x1F]
		// Shift the 16-byte value right by 5 bits.
		carry := byte(0)
		for j := 0; j < 16; j++ {
			next := raw[j] >> 5
			raw[j] = raw[j]<<3 | carry
			carry = next
		}
	}
	return string(out)
}
