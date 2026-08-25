// Command linuxai is a terminal-first CLI assistant for Linux and
// Linux-programming questions.
package main

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"time"

	"linuxai/internal/config"
	"linuxai/internal/history"
	"linuxai/internal/imageutil"
	"linuxai/internal/llm"
	"linuxai/internal/mdterm"
	"linuxai/internal/searxng"
	"linuxai/internal/tui"
)

// replayBudgetTokens caps how much prior thread history is replayed as
// context on each turn (rough estimate: content length / 4).
const replayBudgetTokens = 6000

// threadIdleTimeout starts a new conversation after the current thread has
// gone quiet for this long.
const threadIdleTimeout = 5 * time.Minute

// version is set at build time via -ldflags "-X main.version=...", normally
// from `git describe --tags --always --dirty` (see scripts/package.sh).
// Left as "dev" for a plain `go build` with no ldflags.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "linuxai: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	args, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
	}

	if args.Help {
		printHelp(os.Stdout)
		return nil
	}
	if args.Version {
		fmt.Println("linuxai " + version)
		return nil
	}

	if err := config.LoadDotEnv(); err != nil {
		return fmt.Errorf("loading .env: %w", err)
	}
	webAvailable := config.WebConfigured()
	if args.Web && !webAvailable {
		return fmt.Errorf("web search is not configured; set LINUXAI_SEARXNG_URL in the environment or config .env")
	}

	store, err := history.NewStore()
	if err != nil {
		return err
	}

	// Info commands that don't call the LLM.
	switch {
	case args.List:
		return runList(store)
	case args.SearchGiven:
		return runSearch(store, args.Search)
	}

	threadID, fresh, err := resolveThread(store, args)
	if err != nil {
		return err
	}

	prompt, err := readPrompt(args.Prompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" && stdinIsTerminal() {
		result, err := tui.Run(store, threadID, fresh, args.Web, webAvailable)
		if err != nil {
			return err
		}
		if result.Canceled {
			return nil
		}
		threadID = result.ThreadID
		prompt = result.Prompt
		args.Web = result.Web
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("no prompt given (pass it as an argument or pipe it on stdin)")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	imageDataURL, imageRef, err := loadImage(args.Image)
	if err != nil {
		return err
	}

	sentContent := prompt
	if args.Web {
		if err := cfg.ValidateWeb(); err != nil {
			return err
		}
		results, err := searxng.Search(cfg.SearXNGURL, prompt)
		if err != nil {
			return fmt.Errorf("web search: %w", err)
		}
		sentContent = searxng.GroundingBlock(results, prompt)
	}

	priorMessages, err := store.Load(threadID)
	if err != nil {
		return err
	}
	priorMessages = history.ReplayBudget(priorMessages, replayBudgetTokens)

	messages := buildMessages(cfg.Instructions, priorMessages, sentContent, imageDataURL)

	if err := store.Append(threadID, history.Message{Role: "user", Content: prompt, Image: imageRef}); err != nil {
		return fmt.Errorf("saving message: %w", err)
	}

	client := llm.NewClient(cfg.BaseURL, cfg.APIKey)
	renderer := mdterm.NewRenderer(os.Stdout, mdterm.ShouldColor(os.Stdout))
	var reply strings.Builder
	err = client.StreamChat(cfg.Model, messages, func(token string) {
		renderer.WriteString(token)
		reply.WriteString(token)
	})
	renderer.Close()
	fmt.Println()
	if err != nil {
		return err
	}

	if err := store.Append(threadID, history.Message{Role: "assistant", Content: reply.String()}); err != nil {
		return fmt.Errorf("saving reply: %w", err)
	}

	return nil
}

