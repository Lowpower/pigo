package shell

import "strings"

// ANSIStripper removes complete CSI and OSC sequences from a stream.
// An escape that has not reached its terminator is held and joined to the
// next Write. Flush emits anything that is still unfinished.
type ANSIStripper struct {
	pending string
}

type ansiKind int

const (
	ansiNone ansiKind = iota
	ansiDone
	ansiMore
)

// Write returns the text that is safe to emit. A trailing incomplete escape
// is kept for the next call.
func (s *ANSIStripper) Write(chunk string) string {
	in := s.pending + chunk
	s.pending = ""
	var b strings.Builder
	for i := 0; i < len(in); {
		if !ansiIntro(in, i) {
			b.WriteByte(in[i])
			i++
			continue
		}
		n, kind := skipANSI(in, i)
		switch kind {
		case ansiDone:
			i = n
		case ansiMore:
			s.pending = in[i:]
			return b.String()
		default:
			b.WriteByte(in[i])
			i++
		}
	}
	return b.String()
}

// Flush finishes the stream. A sequence that is still incomplete is emitted
// as text.
func (s *ANSIStripper) Flush() string {
	out := s.Write("")
	tail := s.pending
	s.pending = ""
	return out + tail
}

func ansiIntro(s string, i int) bool {
	switch s[i] {
	case 0x1b:
		return true
	case 0x90, 0x98, 0x9b, 0x9d, 0x9e, 0x9f:
		return utf8Boundary(s, i)
	default:
		return false
	}
}

func skipANSI(s string, i int) (int, ansiKind) {
	switch s[i] {
	case 0x1b:
		if i+1 >= len(s) {
			return 0, ansiMore
		}
		switch s[i+1] {
		case '[':
			return skipCSI(s, i+2)
		case ']', 'P', 'X', '^', '_':
			return skipUntilST(s, i+2)
		default:
			return skipESCFinal(s, i+1)
		}
	case 0x9b:
		return skipCSI(s, i+1)
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
		return skipUntilST(s, i+1)
	default:
		return 0, ansiNone
	}
}

func skipCSI(s string, j int) (int, ansiKind) {
	for ; j < len(s); j++ {
		c := s[j]
		if c >= 0x40 && c <= 0x7e {
			return j + 1, ansiDone
		}
		if c < 0x20 || c > 0x3f {
			return 0, ansiNone
		}
	}
	return 0, ansiMore
}

func skipUntilST(s string, j int) (int, ansiKind) {
	for ; j < len(s); j++ {
		switch s[j] {
		case 0x07, 0x9c:
			return j + 1, ansiDone
		case 0x1b:
			if j+1 >= len(s) {
				return 0, ansiMore
			}
			if s[j+1] == '\\' {
				return j + 2, ansiDone
			}
		}
	}
	return 0, ansiMore
}

func skipESCFinal(s string, j int) (int, ansiKind) {
	for ; j < len(s); j++ {
		c := s[j]
		if c >= 0x30 && c <= 0x7e {
			return j + 1, ansiDone
		}
		if c < 0x20 || c > 0x2f {
			return 0, ansiNone
		}
	}
	return 0, ansiMore
}

// utf8Boundary reports whether i is not in the middle of a UTF-8 character.
// C1 bytes are also UTF-8 continuations, so a raw C1 introducer is only
// recognized at a character boundary.
func utf8Boundary(s string, i int) bool {
	if i == 0 || s[i]&0xc0 != 0x80 {
		return true
	}
	j := i - 1
	for j > i-4 && j >= 0 && s[j]&0xc0 == 0x80 {
		j--
	}
	if j < 0 || s[j]&0x80 == 0 {
		return true
	}
	var want int
	switch lead := s[j]; {
	case lead&0xe0 == 0xc0:
		want = 2
	case lead&0xf0 == 0xe0:
		want = 3
	case lead&0xf8 == 0xf0:
		want = 4
	default:
		return true
	}
	return i-j >= want
}
