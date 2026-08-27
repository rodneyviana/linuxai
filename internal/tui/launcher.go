// Package tui provides the interactive launcher shown when linuxai is invoked
// without a prompt on a terminal.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"linuxai/internal/history"
)

type Result struct {
	ThreadID string
	Prompt   string
	Web      bool
	Canceled bool
}

type screen int

const (
	menuScreen screen = iota
	promptScreen
	threadsScreen
	searchScreen
	resultsScreen
	settingsScreen
	modelSearchScreen
	modelCardScreen
)

var (
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	titleStyle    = lipgloss.NewStyle().Bold(true)
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("39")).Padding(1, 2)
)

type model struct {
	store        *history.Store
	currentID    string
	screen       screen
	previous     screen
	menuCursor   int
	listCursor   int
	width        int
	height       int
	web          bool
	webAvailable bool
	prompt       textarea.Model
	search       textinput.Model
	settings     settingsState
	picker       pickerState
	threads      []history.ThreadSummary
	results      []history.SearchResult
	result       Result
	err          error
}

func Run(store *history.Store, currentID string, fresh, web, webAvailable bool) (Result, error) {
	m, err := newModel(store, currentID, fresh, web, webAvailable)
	if err != nil {
		return Result{}, err
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Result{}, fmt.Errorf("running interface: %w", err)
	}
	finished := final.(model)
	if finished.err != nil {
		return Result{}, finished.err
	}
	return finished.result, nil
}

func newModel(store *history.Store, currentID string, fresh, web, webAvailable bool) (model, error) {
	prompt := textarea.New()
	prompt.Placeholder = "Ask a Linux or programming question"
	prompt.ShowLineNumbers = false
	prompt.SetWidth(66)
	prompt.SetHeight(5)

	search := textinput.New()
	search.Placeholder = "Search saved conversations"
	search.CharLimit = 120
	search.Width = 60

	threads, err := store.List()
	if err != nil {
		return model{}, err
	}
	screen := menuScreen
	if fresh {
		screen = promptScreen
		prompt.Focus()
	}
	return model{
		store:        store,
		currentID:    currentID,
		screen:       screen,
		prompt:       prompt,
		search:       search,
		settings:     newSettingsState(),
		picker:       newPickerState(),
		threads:      threads,
		web:          web,
		webAvailable: webAvailable,
		result:       Result{ThreadID: currentID},
		width:        76,
		height:       18,
	}, nil
}

func (m model) Init() tea.Cmd {
	if m.screen == promptScreen {
		return textarea.Blink
	}
	return nil
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := message.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.resizeInputs()
		return m, nil
	}

	key, isKey := message.(tea.KeyMsg)
	if isKey && key.String() == "ctrl+c" {
		m.result.Canceled = true
		return m, tea.Quit
	}

	switch m.screen {
	case menuScreen:
		return m.updateMenu(key, isKey)
	case promptScreen:
		return m.updatePrompt(message, key, isKey)
	case threadsScreen:
		return m.updateThreads(key, isKey)
	case searchScreen:
		return m.updateSearch(message, key, isKey)
	case resultsScreen:
		return m.updateResults(key, isKey)
	case settingsScreen:
		return m.updateSettings(message, key, isKey)
	case modelSearchScreen:
		return m.updatePicker(message, key, isKey)
	case modelCardScreen:
		return m.updateModelCard(key, isKey)
	default:
		return m, nil
	}
}

