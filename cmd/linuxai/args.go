package main

import (
	"fmt"
	"strings"
)

// cliArgs holds the parsed command line: known flags plus every other
// token, joined back together as the prompt so users never need to quote
// their question.
type cliArgs struct {
	New    bool
	List   bool
	Web    bool
	Resume string // thread id; ResumeGiven distinguishes "" from unset
	Search string // search term; SearchGiven distinguishes "" from unset
	Image  string // path from --image

	ResumeGiven bool
	SearchGiven bool

	Prompt string
}

// parseArgs recognizes --new, --list, --web, --clipboard, --resume <id>,
// --search <term>, and --image <path> wherever they appear in args, and
// joins every other token, in order, into the prompt.
func parseArgs(args []string) (*cliArgs, error) {
	a := &cliArgs{}
	var promptParts []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--new":
			a.New = true
		case "--list":
			a.List = true
		case "--web":
			a.Web = true
		case "--clipboard":
			a.Image = "-" // sentinel: read from clipboard instead of a path
		case "--resume":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--resume requires a thread id")
			}
			a.Resume = args[i]
			a.ResumeGiven = true
		case "--search":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--search requires a search term")
			}
			a.Search = args[i]
			a.SearchGiven = true
		case "--image":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--image requires a file path")
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
