// Package mdterm incrementally renders a subset of Markdown as ANSI escape
// sequences suitable for a terminal, without buffering the whole response
// (so token-by-token streaming still feels live). It understands
// **bold**, *italic*, `inline code`, fenced ```code blocks```, ATX headers,
// "- "/"* " bullet lists, pipe tables, and LaTeX math spans; anything else
// passes through unchanged.
package mdterm

import (
	"bytes"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ansiBoldOn    = "\x1b[1m"
	ansiBoldOff   = "\x1b[22m"
	ansiItalicOn  = "\x1b[3m"
	ansiItalicOff = "\x1b[23m"
	ansiCodeOn    = "\x1b[33m"
	ansiCodeOff   = "\x1b[39m"
	ansiFenceOn   = "\x1b[36m"
	ansiFenceOff  = "\x1b[39m"
	ansiHeaderOn  = "\x1b[1;4m"
	ansiHeaderOff = "\x1b[22;24m"
	ansiBulletOn  = "\x1b[34m"
	ansiBulletOff = "\x1b[39m"
	ansiTableOn   = "\x1b[34m"
	ansiTableOff  = "\x1b[39m"
	ansiMathOn    = "\x1b[36m"
	ansiMathOff   = "\x1b[39m"
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
	inItalic  bool
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
			if _, err := r.w.Write(r.buf); err != nil {
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

	if r.lineStart && len(r.buf) > 0 && r.buf[0] == '|' {
		if consumed, out, matched, _ := matchTable(r.buf, true); matched {
			if _, err := io.WriteString(r.w, out); err != nil {
				return err
			}
			r.buf = r.buf[consumed:]
		}
	}
	if r.lineStart && len(r.buf) > 0 && r.buf[0] == '#' {
		if consumed, out, matched := matchHeader(r.buf, true); matched {
			if _, err := io.WriteString(r.w, out); err != nil {
				return err
			}
			r.buf = r.buf[consumed:]
		}
	}
	if len(r.buf) > 0 {
		if _, err := r.w.Write(r.buf); err != nil {
			return err
		}
		r.buf = nil
	}
	if r.inBold || r.inItalic || r.inCode {
		r.inBold, r.inItalic, r.inCode = false, false, false
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
	if r.inCode && buf[0] != '`' {
		if buf[0] == '\n' {
			r.lineStart = true
			return 1, "\n", false
		}
		if !utf8.FullRune(buf) {
			return 0, "", true
		}
		_, size := utf8.DecodeRune(buf)
		r.lineStart = false
		return size, string(buf[:size]), false
	}

	if r.lineStart {
		if consumed, out, matched, needMore := matchFenceOpen(buf); needMore {
			return 0, "", true
		} else if matched {
			r.inFence = true
			r.lineStart = true
			return consumed, out, false
		}
		if buf[0] == '|' {
			if consumed, out, matched, needMore := matchTable(buf, false); needMore {
				return 0, "", true
			} else if matched {
				r.lineStart = true
				return consumed, out, false
			}
		}

		switch buf[0] {
		case '#':
			if consumed, out, matched := matchHeader(buf, false); matched {
				r.lineStart = true
				return consumed, out, false
			} else if consumed == 0 {
				return 0, "", true
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
	if buf[0] == '*' {
		if !r.inItalic && len(buf) >= 2 && (buf[1] == ' ' || buf[1] == '\t' || buf[1] == '\n') {
			r.lineStart = false
			return 1, "*", false
		}
		if !r.inItalic {
			next, _ := utf8.DecodeRune(buf[1:])
			if !unicode.IsLetter(next) && !unicode.IsNumber(next) {
				r.lineStart = false
				return 1, "*", false
			}
		}
		r.inItalic = !r.inItalic
		r.lineStart = false
		if r.inItalic {
			return 1, ansiItalicOn, false
		}
		return 1, ansiItalicOff, false
	}

	if buf[0] == '`' {
		r.inCode = !r.inCode
		r.lineStart = false
		if r.inCode {
			return 1, ansiCodeOn, false
		}
		return 1, ansiCodeOff, false
	}

	if buf[0] == '$' || buf[0] == '\\' {
		if consumed, math, matched, needMore := matchMath(buf); matched {
			r.lineStart = false
			return consumed, ansiMathOn + formatLatex(math) + ansiMathOff, false
		} else if needMore {
			return 0, "", true
		}
	}

	if buf[0] == '\n' {
		r.lineStart = true
		return 1, "\n", false
	}

	if !utf8.FullRune(buf) {
		return 0, "", true
	}
	_, size := utf8.DecodeRune(buf)
	r.lineStart = false
	return size, string(buf[:size]), false
}

func matchHeader(buf []byte, final bool) (consumed int, out string, matched bool) {
	n, complete := leadingRun(buf, '#')
	if !complete && n <= 6 {
		return 0, "", false
	}
	if n < 1 || n > 6 || !complete || buf[n] != ' ' {
		return 1, "", false
	}
	rest := buf[n+1:]
	newline := bytes.IndexByte(rest, '\n')
	if newline < 0 && !final {
		return 0, "", false
	}
	if newline < 0 {
		newline = len(rest)
		consumed = len(buf)
	} else {
		consumed = n + 1 + newline + 1
	}
	text, _ := renderInline(string(rest[:newline]), true)
	suffix := "\n"
	if final && consumed == len(buf) {
		suffix = ""
	}
	return consumed, ansiHeaderOn + text + ansiHeaderOff + suffix, true
}

func matchTable(buf []byte, final bool) (consumed int, out string, matched, needMore bool) {
	headerLine, next, complete := tableLine(buf, 0, final)
	if !complete {
		return 0, "", false, true
	}
	header, ok := parseTableRow(headerLine)
	if !ok {
		return 0, "", false, false
	}

	separatorLine, next, complete := tableLine(buf, next, final)
	if !complete {
		return 0, "", false, true
	}
	separator, ok := parseTableRow(separatorLine)
	if !ok || len(separator) != len(header) || !isTableSeparator(separator) {
		return 0, "", false, false
	}

	rows := [][]string{header}
	position := next
	for position < len(buf) {
		if buf[position] != '|' {
			break
		}
		line, lineEnd, complete := tableLine(buf, position, final)
		if !complete {
			return 0, "", false, true
		}
		row, ok := parseTableRow(line)
		if !ok {
			break
		}
		rows = append(rows, row)
		position = lineEnd
	}
	if !final && position == len(buf) {
		return 0, "", false, true
	}

	return position, renderTable(rows), true, false
}

func tableLine(buf []byte, start int, final bool) (line []byte, next int, complete bool) {
	if start >= len(buf) {
		return nil, start, false
	}
	if newline := bytes.IndexByte(buf[start:], '\n'); newline >= 0 {
		end := start + newline
		return buf[start:end], end + 1, true
	}
	if final {
		return buf[start:], len(buf), true
	}
	return nil, start, false
}

func parseTableRow(line []byte) ([]string, bool) {
	trimmed := strings.TrimSpace(string(line))
	if len(trimmed) < 2 || trimmed[0] != '|' || trimmed[len(trimmed)-1] != '|' {
		return nil, false
	}
	var parts []string
	var cell strings.Builder
	inCode := false
	mathClose := ""
	escaped := false
	for position := 1; position < len(trimmed)-1; {
		char, size := utf8.DecodeRuneInString(trimmed[position:])
		switch {
		case escaped:
			cell.WriteRune(char)
			escaped = false
			position += size
		case !inCode && mathClose == "" && char == '\\' && position+1 < len(trimmed)-1 && (trimmed[position+1] == '(' || trimmed[position+1] == '['):
			if trimmed[position+1] == '(' {
				mathClose = `\)`
			} else {
				mathClose = `\]`
			}
			cell.WriteString(trimmed[position : position+2])
			position += 2
		case !inCode && mathClose != "" && strings.HasPrefix(trimmed[position:], mathClose):
			cell.WriteString(mathClose)
			position += len(mathClose)
			mathClose = ""
		case char == '\\':
			escaped = true
			cell.WriteRune(char)
			position += size
		case char == '`':
			inCode = !inCode
			cell.WriteRune(char)
			position += size
		case char == '$' && !inCode && (mathClose == "" || mathClose[0] == '$'):
			delimiter := 1
			if position+1 < len(trimmed)-1 && trimmed[position+1] == '$' {
				delimiter = 2
			}
			if mathClose == "" {
				mathClose = strings.Repeat("$", delimiter)
			} else if len(mathClose) == delimiter {
				mathClose = ""
			}
			cell.WriteString(trimmed[position : position+delimiter])
			position += delimiter
		case char == '|' && !inCode && mathClose == "":
			parts = append(parts, strings.TrimSpace(cell.String()))
			cell.Reset()
			position += size
		default:
			cell.WriteRune(char)
			position += size
		}
	}
	parts = append(parts, strings.TrimSpace(cell.String()))
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts, len(parts) > 0
}

func isTableSeparator(cells []string) bool {
	for _, cell := range cells {
		cell = strings.TrimPrefix(cell, ":")
		cell = strings.TrimSuffix(cell, ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func renderTable(rows [][]string) string {
	columns := len(rows[0])
	widths := make([]int, columns)
	for _, row := range rows {
		for column := 0; column < columns && column < len(row); column++ {
			_, width := renderInline(row[column], false)
			if width > widths[column] {
				widths[column] = width
			}
		}
	}

	var out strings.Builder
	writeTableBorder(&out, "┌", "┬", "┐", widths)
	writeTableRow(&out, rows[0], widths, true)
	writeTableBorder(&out, "├", "┼", "┤", widths)
	for _, row := range rows[1:] {
		writeTableRow(&out, row, widths, false)
	}
	writeTableBorder(&out, "└", "┴", "┘", widths)
	return out.String()
}

func writeTableBorder(out *strings.Builder, left, middle, right string, widths []int) {
	out.WriteString(ansiTableOn)
	out.WriteString(left)
	for column, width := range widths {
		out.WriteString(strings.Repeat("─", width+2))
		if column < len(widths)-1 {
			out.WriteString(middle)
		}
	}
	out.WriteString(right)
	out.WriteString(ansiTableOff)
	out.WriteByte('\n')
}

func writeTableRow(out *strings.Builder, row []string, widths []int, header bool) {
	out.WriteString(ansiTableOn + "│" + ansiTableOff)
	for column, width := range widths {
		cell := ""
		if column < len(row) {
			cell = row[column]
		}
		out.WriteByte(' ')
		if header {
			out.WriteString(ansiBoldOn)
		}
		formatted, visibleWidth := renderInline(cell, header)
		out.WriteString(formatted)
		out.WriteString(strings.Repeat(" ", width-visibleWidth))
		if header {
			out.WriteString(ansiBoldOff)
		}
		out.WriteString(" " + ansiTableOn + "│" + ansiTableOff)
	}
	out.WriteByte('\n')
}

func renderInline(text string, baseBold bool) (string, int) {
	var out strings.Builder
	visibleWidth := 0
	inBold := false
	inItalic := false
	inCode := false
	for position := 0; position < len(text); {
		if inCode && text[position] != '`' {
			_, size := utf8.DecodeRuneInString(text[position:])
			out.WriteString(text[position : position+size])
			visibleWidth++
			position += size
			continue
		}
		switch {
		case strings.HasPrefix(text[position:], "**") && (inBold || strings.Contains(text[position+2:], "**")):
			inBold = !inBold
			if inBold {
				out.WriteString(ansiBoldOn)
			} else {
				out.WriteString(ansiBoldOff)
				if baseBold {
					out.WriteString(ansiBoldOn)
				}
			}
			position += 2
		case text[position] == '*' && (inItalic || hasItalicClose(text[position+1:])):
			inItalic = !inItalic
			if inItalic {
				out.WriteString(ansiItalicOn)
			} else {
				out.WriteString(ansiItalicOff)
			}
			position++
		case text[position] == '`' && (inCode || strings.ContainsRune(text[position+1:], '`')):
			inCode = !inCode
			if inCode {
				out.WriteString(ansiCodeOn)
			} else {
				out.WriteString(ansiCodeOff)
			}
			position++
		case text[position] == '$' || text[position] == '\\':
			consumed, math, matched, _ := matchMath([]byte(text[position:]))
			if !matched {
				if text[position] == '$' || position+1 >= len(text) {
					out.WriteByte(text[position])
					visibleWidth++
					position++
					continue
				}
				_, size := utf8.DecodeRuneInString(text[position+1:])
				out.WriteString(text[position+1 : position+1+size])
				visibleWidth++
				position += 1 + size
				continue
			}
			formatted := formatLatex(math)
			out.WriteString(ansiMathOn)
			out.WriteString(formatted)
			out.WriteString(ansiMathOff)
			if baseBold {
				out.WriteString(ansiBoldOn)
			}
			visibleWidth += utf8.RuneCountInString(formatted)
			position += consumed
		default:
			_, size := utf8.DecodeRuneInString(text[position:])
			out.WriteString(text[position : position+size])
			visibleWidth++
			position += size
		}
	}
	if inBold || inItalic || inCode {
		out.WriteString(ansiReset)
		if baseBold {
			out.WriteString(ansiBoldOn)
		}
	}
	return out.String(), visibleWidth
}

func hasItalicClose(text string) bool {
	for position := 0; position < len(text); position++ {
		if text[position] == '*' && (position+1 >= len(text) || text[position+1] != '*') {
			return true
		}
	}
	return false
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
