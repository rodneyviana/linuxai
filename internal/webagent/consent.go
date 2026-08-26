package webagent

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Consent prompts for non-whitelisted web origins and remembers explicit
// session approvals. It is safe for repeated calls in one tool loop.
type Consent struct {
	mu       sync.Mutex
	input    *bufio.Reader
	output   io.Writer
	closer   io.Closer
	approved map[string]bool
}

// NewConsent creates a consent prompt over supplied streams, primarily for
// tests and embedding. A nil input denies all non-whitelisted origins.
func NewConsent(input io.Reader, output io.Writer) *Consent {
	var reader *bufio.Reader
	if input != nil {
		reader = bufio.NewReader(input)
	}
	return &Consent{input: reader, output: output, approved: make(map[string]bool)}
}

// NewTTYConsent opens /dev/tty so piped stdin remains reserved for the user
// prompt. If no controlling terminal exists, unknown origins are denied.
func NewTTYConsent(fallback io.Writer) *Consent {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return NewConsent(nil, fallback)
	}
	consent := NewConsent(tty, tty)
	consent.closer = tty
	return consent
}

// Close releases the controlling terminal opened by NewTTYConsent.
func (c *Consent) Close() error {
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}

// Authorize implements webread.Authorizer.
func (c *Consent) Authorize(origin, rawURL string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.approved[origin] {
		return true
	}
	if c.input == nil {
		if c.output != nil {
			fmt.Fprintf(c.output, "Web read denied for %s: no interactive terminal for authorization.\n", origin)
		}
		return false
	}

	fmt.Fprintf(c.output, "\nWeb access authorization required\nURL: %s\n[o] allow once  [s] allow origin for session  [d] deny: ", rawURL)
	answer, err := c.input.ReadString('\n')
	if err != nil && len(answer) == 0 {
		fmt.Fprintln(c.output, "denied")
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "o", "once", "y", "yes":
		return true
	case "s", "session":
		c.approved[origin] = true
		return true
	default:
		return false
	}
}