func buildMessages(instructions string, prior []history.Message, prompt, imageDataURL string) []llm.Message {
	messages := make([]llm.Message, 0, len(prior)+2)
	messages = append(messages, llm.Message{Role: "system", Content: instructions})
	for _, message := range prior {
		messages = append(messages, llm.Message{Role: message.Role, Content: message.Content})
	}
	return append(messages, llm.Message{
		Role:         "user",
		Content:      prompt,
		ImageDataURL: imageDataURL,
	})
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `linuxai - terminal-first assistant for Linux and programming

Usage:
	linuxai [options] [prompt...]
	command | linuxai [options]

Running linuxai without a prompt opens the interactive launcher on a terminal.

Options:
	-n, --new, --new-thread  Start a new conversation
	-l, --list               List saved conversations
	-r, --resume ID          Resume a saved conversation
	-s, --search TERM        Search conversation history
	-w, --web                Ground the answer with web search
	-i, --image PATH         Attach an image file
	-c, --clipboard          Attach an image from the local clipboard
	-v, --version            Print version information
	-h, --help               Show this help

Configuration:
	Local .env:       ./.env (highest file precedence)
	Config directory: ${XDG_CONFIG_HOME:-~/.config}/linuxai
	Config .env:      ${XDG_CONFIG_HOME:-~/.config}/linuxai/.env
	Instructions:     ${XDG_CONFIG_HOME:-~/.config}/linuxai/instructions.txt

Environment / .env keys:
	NVIDIA_API_KEY       NVIDIA hosted endpoint API key
	LINUXAI_BASE_URL     OpenAI-compatible API base URL
	LINUXAI_MODEL        Model name
	LINUXAI_SEARXNG_URL  SearXNG server; required for -w/--web

Process environment values override .env values. If instructions.txt is
missing or blank, linuxai uses its built-in OS/Linux/programming instructions.

Examples:
	linuxai -n how do I check disk usage
	linuxai -r 20260704-143347-b9c9bd explain that another way
	linuxai -w what is the latest stable Linux kernel
`)
}

// resolveThread applies explicit flags and the idle-time rule. fresh reports
// whether the invocation should open directly in a new-thread prompt.
func resolveThread(store *history.Store, args *cliArgs) (id string, fresh bool, err error) {
	return resolveThreadAt(store, args, time.Now())
}

func resolveThreadAt(store *history.Store, args *cliArgs, now time.Time) (id string, created bool, err error) {
	switch {
	case args.New:
		id, err := store.NewThread()
		return id, true, err
	case args.ResumeGiven:
		if !store.ThreadExists(args.Resume) {
			return "", false, fmt.Errorf("no such thread %q", args.Resume)
		}
		if err := store.SetCurrent(args.Resume); err != nil {
			return "", false, err
		}
		return args.Resume, false, nil
	default:
		id, err := store.CurrentThreadID()
		if err != nil {
			return "", false, err
		}
		messages, err := store.Load(id)
		if err != nil {
			return "", false, err
		}
		if len(messages) == 0 {
			return id, true, nil
		}
		modified, err := store.ThreadModified(id)
		if err != nil {
			return "", false, err
		}
		if now.Sub(modified) <= threadIdleTimeout {
			return id, false, nil
		}
		id, err = store.NewThread()
		return id, true, err
	}
}

// loadImage resolves the image source (explicit path, clipboard sentinel,
// or none) into a data URL for the API call and a short reference to store
// in history.
func loadImage(source string) (dataURL, ref string, err error) {
	if source == "" {
		return "", "", nil
	}

	decodeFn := imageutil.LoadFile
	ref = source
	if source == "-" {
		if !imageutil.HasLocalDisplay() {
			return "", "", fmt.Errorf("--clipboard needs a local display; use --image PATH over SSH")
		}
		decodeFn = func(string) (image.Image, error) { return imageutil.LoadClipboard() }
		ref = "(clipboard)"
	}

	img, err := decodeFn(source)
	if err != nil {
		return "", "", err
	}

	dataURL, err = imageutil.ToDataURL(img)
	if err != nil {
		return "", "", err
	}
	return dataURL, ref, nil
}

func runList(store *history.Store) error {
	threads, err := store.List()
	if err != nil {
		return err
	}
	if len(threads) == 0 {
		fmt.Println("No saved threads yet.")
		return nil
	}
	for _, t := range threads {
		fmt.Printf("%s  %s  %s\n", t.ID, t.Modified.Format("2006-01-02 15:04"), t.Title)
	}
	return nil
}

func runSearch(store *history.Store, term string) error {
	results, err := store.Search(term)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No matches.")
		return nil
	}
	for _, r := range results {
		fmt.Printf("%s: %s\n", r.ID, r.Line)
	}
	return nil
}

// readPrompt returns argPrompt if non-empty; otherwise it reads the prompt
// from stdin (useful for piping), returning "" if nothing is piped in.
func readPrompt(argPrompt string) (string, error) {
	if strings.TrimSpace(argPrompt) != "" {
		return argPrompt, nil
	}

	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("checking stdin: %w", err)
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		// No piped input and no args: nothing to read.
		return "", nil
	}

	data, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	return string(data), nil
}

func stdinIsTerminal() bool {
	stat, err := os.Stdin.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
}
