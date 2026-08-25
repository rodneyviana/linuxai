// Package history implements append-only JSONL chat sessions under
// ~/.local/share/linuxai/chats/, with a "current" pointer file naming the
// active thread.
package history

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Message is one line of a thread's JSONL file.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Image   string `json:"image,omitempty"`
	TS      int64  `json:"ts"`
}

// Store manages thread files rooted at a data directory
// (~/.local/share/linuxai by default).
type Store struct {
	dir string // .../linuxai
}

// NewStore builds a Store rooted at ~/.local/share/linuxai.
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home dir: %w", err)
	}
	dir := filepath.Join(home, ".local", "share", "linuxai")
	return &Store{dir: dir}, nil
}

func (s *Store) chatsDir() string {
	return filepath.Join(s.dir, "chats")
}

func (s *Store) currentPointerPath() string {
	return filepath.Join(s.dir, "current")
}

func (s *Store) threadPath(id string) string {
	return filepath.Join(s.chatsDir(), id+".jsonl")
}

// NewThreadID generates a sortable, collision-resistant thread id.
func NewThreadID() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating thread id: %w", err)
	}
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(buf), nil
}

// NewThread creates an empty thread file, points "current" at it, and
// returns its id.
func (s *Store) NewThread() (string, error) {
	if err := os.MkdirAll(s.chatsDir(), 0o755); err != nil {
		return "", fmt.Errorf("creating chats dir: %w", err)
	}
	id, err := NewThreadID()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(s.threadPath(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("creating thread file: %w", err)
	}
	f.Close()

	if err := s.SetCurrent(id); err != nil {
		return "", err
	}
	return id, nil
}

// CurrentThreadID returns the id of the active thread, creating a new one
// if none exists yet.
func (s *Store) CurrentThreadID() (string, error) {
	data, err := os.ReadFile(s.currentPointerPath())
	if err != nil {
		if os.IsNotExist(err) {
			return s.NewThread()
		}
		return "", fmt.Errorf("reading current pointer: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return s.NewThread()
	}
	if _, err := os.Stat(s.threadPath(id)); err != nil {
		if os.IsNotExist(err) {
			return s.NewThread()
		}
		return "", err
	}
	return id, nil
}

// SetCurrent repoints "current" at the given thread id.
func (s *Store) SetCurrent(id string) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	return os.WriteFile(s.currentPointerPath(), []byte(id), 0o644)
}

// ThreadExists reports whether a thread with the given id exists.
func (s *Store) ThreadExists(id string) bool {
	_, err := os.Stat(s.threadPath(id))
	return err == nil
}

// ThreadModified returns the last activity time for a thread. Appending any
// message updates the thread file's modification time.
func (s *Store) ThreadModified(id string) (time.Time, error) {
	info, err := os.Stat(s.threadPath(id))
	if err != nil {
		return time.Time{}, fmt.Errorf("checking thread %q: %w", id, err)
	}
	return info.ModTime(), nil
}

// Append writes one message to the end of a thread's JSONL file.
func (s *Store) Append(id string, msg Message) error {
	if msg.TS == 0 {
		msg.TS = time.Now().Unix()
	}
	if err := os.MkdirAll(s.chatsDir(), 0o755); err != nil {
		return fmt.Errorf("creating chats dir: %w", err)
	}
	f, err := os.OpenFile(s.threadPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening thread file: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	return nil
}

// Load reads every message in a thread, in order.
func (s *Store) Load(id string) ([]Message, error) {
	f, err := os.Open(s.threadPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening thread file: %w", err)
	}
	defer f.Close()

	var messages []Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // skip malformed lines rather than fail the whole load
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading thread file: %w", err)
	}
	return messages, nil
}

// ReplayBudget replays messages under an approximate token budget by
// keeping the most recent messages and dropping the oldest ones once the
// budget is exceeded. Token count is estimated as len(content)/4.
func ReplayBudget(messages []Message, maxTokens int) []Message {
	if maxTokens <= 0 {
		return messages
	}
	total := 0
	cut := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		total += len(messages[i].Content)/4 + 1
		if total > maxTokens {
			cut = i + 1
			break
		}
		cut = i
	}
	return messages[cut:]
}

// ThreadSummary is a one-line description of a thread, used by List.
type ThreadSummary struct {
	ID       string
	Title    string
	Modified time.Time
}

// List returns a summary of every thread, most recently modified first.
func (s *Store) List() ([]ThreadSummary, error) {
	entries, err := os.ReadDir(s.chatsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading chats dir: %w", err)
	}

	var summaries []ThreadSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		info, err := entry.Info()
		if err != nil {
			continue
		}
		summaries = append(summaries, ThreadSummary{
			ID:       id,
			Title:    s.firstUserMessage(id),
			Modified: info.ModTime(),
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Modified.After(summaries[j].Modified)
	})
	return summaries, nil
}

func (s *Store) firstUserMessage(id string) string {
	messages, err := s.Load(id)
	if err != nil {
		return "(unreadable)"
	}
	for _, m := range messages {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			title := strings.TrimSpace(m.Content)
			title = strings.SplitN(title, "\n", 2)[0]
			const maxLen = 72
			if len(title) > maxLen {
				title = title[:maxLen] + "..."
			}
			return title
		}
	}
	return "(empty)"
}

// SearchResult is one match from Search.
type SearchResult struct {
	ID   string
	Line string
}

// Search greps every thread's content for term (case-insensitive) and
// returns matching lines with their thread id.
func (s *Store) Search(term string) ([]SearchResult, error) {
	entries, err := os.ReadDir(s.chatsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading chats dir: %w", err)
	}

	needle := strings.ToLower(term)
	var results []SearchResult
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		messages, err := s.Load(id)
		if err != nil {
			continue
		}
		for _, m := range messages {
			if strings.Contains(strings.ToLower(m.Content), needle) {
				snippet := strings.TrimSpace(strings.SplitN(m.Content, "\n", 2)[0])
				results = append(results, SearchResult{ID: id, Line: snippet})
			}
		}
	}
	return results, nil
}
