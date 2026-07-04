// Package mdterm incrementally renders a subset of Markdown as ANSI escape
// sequences suitable for a terminal, without buffering the whole response
// (so token-by-token streaming still feels live). It understands
// **bold**, `inline code`, fenced ```code blocks```, ATX #/## headers, and
// "- "/"* " bullet lists; anything else passes through unchanged.
package mdterm

import (
	"bytes"
	"io"
	"os"
)

const (
	ansiBoldOn    = "\x1b[1m"
	ansiBoldOff   = "\x1b[22m"
	ansiCodeOn    = "\x1b[33m"
	ansiCodeOff   = "\x1b[39m"
	ansiFenceOn   = "\x1b[36m"
	ansiFenceOff  = "\x1b[39m"
	ansiHeaderOn  = "\x1b[1;4m"
	ansiHeaderOff = "\x1b[22;24m"
	ansiBulletOn  = "\x1b[34m"
	ansiBulletOff = "\x1b[39m"
	ansiReset     = "\x1b[0m"
)

// maxFenceIndent is CommonMark's limit on how many leading spaces a fence
// marker (opening or closing) may have before it stops counting as one.
const maxFenceIndent = 3

// Renderer incrementally converts a stream of text chunks into ANSI-styled
// output. It must be fed in order via Write/WriteString, and Close must be
// called once the stream ends to flush any buffered remainder and reset
// terminal attributes.
type Renderer struct {
	w         io.Writer
	enabled   bool
	buf       []byte
	lineStart bool
	inBold    bool
	inCode    bool
	inFence   bool
}

// NewRenderer builds a Renderer writing to w. When enabled is false, all
// input passes through unchanged (used when stdout isn't a terminal, or
// NO_COLOR is set).
func NewRenderer(w io.Writer, enabled bool) *Renderer {
	return &Renderer{w: w, enabled: enabled, lineStart: true}
}

// ShouldColor reports whether f looks like a real terminal and NO_COLOR is
// not set, i.e. whether ANSI rendering should be enabled for it.
func ShouldColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

// Write implements io.Writer.
func (r *Renderer) Write(p []byte) (int, error) {
	if !r.enabled {
		return r.w.Write(p)
	}
	r.buf = append(r.buf, p...)
	if err := r.process(); err != nil {
		return 0, err
	}
	return len(p), nil
}

// WriteString is a convenience wrapper around Write for string chunks.
func (r *Renderer) WriteString(s string) (int, error) {
	return r.Write([]byte(s))
}

// Close flushes any buffered, still-ambiguous text as plain literal
// characters (or, inside a fence, as fenced content / a bare unterminated
// closing marker) and resets any open ANSI attribute so it doesn't leak
// into the shell prompt afterward.
func (r *Renderer) Close() error {
	if !r.enabled {
		return nil
	}

	if r.inFence {
		if isClosingFenceLine(r.buf) {
			r.buf = nil
		}
		if len(r.buf) > 0 {
			if _, err := io.WriteString(r.w, string(r.buf)); err != nil {
				return err
			}
			r.buf = nil
		}
		r.inFence = false
		if _, err := io.WriteString(r.w, ansiFenceOff); err != nil {
			return err
		}
		return nil
	}

	if len(r.buf) > 0 {
		if _, err := io.WriteString(r.w, string(r.buf)); err != nil {
			return err
		}
		r.buf = nil
	}
	if r.inBold || r.inCode {
		r.inBold, r.inCode = false, false
		if _, err := io.WriteString(r.w, ansiReset); err != nil {
			return err
		}
	}
	return nil
}

// process consumes as much of r.buf as can be unambiguously resolved,
// writing the transformed output. Any trailing bytes that might still be
// the start of a multi-byte marker are left in r.buf for the next call.
func (r *Renderer) process() error {
	for len(r.buf) > 0 {
		consumed, out, needMore := r.step()
		if needMore {
			return nil
		}
		if out != "" {
			if _, err := io.WriteString(r.w, out); err != nil {
				return err
			}
		}
		r.buf = r.buf[consumed:]
	}
	return nil
}

