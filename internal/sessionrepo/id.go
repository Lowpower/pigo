package sessionrepo

import (
	"crypto/rand"
	"fmt"
	"time"
)

// NewID returns a UUIDv7 string.
func NewID() string {
	var b [16]byte
	ts := uint64(time.Now().UnixMilli())
	b[0] = byte(ts >> 40)
	b[1] = byte(ts >> 32)
	b[2] = byte(ts >> 24)
	b[3] = byte(ts >> 16)
	b[4] = byte(ts >> 8)
	b[5] = byte(ts)
	_, _ = rand.Read(b[6:])
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
