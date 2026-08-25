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

func TestItalic(t *testing.T) {
	input := "this is *italic* text"
	want := "this is " + ansiItalicOn + "italic" + ansiItalicOff + " text"
	if got := renderAll(t, input); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := renderByteByByte(t, input); got != want {
		t.Errorf("byte-by-byte = %q, want %q", got, want)
	}
}

func TestInlineCode(t *testing.T) {
	got := renderAll(t, "run `ls -la` now")
	want := "run " + ansiCodeOn + "ls -la" + ansiCodeOff + " now"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownMarkersInsideCodeAndGlobAreLiteral(t *testing.T) {
	input := "run `locate *.log && echo **done**`; glob *.txt"
	want := "run " + ansiCodeOn + "locate *.log && echo **done**" + ansiCodeOff + "; glob *.txt"
	if got := renderAll(t, input); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := renderByteByByte(t, input); got != want {
		t.Errorf("byte-by-byte = %q, want %q", got, want)
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

func TestFormattedHeaderAtStreamEnd(t *testing.T) {
	input := "## **Bold** and *italic*"
	want := ansiHeaderOn + ansiBoldOn + "Bold" + ansiBoldOff + ansiBoldOn + " and " + ansiItalicOn + "italic" + ansiItalicOff + ansiHeaderOff
	if got := renderAll(t, input); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := renderByteByByte(t, input); got != want {
		t.Errorf("byte-by-byte = %q, want %q", got, want)
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

func TestUTF8PassesThroughAcrossChunkBoundaries(t *testing.T) {
	input := "the file’s ‘real’ path — café"
	if got := renderAll(t, input); got != input {
		t.Errorf("whole-string = %q, want %q", got, input)
	}
	if got := renderByteByByte(t, input); got != input {
		t.Errorf("byte-by-byte = %q, want %q", got, input)
	}
}

func TestTable(t *testing.T) {
	input := "| What you want | Command |\n" +
		"|----------------|---------|\n" +
		"| Find a file by name (recursively) | find / -name \"myfile.txt\" 2>/dev/null |\n" +
		"| Quick DB lookup | locate myfile.txt |\n"
	want := ansiTableOn + "┌" + strings.Repeat("─", 35) + "┬" + strings.Repeat("─", 39) + "┐" + ansiTableOff + "\n" +
		ansiTableOn + "│" + ansiTableOff + " " + ansiBoldOn + "What you want" + strings.Repeat(" ", 20) + ansiBoldOff + " " + ansiTableOn + "│" + ansiTableOff + " " + ansiBoldOn + "Command" + strings.Repeat(" ", 30) + ansiBoldOff + " " + ansiTableOn + "│" + ansiTableOff + "\n" +
		ansiTableOn + "├" + strings.Repeat("─", 35) + "┼" + strings.Repeat("─", 39) + "┤" + ansiTableOff + "\n" +
		ansiTableOn + "│" + ansiTableOff + " Find a file by name (recursively) " + ansiTableOn + "│" + ansiTableOff + " find / -name \"myfile.txt\" 2>/dev/null " + ansiTableOn + "│" + ansiTableOff + "\n" +
		ansiTableOn + "│" + ansiTableOff + " Quick DB lookup" + strings.Repeat(" ", 19) + ansiTableOn + "│" + ansiTableOff + " locate myfile.txt" + strings.Repeat(" ", 21) + ansiTableOn + "│" + ansiTableOff + "\n" +
		ansiTableOn + "└" + strings.Repeat("─", 35) + "┴" + strings.Repeat("─", 39) + "┘" + ansiTableOff + "\n"
	if got := renderAll(t, input); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := renderByteByByte(t, input); got != want {
		t.Errorf("byte-by-byte = %q, want %q", got, want)
	}
}

func TestTableWithoutTrailingNewline(t *testing.T) {
	input := "| Name | Value |\n|---|---:|\n| café | 42 |"
	whole := renderAll(t, input)
	perByte := renderByteByByte(t, input)
	if whole != perByte || !strings.Contains(whole, "café") {
		t.Errorf("whole-string = %q, byte-by-byte = %q", whole, perByte)
	}
}

func TestFormattedTableCellsAndCodeSpanPipe(t *testing.T) {
	input := "| Platform | Example |\n" +
		"|---|---|\n" +
		"| **Linux / macOS** | `Get-ChildItem | Select-String 'error'` |\n" +
		"| *Cross-platform* | `locate *.log` |\n"
	got := renderAll(t, input)
	if perByte := renderByteByByte(t, input); perByte != got {
		t.Errorf("whole-string = %q, byte-by-byte = %q", got, perByte)
	}
	if !strings.Contains(got, ansiBoldOn+"Linux / macOS"+ansiBoldOff) {
		t.Errorf("bold table cell was not formatted: %q", got)
	}
	if !strings.Contains(got, ansiItalicOn+"Cross-platform"+ansiItalicOff) {
		t.Errorf("italic table cell was not formatted: %q", got)
	}
	if !strings.Contains(got, ansiCodeOn+"Get-ChildItem | Select-String 'error'"+ansiCodeOff) {
		t.Errorf("code span containing a pipe was split or not formatted: %q", got)
	}
	if !strings.Contains(got, ansiCodeOn+"locate *.log"+ansiCodeOff) {
		t.Errorf("asterisk inside code span was treated as Markdown: %q", got)
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