// step attempts exactly one unit of progress on r.buf (which is always
// non-empty when called). needMore means: stop and wait for more input
// before this position can be resolved.
func (r *Renderer) step() (consumed int, out string, needMore bool) {
	buf := r.buf

	if r.inFence {
		return r.stepFenceBody(buf)
	}

	if r.lineStart {
		if consumed, out, matched, needMore := matchFenceOpen(buf); needMore {
			return 0, "", true
		} else if matched {
			r.inFence = true
			r.lineStart = true
			return consumed, out, false
		}

		switch buf[0] {
		case '#':
			n, complete := leadingRun(buf, '#')
			if !complete && n <= 6 {
				return 0, "", true
			}
			if n >= 1 && n <= 6 && complete && buf[n] == ' ' {
				rest := buf[n+1:]
				nl := bytes.IndexByte(rest, '\n')
				if nl == -1 {
					return 0, "", true
				}
				text := string(rest[:nl])
				r.lineStart = true
				return n + 1 + nl + 1, ansiHeaderOn + text + ansiHeaderOff + "\n", false
			}

		case '-', '*', '+':
			if len(buf) >= 2 && buf[1] == ' ' {
				r.lineStart = false
				return 2, ansiBulletOn + "•" + ansiBulletOff + " ", false
			}
			if len(buf) < 2 {
				return 0, "", true
			}
		}
	}

	if len(buf) >= 2 && buf[0] == '*' && buf[1] == '*' {
		r.inBold = !r.inBold
		r.lineStart = false
		if r.inBold {
			return 2, ansiBoldOn, false
		}
		return 2, ansiBoldOff, false
	}
	if len(buf) == 1 && buf[0] == '*' {
		return 0, "", true
	}

	if buf[0] == '`' {
		r.inCode = !r.inCode
		r.lineStart = false
		if r.inCode {
			return 1, ansiCodeOn, false
		}
		return 1, ansiCodeOff, false
	}

	if buf[0] == '\n' {
		r.lineStart = true
		return 1, "\n", false
	}

	r.lineStart = false
	return 1, string(buf[0]), false
}

// matchFenceOpen tries to match up to maxFenceIndent leading spaces
// followed by a ```-open line at the start of buf. matched=false (with
// needMore=false) means buf definitely isn't a fence open; needMore=true
// means wait for more input before deciding.
func matchFenceOpen(buf []byte) (consumed int, out string, matched, needMore bool) {
	indent := 0
	for indent < len(buf) && indent < maxFenceIndent && buf[indent] == ' ' {
		indent++
	}
	indentComplete := indent < len(buf)
	rest := buf[indent:]

	if bytes.HasPrefix(rest, []byte("```")) {
		afterFence := rest[3:]
		nl := bytes.IndexByte(afterFence, '\n')
		if nl == -1 {
			return 0, "", false, true
		}
		return indent + 3 + nl + 1, string(buf[:indent]) + ansiFenceOn, true, false
	}
	if !indentComplete {
		return 0, "", false, true
	}
	if len(rest) > 0 && len(rest) < 3 && allBackticks(rest) {
		return 0, "", false, true
	}
	return 0, "", false, false
}

// stepFenceBody handles text while inside a fenced code block. It always
// waits for a complete line (ending in '\n') before deciding whether that
// line is the closing marker or ordinary body content, which sidesteps
// any ambiguity from indentation or chunk boundaries.
func (r *Renderer) stepFenceBody(buf []byte) (consumed int, out string, needMore bool) {
	nl := bytes.IndexByte(buf, '\n')
	if nl == -1 {
		return 0, "", true
	}

	line := buf[:nl]
	if isClosingFenceLine(line) {
		r.inFence = false
		r.lineStart = true
		return nl + 1, ansiFenceOff, false
	}

	return nl + 1, string(line) + "\n", false
}

// isClosingFenceLine reports whether line (with no trailing newline) is a
// valid closing fence marker: up to maxFenceIndent leading spaces, then
// ```, then only whitespace (if anything) after that.
func isClosingFenceLine(line []byte) bool {
	i := 0
	for i < len(line) && i < maxFenceIndent && line[i] == ' ' {
		i++
	}
	rest := line[i:]
	if !bytes.HasPrefix(rest, []byte("```")) {
		return false
	}
	for _, c := range rest[3:] {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

func allBackticks(buf []byte) bool {
	for _, b := range buf {
		if b != '`' {
			return false
		}
	}
	return true
}

// leadingRun counts the leading run of c in buf, up to a max of 6.
// complete is false if the whole buffer (up to 6) is c, meaning the run
// might still continue with more input.
func leadingRun(buf []byte, c byte) (n int, complete bool) {
	for n < len(buf) && n < 6 && buf[n] == c {
		n++
	}
	return n, n < len(buf)
}
