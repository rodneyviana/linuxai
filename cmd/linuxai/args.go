package main

import (
	"fmt"
	"strings"
)

// cliArgs holds the parsed command line: known flags plus every other
// token, joined back together as the prompt so users never need to quote
// their question.
type cliArgs struct {
	New     bool
	List    bool
	Web     bool
	Version bool
	Help    bool
	Resume  string // thread id; ResumeGiven distinguishes "" from unset
	Search  string // search term; SearchGiven distinguishes "" from unset
	Image   string // path from --image

	ResumeGiven bool
	SearchGiven bool

	Prompt string
}

// parseArgs recognizes long options and their one-letter aliases wherever they
// appear, and joins every other token, in order, into the prompt.
func parseArgs(args []string) (*cliArgs, error) {
	a := &cliArgs{}
	var promptParts []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--new", "--new-thread", "-n":
			a.New = true
		case "--list", "-l":
			a.List = true
		case "--web", "-w":
			a.Web = true
		case "--version", "-v":
			a.Version = true
		case "--help", "-h":
			a.Help = true
		case "--clipboard", "-c":
			a.Image = "-" // sentinel: read from clipboard instead of a path
		case "--resume", "-r":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("%s requires a thread id", args[i-1])
			}
			a.Resume = args[i]
			a.ResumeGiven = true
		case "--search", "-s":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("%s requires a search term", args[i-1])
			}
			a.Search = args[i]
			a.SearchGiven = true
		case "--image", "-i":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("%s requires a file path", args[i-1])
			}
			a.Image = args[i]
		default:
			promptParts = append(promptParts, args[i])
		}
	}

	a.Prompt = strings.Join(promptParts, " ")

	const webPrefix = "/web "
	if strings.HasPrefix(a.Prompt, webPrefix) {
		a.Web = true
		a.Prompt = strings.TrimPrefix(a.Prompt, webPrefix)
	}

	return a, nil
}