func (m model) updateMenu(key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	if !isKey {
		return m, nil
	}
	items := menuItems()
	switch key.String() {
	case "up", "k":
		m.menuCursor = moveCursor(m.menuCursor, -1, len(items))
	case "down", "j":
		m.menuCursor = moveCursor(m.menuCursor, 1, len(items))
	case "esc", "q":
		m.result.Canceled = true
		return m, tea.Quit
	case "enter":
		switch m.menuCursor {
		case 0:
			return m.openPrompt()
		case 1:
			return m.newThread()
		case 2:
			m.refreshThreads()
			m.screen = threadsScreen
			m.listCursor = 0
		case 3:
			m.previous = menuScreen
			m.screen = searchScreen
			m.search.Focus()
			return m, textinput.Blink
		case 4:
			return m.openSettings()
		case 5:
			m.result.Canceled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) updatePrompt(message tea.Msg, key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	if isKey {
		switch key.String() {
		case "ctrl+s":
			prompt := strings.TrimSpace(m.prompt.Value())
			if prompt != "" {
				m.result.ThreadID = m.currentID
				m.result.Prompt = prompt
				m.result.Web = m.web
				return m, tea.Quit
			}
			return m, nil
		case "ctrl+n":
			return m.newThread()
		case "ctrl+w":
			if m.webAvailable {
				m.web = !m.web
			}
			return m, nil
		case "esc":
			m.prompt.Blur()
			m.screen = menuScreen
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(message)
	return m, cmd
}

func (m model) updateThreads(key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	if !isKey {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		m.listCursor = moveCursor(m.listCursor, -1, len(m.threads))
	case "down", "j":
		m.listCursor = moveCursor(m.listCursor, 1, len(m.threads))
	case "esc":
		m.screen = menuScreen
	case "/":
		m.previous = threadsScreen
		m.screen = searchScreen
		m.search.Focus()
		return m, textinput.Blink
	case "enter":
		if len(m.threads) == 0 {
			return m, nil
		}
		id := m.threads[m.listCursor].ID
		if err := m.store.SetCurrent(id); err != nil {
			m.err = err
			return m, tea.Quit
		}
		m.currentID = id
		m.result.ThreadID = id
		return m.openPrompt()
	}
	return m, nil
}

func (m model) updateSearch(message tea.Msg, key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	if isKey {
		switch key.String() {
		case "esc":
			m.search.Blur()
			m.screen = m.previous
			return m, nil
		case "enter":
			results, err := m.store.Search(strings.TrimSpace(m.search.Value()))
			if err != nil {
				m.err = err
				return m, tea.Quit
			}
			m.results = results
			m.listCursor = 0
			m.search.Blur()
			m.screen = resultsScreen
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(message)
	return m, cmd
}

func (m model) updateResults(key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	if !isKey {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		m.listCursor = moveCursor(m.listCursor, -1, len(m.results))
	case "down", "j":
		m.listCursor = moveCursor(m.listCursor, 1, len(m.results))
	case "esc":
		m.screen = searchScreen
		m.search.Focus()
		return m, textinput.Blink
	case "enter":
		if len(m.results) == 0 {
			return m, nil
		}
		id := m.results[m.listCursor].ID
		if err := m.store.SetCurrent(id); err != nil {
			m.err = err
			return m, tea.Quit
		}
		m.currentID = id
		m.result.ThreadID = id
		return m.openPrompt()
	}
	return m, nil
}

func (m model) openPrompt() (tea.Model, tea.Cmd) {
	m.screen = promptScreen
	m.prompt.Focus()
	return m, textarea.Blink
}

func (m model) newThread() (tea.Model, tea.Cmd) {
	id, err := m.store.NewThread()
	if err != nil {
		m.err = err
		return m, tea.Quit
	}
	m.currentID = id
	m.result.ThreadID = id
	m.prompt.Reset()
	m.refreshThreads()
	return m.openPrompt()
}

func (m *model) refreshThreads() {
	threads, err := m.store.List()
	if err != nil {
		m.err = err
		return
	}
	m.threads = threads
}

func (m *model) resizeInputs() {
	width := m.contentWidth() - 4
	if width < 24 {
		width = 24
	}
	m.prompt.SetWidth(width)
	m.search.Width = width
	// Settings rows are indented two columns and each input renders a
	// two-column prompt, so they get less room than the plain inputs.
	fieldWidth := m.contentWidth() - 8
	if fieldWidth < 20 {
		fieldWidth = 20
	}
	m.picker.query.Width = fieldWidth
	for index := range m.settings.fields {
		m.settings.fields[index].Width = fieldWidth
	}
}

func (m model) View() string {
	var body string
	switch m.screen {
	case menuScreen:
		body = m.menuView()
	case promptScreen:
		body = m.promptView()
	case threadsScreen:
		body = m.threadsView()
	case searchScreen:
		body = m.searchView()
	case resultsScreen:
		body = m.resultsView()
	case settingsScreen:
		body = m.settingsView()
	case modelSearchScreen:
		body = m.modelSearchView()
	case modelCardScreen:
		body = m.modelCardView()
	}
	return boxStyle.Width(m.contentWidth()).Render(body)
}

func (m model) menuView() string {
	var out strings.Builder
	writeLine(&out, titleStyle.Render("linuxai"))
	out.WriteByte('\n')
	if current := m.currentThread(); current != nil {
		writeLine(&out, mutedStyle.Render("Current thread"))
		writeLine(&out, current.Title)
		writeLine(&out, mutedStyle.Render("Last activity: "+relativeTime(current.Modified)))
		out.WriteByte('\n')
	}
	for index, item := range menuItems() {
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == m.menuCursor {
			prefix = "› "
			style = selectedStyle
		}
		writeLine(&out, style.Render(prefix+item))
	}
	out.WriteByte('\n')
	out.WriteString(mutedStyle.Render("↑/↓ move   Enter select   Esc quit"))
	return out.String()
}

func (m model) promptView() string {
	title := "New thread"
	if current := m.currentThread(); current != nil && current.Title != "(empty)" {
		title = current.Title
	}
	web := "[ ] Search web"
	webHelp := "Ctrl+W web"
	if !m.webAvailable {
		web = mutedStyle.Render("[-] Search web unavailable (configure SearXNG)")
		webHelp = ""
	} else if m.web {
		web = "[x] Search web"
	}
	help := strings.Join(filterNonEmpty([]string{"Ctrl+S send", "Ctrl+N new chat", webHelp, "Esc menu"}), "   ")
	return titleStyle.Render("linuxai · "+title) + "\n\n" +
		accentStyle.Render("Ask") + "\n" + m.prompt.View() + "\n" +
		web + "\n\n" + mutedStyle.Render(help)
}

func (m model) threadsView() string {
	var out strings.Builder
	writeLine(&out, titleStyle.Render("Resume thread"))
	out.WriteByte('\n')
	if len(m.threads) == 0 {
		writeLine(&out, mutedStyle.Render("No saved threads"))
	}
	for index, thread := range visibleThreads(m.threads, m.listCursor, m.visibleRows()) {
		actual := visibleStart(len(m.threads), m.listCursor, m.visibleRows()) + index
		prefix := "  "
		style := lipgloss.NewStyle()
		if actual == m.listCursor {
			prefix = "› "
			style = selectedStyle
		}
		line := fmt.Sprintf("%-48s %s", truncate(thread.Title, 48), relativeTime(thread.Modified))
		writeLine(&out, style.Render(prefix+line))
	}
	out.WriteByte('\n')
	out.WriteString(mutedStyle.Render("↑/↓ move   Enter resume   / search   Esc back"))
	return out.String()
}

func (m model) searchView() string {
	return titleStyle.Render("Search history") + "\n\n" + m.search.View() + "\n\n" +
		mutedStyle.Render("Enter search   Esc back")
}

func (m model) resultsView() string {
	var out strings.Builder
	writeLine(&out, titleStyle.Render("Search results"))
	out.WriteByte('\n')
	if len(m.results) == 0 {
		writeLine(&out, mutedStyle.Render("No matches"))
	}
	start := visibleStart(len(m.results), m.listCursor, m.visibleRows())
	end := start + m.visibleRows()
	if end > len(m.results) {
		end = len(m.results)
	}
	for index := start; index < end; index++ {
		result := m.results[index]
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == m.listCursor {
			prefix = "› "
			style = selectedStyle
		}
		writeLine(&out, style.Render(prefix+truncate(result.Line, m.contentWidth()-6)))
	}
	out.WriteByte('\n')
	out.WriteString(mutedStyle.Render("↑/↓ move   Enter resume   Esc search"))
	return out.String()
}

func (m model) currentThread() *history.ThreadSummary {
	for index := range m.threads {
		if m.threads[index].ID == m.currentID {
			return &m.threads[index]
		}
	}
	return nil
}

func (m model) contentWidth() int {
	width := m.width - 8
	if width < 40 {
		return 40
	}
	if width > 76 {
		return 76
	}
	return width
}

func (m model) visibleRows() int {
	rows := m.height - 10
	if rows < 3 {
		return 3
	}
	return rows
}

func menuItems() []string {
	return []string{"Continue current thread", "New chat", "Resume another thread", "Search history", "Settings", "Quit"}
}

func moveCursor(cursor, delta, length int) int {
	if length == 0 {
		return 0
	}
	cursor = (cursor + delta) % length
	if cursor < 0 {
		cursor += length
	}
	return cursor
}

func visibleStart(length, cursor, rows int) int {
	if length <= rows || cursor < rows {
		return 0
	}
	start := cursor - rows + 1
	if start+rows > length {
		return length - rows
	}
	return start
}

func visibleThreads(threads []history.ThreadSummary, cursor, rows int) []history.ThreadSummary {
	start := visibleStart(len(threads), cursor, rows)
	end := start + rows
	if end > len(threads) {
		end = len(threads)
	}
	return threads[start:end]
}

func relativeTime(when time.Time) string {
	delta := time.Since(when)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%d min ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(delta.Hours()))
	default:
		return when.Format("Jan 2")
	}
}

func truncate(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func writeLine(out *strings.Builder, value string) {
	out.WriteString(value)
	out.WriteByte('\n')
}

func filterNonEmpty(values []string) []string {
	filtered := values[:0]
	for _, value := range values {
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
