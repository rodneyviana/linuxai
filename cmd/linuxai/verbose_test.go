package main

import (
	"testing"
)

func TestParseArgsVerbose(t *testing.T) {
	for _, flag := range []string{"--verbose", "-V"} {
		a, err := parseArgs([]string{flag, "why"})
		if err != nil {
			t.Fatalf("parseArgs %s: %v", flag, err)
		}
		if !a.Verbose {
			t.Errorf("%s did not set Verbose", flag)
		}
		if a.Version {
			t.Errorf("%s must not be treated as --version", flag)
		}
		if a.Prompt != "why" {
			t.Errorf("%s: Prompt = %q, want the flag stripped", flag, a.Prompt)
		}
	}
}

// -v predates --verbose and must keep meaning --version.
func TestParseArgsShortVStillMeansVersion(t *testing.T) {
	a, err := parseArgs([]string{"-v"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !a.Version || a.Verbose {
		t.Errorf("parseArgs(-v) = %+v, want Version without Verbose", a)
	}
}
