package mdterm

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// renderAll feeds the entire input through a Renderer in one Write call.
func renderAll(t *testing.T, input string) string {
	t.Helper()
	var buf bytes.Buffer
	r := NewRenderer(&buf, true)
	if _, err := r.WriteString(input); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.String()
}

// renderByteByByte feeds the input one byte at a time, simulating worst-case
// network chunking, to verify the streaming state machine never depends on
// chunk boundaries.
func renderByteByByte(t *testing.T, input string) string {
	t.Helper()
	var buf bytes.Buffer
	r := NewRenderer(&buf, true)
	for i := 0; i < len(input); i++ {
		if _, err := r.WriteString(input[i : i+1]); err != nil {
			t.Fatalf("WriteString: %v", err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.String()
}

func TestBold(t *testing.T) {
	got := renderAll(t, "this is **bold** text")
	want := "this is " + ansiBoldOn + "bold" + ansiBoldOff + " text"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInlineCode(t *testing.T) {
	got := renderAll(t, "run `ls -la` now")
	want := "run " + ansiCodeOn + "ls -la" + ansiCodeOff + " now"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFencedCodeBlock(t *testing.T) {
	got := renderAll(t, "before\n```bash\ndf -h\n```\nafter")
	want := "before\n" + ansiFenceOn + "df -h\n" + ansiFenceOff + "after"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHeader(t *testing.T) {
	got := renderAll(t, "# Title\nbody")
	want := ansiHeaderOn + "Title" + ansiHeaderOff + "\nbody"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHeaderLevelTwo(t *testing.T) {
	got := renderAll(t, "## Subtitle\n")
	want := ansiHeaderOn + "Subtitle" + ansiHeaderOff + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTooManyHashesIsNotAHeader(t *testing.T) {
	got := renderAll(t, "####### not a header\n")
	if got != "####### not a header\n" {
		t.Errorf("got %q, want unchanged plain text", got)
	}
}

func TestHashWithoutSpaceIsNotAHeader(t *testing.T) {
	got := renderAll(t, "#nospace\n")
	if got != "#nospace\n" {
		t.Errorf("got %q, want unchanged plain text", got)
	}
}

func TestBulletList(t *testing.T) {
	got := renderAll(t, "- one\n- two\n")
	want := ansiBulletOn + "•" + ansiBulletOff + " one\n" + ansiBulletOn + "•" + ansiBulletOff + " two\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAsteriskBulletList(t *testing.T) {
	got := renderAll(t, "* item\n")
	want := ansiBulletOn + "•" + ansiBulletOff + " item\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPlainTextPassesThroughUnchanged(t *testing.T) {
	input := "just a normal sentence with no markdown at all."
	if got := renderAll(t, input); got != input {
		t.Errorf("got %q, want unchanged %q", got, input)
	}
}

func TestLoneAsteriskIsLiteral(t *testing.T) {
	got := renderAll(t, "3 * 4 = 12")
	if got != "3 * 4 = 12" {
		t.Errorf("got %q, want unchanged (lone asterisk is not bold)", got)
	}
}

func TestDisabledRendererPassesThroughRaw(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf, false)
	input := "**bold** and `code`"
	if _, err := r.WriteString(input); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.String() != input {
		t.Errorf("got %q, want raw passthrough %q", buf.String(), input)
	}
}

func TestUnterminatedBoldFlushesLiterally(t *testing.T) {
	got := renderAll(t, "some **unterminated")
	want := "some " + ansiBoldOn + "unterminated" + ansiReset
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFenceClosingWithNoTrailingNewlineAtStreamEnd(t *testing.T) {
	// The model's final bytes are exactly the closing ``` marker with no
	// trailing newline (stream just ends there); Close() must still treat
	// it as closed rather than printing literal backticks.
	got := renderAll(t, "before\n```\ncode\n```")
	want := "before\n" + ansiFenceOn + "code\n" + ansiFenceOff
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFenceClosingWithNoTrailingNewlineByteByByte(t *testing.T) {
	whole := renderAll(t, "before\n```\ncode\n```")
	perByte := renderByteByByte(t, "before\n```\ncode\n```")
	if whole != perByte {
		t.Errorf("whole-string = %q, byte-by-byte = %q", whole, perByte)
	}
}

func TestIndentedFencedCodeBlock(t *testing.T) {
	// LLM output commonly indents a fence nested under a numbered/bulleted
	// step, e.g. "1. Do this:\n   ```bash\n   df -h\n   ```\n".
	got := renderAll(t, "   ```bash\n   df -h\n   ```\nafter")
	want := "   " + ansiFenceOn + "   df -h\n" + ansiFenceOff + "after"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestIndentedFenceByteByByte(t *testing.T) {
	input := "1. Step:\n   ```bash\n   df -h\n   more\n   ```\nafter"
	whole := renderAll(t, input)
	perByte := renderByteByByte(t, input)
	if whole != perByte {
		t.Errorf("whole-string = %q, byte-by-byte = %q", whole, perByte)
	}
}

func TestFourSpaceIndentIsNotAFence(t *testing.T) {
	// 4+ leading spaces is an indented code block per CommonMark, not a
	// fence; the renderer must not treat it as a fence-open, which would
	// leave it waiting forever for a matching indented close and color
	// all subsequent output (the rest of the response) as fence body.
	got := renderAll(t, "    ```not a fence\nplain\n")
	if strings.Contains(got, ansiFenceOn) {
		t.Errorf("got %q, incorrectly opened a fenced code block on a 4-space-indented ```", got)
	}
}

func TestByteByByteMatchesWholeString(t *testing.T) {
	inputs := []string{
		"this is **bold** text",
		"run `ls -la` now",
		"before\n```bash\ndf -h\nmore code\n```\nafter",
		"# Title\nbody\n## Sub\nmore",
		"- one\n- two\n* three\n",
		"plain text, no markers, just words.",
		"3 * 4 = 12, not bold",
		"mixed **bold** and `code` and\n# a header\n- a bullet\n```\nfenced\n```\ntail",
	}
	for _, in := range inputs {
		whole := renderAll(t, in)
		perByte := renderByteByByte(t, in)
		if whole != perByte {
			t.Errorf("input %q:\n whole-string  = %q\n byte-by-byte  = %q", in, whole, perByte)
		}
	}
}

func TestShouldColorRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if ShouldColor(os.Stdout) {
		t.Error("ShouldColor should be false when NO_COLOR is set")
	}
}

func TestShouldColorFalseForNonTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	f, err := os.CreateTemp(t.TempDir(), "notatty")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()
	if ShouldColor(f) {
		t.Error("ShouldColor should be false for a regular file")
	}
}
