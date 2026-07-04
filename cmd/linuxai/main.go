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

	"linuxai/internal/config"
	"linuxai/internal/history"
	"linuxai/internal/imageutil"
	"linuxai/internal/llm"
	"linuxai/internal/mdterm"
	"linuxai/internal/searxng"
)

// replayBudgetTokens caps how much prior thread history is replayed as
// context on each turn (rough estimate: content length / 4).
const replayBudgetTokens = 6000

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "linuxai: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(); err != nil {
		return fmt.Errorf("loading .env: %w", err)
	}

	args, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
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

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	threadID, err := resolveThread(store, args)
	if err != nil {
		return err
	}

	prompt, err := readPrompt(args.Prompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("no prompt given (pass it as an argument or pipe it on stdin)")
	}

	imageDataURL, imageRef, err := loadImage(args.Image)
	if err != nil {
		return err
	}

	sentContent := prompt
	if args.Web {
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

	messages := make([]llm.Message, 0, len(priorMessages)+1)
	for _, m := range priorMessages {
		messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, llm.Message{
		Role:         "user",
		Content:      sentContent,
		ImageDataURL: imageDataURL,
	})

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

// resolveThread applies --new / --resume / bare-continue semantics and
// returns the thread id to use for this invocation.
func resolveThread(store *history.Store, args *cliArgs) (string, error) {
	switch {
	case args.New:
		return store.NewThread()
	case args.ResumeGiven:
		if !store.ThreadExists(args.Resume) {
			return "", fmt.Errorf("no such thread %q", args.Resume)
		}
		if err := store.SetCurrent(args.Resume); err != nil {
			return "", err
		}
		return args.Resume, nil
	default:
		return store.CurrentThreadID()
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
