package models

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// profilesAssign matches the `_PROFILES: dict[...] = ` line that introduces
// the literal in the upstream file.
var profilesAssign = regexp.MustCompile(`(?m)^_PROFILES\b[^=\n]*=[ \t]*`)

// ParseProfiles extracts the _PROFILES dict from the upstream Python source.
// Only literal values are accepted; anything else is a parse error.
func ParseProfiles(src []byte) (map[string]Profile, error) {
	loc := profilesAssign.FindIndex(src)
	if loc == nil {
		return nil, fmt.Errorf("no _PROFILES assignment found")
	}

	p := &pyParser{src: string(src), pos: loc[1]}
	value, err := p.parseValue()
	if err != nil {
		return nil, err
	}

	// Round-tripping through JSON reuses the struct tags for field mapping.
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding profiles: %w", err)
	}
	profiles := map[string]Profile{}
	if err := json.Unmarshal(encoded, &profiles); err != nil {
		return nil, fmt.Errorf("decoding profiles: %w", err)
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("_PROFILES is empty")
	}
	return profiles, nil
}

// pyParser reads the subset of Python literal syntax used by the profile
// data: dicts, lists, strings, ints, floats, True/False/None.
type pyParser struct {
	src string
	pos int
}

func (p *pyParser) parseValue() (any, error) {
	p.skipIgnorable()
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.parseDict()
	case c == '[' || c == '(':
		return p.parseList()
	case c == '\'' || c == '"':
		return p.parseString()
	case c == '-' || c == '+' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return p.parseKeyword()
	}
}

func (p *pyParser) parseDict() (any, error) {
	p.pos++ // consume '{'
	out := map[string]any{}
	for {
		p.skipIgnorable()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated dict")
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return out, nil
		}
		key, err := p.parseString()
		if err != nil {
			return nil, fmt.Errorf("dict key: %w", err)
		}
		p.skipIgnorable()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return nil, fmt.Errorf("expected ':' after key %q", key)
		}
		p.pos++
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		out[key] = value
		if !p.consumeSeparator('}') {
			return nil, fmt.Errorf("expected ',' or '}' after key %q", key)
		}
	}
}

func (p *pyParser) parseList() (any, error) {
	closer := byte(']')
	if p.src[p.pos] == '(' {
		closer = ')'
	}
	p.pos++
	out := []any{}
	for {
		p.skipIgnorable()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated list")
		}
		if p.src[p.pos] == closer {
			p.pos++
			return out, nil
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		out = append(out, value)
		if !p.consumeSeparator(closer) {
			return nil, fmt.Errorf("expected ',' or '%c' in list", closer)
		}
	}
}

// consumeSeparator accepts a comma, or reports success without advancing when
// the next token closes the container (so trailing commas are tolerated).
func (p *pyParser) consumeSeparator(closer byte) bool {
	p.skipIgnorable()
	if p.pos >= len(p.src) {
		return false
	}
	switch p.src[p.pos] {
	case ',':
		p.pos++
		return true
	case closer:
		return true
	default:
		return false
	}
}

func (p *pyParser) parseString() (string, error) {
	p.skipIgnorable()
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("unexpected end of input")
	}
	quote := p.src[p.pos]
	if quote != '\'' && quote != '"' {
		return "", fmt.Errorf("expected a string at offset %d", p.pos)
	}
	p.pos++

	var out strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch c {
		case quote:
			p.pos++
			return out.String(), nil
		case '\\':
			p.pos++
			if p.pos >= len(p.src) {
				return "", fmt.Errorf("unterminated escape")
			}
			switch e := p.src[p.pos]; e {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'r':
				out.WriteByte('\r')
			default:
				out.WriteByte(e)
			}
			p.pos++
		default:
			out.WriteByte(c)
			p.pos++
		}
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *pyParser) parseNumber() (any, error) {
	start := p.pos
	for p.pos < len(p.src) && strings.IndexByte("+-0123456789._eE", p.src[p.pos]) >= 0 {
		p.pos++
	}
	text := strings.ReplaceAll(p.src[start:p.pos], "_", "")
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number %q", text)
	}
	return f, nil
}

func (p *pyParser) parseKeyword() (any, error) {
	start := p.pos
	for p.pos < len(p.src) && (isWordByte(p.src[p.pos])) {
		p.pos++
	}
	switch word := p.src[start:p.pos]; word {
	case "True":
		return true, nil
	case "False":
		return false, nil
	case "None":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported token %q at offset %d", word, start)
	}
}

// skipIgnorable advances past whitespace and # comments.
func (p *pyParser) skipIgnorable() {
	for p.pos < len(p.src) {
		switch c := p.src[p.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case c == '#':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
