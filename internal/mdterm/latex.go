package mdterm

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

var latexSymbols = map[string]string{
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ",
	"epsilon": "ε", "theta": "θ", "lambda": "λ", "mu": "μ",
	"pi": "π", "rho": "ρ", "sigma": "σ", "phi": "φ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ",
	"Pi": "Π", "Sigma": "Σ", "Phi": "Φ", "Omega": "Ω",
	"pm": "±", "mp": "∓", "times": "×", "cdot": "·", "div": "÷",
	"le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "ne": "≠",
	"neq": "≠", "approx": "≈", "equiv": "≡", "in": "∈", "notin": "∉",
	"subset": "⊂", "subseteq": "⊆", "supset": "⊃", "supseteq": "⊇",
	"to": "→", "rightarrow": "→", "leftarrow": "←", "Rightarrow": "⇒",
	"infty": "∞", "sum": "∑", "prod": "∏", "int": "∫", "partial": "∂",
	"nabla": "∇", "forall": "∀", "exists": "∃", "land": "∧", "lor": "∨",
}

var superscripts = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵',
	'6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹', '+': '⁺', '-': '⁻',
	'=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ',
}

var subscripts = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅',
	'6': '₆', '7': '₇', '8': '₈', '9': '₉', '+': '₊', '-': '₋',
	'=': '₌', '(': '₍', ')': '₎', 'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ',
	'i': 'ᵢ', 'j': 'ⱼ', 'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ',
	'o': 'ₒ', 'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ',
	'v': 'ᵥ', 'x': 'ₓ',
}

func matchMath(buf []byte) (consumed int, body string, matched, needMore bool) {
	if len(buf) == 0 {
		return 0, "", false, false
	}
	var opening, closing []byte
	inline := false
	switch {
	case buf[0] == '$':
		if len(buf) == 1 {
			return 0, "", false, true
		}
		opening, closing, inline = []byte("$"), []byte("$"), true
		if buf[1] == '$' {
			opening, closing, inline = []byte("$$"), []byte("$$"), false
		}
	case buf[0] == '\\':
		if len(buf) == 1 {
			return 0, "", false, true
		}
		switch buf[1] {
		case '(':
			opening, closing, inline = []byte(`\(`), []byte(`\)`), true
		case '[':
			opening, closing, inline = []byte(`\[`), []byte(`\]`), false
		default:
			return 0, "", false, false
		}
	default:
		return 0, "", false, false
	}

	start := len(opening)
	for position := start; position < len(buf); position++ {
		if inline && buf[position] == '\n' {
			return 1, "", false, false
		}
		if bytes.HasPrefix(buf[position:], closing) && (opening[0] == '\\' || !escapedAt(buf, position)) {
			if position == start || opening[0] == '$' && (unicode.IsSpace(rune(buf[start])) || unicode.IsSpace(rune(buf[position-1]))) {
				return 1, "", false, false
			}
			return position + len(closing), string(buf[start:position]), true, false
		}
	}
	return 0, "", false, true
}

func escapedAt(text []byte, position int) bool {
	backslashes := 0
	for position--; position >= 0 && text[position] == '\\'; position-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func formatLatex(input string) string {
	parser := latexParser{input: input}
	return parser.parse(0)
}

type latexParser struct {
	input    string
	position int
}

func (p *latexParser) parse(stop byte) string {
	var out strings.Builder
	for p.position < len(p.input) {
		char := p.input[p.position]
		if stop != 0 && char == stop {
			p.position++
			break
		}
		switch char {
		case '\\':
			out.WriteString(p.parseCommand())
		case '{':
			p.position++
			out.WriteString(p.parse('}'))
		case '^', '_':
			p.position++
			value := p.parseArgument()
			if converted, ok := convertScript(value, char == '^'); ok {
				out.WriteString(converted)
			} else {
				out.WriteByte(char)
				out.WriteByte('(')
				out.WriteString(value)
				out.WriteByte(')')
			}
		default:
			_, size := utf8.DecodeRuneInString(p.input[p.position:])
			out.WriteString(p.input[p.position : p.position+size])
			p.position += size
		}
	}
	return out.String()
}

func (p *latexParser) parseCommand() string {
	p.position++
	start := p.position
	for p.position < len(p.input) && unicode.IsLetter(rune(p.input[p.position])) {
		p.position++
	}
	if start == p.position {
		if p.position >= len(p.input) {
			return "\\"
		}
		char := p.input[p.position]
		p.position++
		switch char {
		case ',', ':', ';', ' ':
			return " "
		case '!':
			return ""
		}
		return string(char)
	}
	command := p.input[start:p.position]
	if symbol, ok := latexSymbols[command]; ok {
		return symbol
	}
	switch command {
	case "frac":
		numerator := p.parseArgument()
		denominator := p.parseArgument()
		return parenthesizeFractionPart(numerator) + "⁄" + parenthesizeFractionPart(denominator)
	case "sqrt":
		value := p.parseArgument()
		if utf8.RuneCountInString(value) == 1 {
			return "√" + value
		}
		return "√(" + value + ")"
	case "text", "textrm", "mathrm", "mathbf", "mathit", "operatorname", "boxed":
		return p.parseArgument()
	case "left", "right":
		return ""
	case "quad":
		p.skipSpaces()
		return " "
	case "qquad":
		p.skipSpaces()
		return "  "
	case "mathbb":
		return blackboardBold(p.parseArgument())
	}
	return "\\" + command
}

func (p *latexParser) skipSpaces() {
	for p.position < len(p.input) && p.input[p.position] == ' ' {
		p.position++
	}
}

func (p *latexParser) parseArgument() string {
	for p.position < len(p.input) && unicode.IsSpace(rune(p.input[p.position])) {
		p.position++
	}
	if p.position >= len(p.input) {
		return ""
	}
	if p.input[p.position] == '{' {
		p.position++
		return p.parse('}')
	}
	if p.input[p.position] == '\\' {
		return p.parseCommand()
	}
	_, size := utf8.DecodeRuneInString(p.input[p.position:])
	value := p.input[p.position : p.position+size]
	p.position += size
	return value
}

func convertScript(value string, superscript bool) (string, bool) {
	table := subscripts
	if superscript {
		table = superscripts
	}
	var out strings.Builder
	for _, char := range value {
		converted, ok := table[char]
		if !ok {
			return "", false
		}
		out.WriteRune(converted)
	}
	return out.String(), true
}

func parenthesizeFractionPart(value string) string {
	if utf8.RuneCountInString(value) <= 1 {
		return value
	}
	return "(" + value + ")"
}

func blackboardBold(value string) string {
	return strings.NewReplacer(
		"C", "ℂ", "H", "ℍ", "N", "ℕ", "P", "ℙ", "Q", "ℚ", "R", "ℝ", "Z", "ℤ",
	).Replace(value)
}
