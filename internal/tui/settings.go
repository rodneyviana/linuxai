package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"linuxai/internal/config"
	"linuxai/internal/history"
	"linuxai/internal/models"
)

// Rows on the settings screen: the four editable values, then the actions.
const (
	fieldAPIKey = iota
	fieldBaseURL
	fieldModel
	fieldSearXNG
	fieldCount

	rowSearch = fieldCount
	rowSave   = fieldCount + 1
	rowCount  = fieldCount + 2
)

var settingsKeys = [fieldCount]string{
	fieldAPIKey:  config.KeyAPIKey,
	fieldBaseURL: config.KeyBaseURL,
	fieldModel:   config.KeyModel,
	fieldSearXNG: config.KeySearXNG,
}

var settingsLabels = [fieldCount]string{
	fieldAPIKey:  "API key",
	fieldBaseURL: "Base URL",
	fieldModel:   "Model",
	fieldSearXNG: "SearXNG URL",
}

type settingsState struct {
	fields [fieldCount]textinput.Model
	cursor int
	path   string
	status string
	only   bool
}

type pickerState struct {
	query      textinput.Model
	entries    []models.Entry
	view       []models.Entry
	cursor     int
	filter     models.Filter
	order      models.Order
	status     string
	liveLoaded bool
	card       models.Entry
}

type liveModelsMsg struct {
	ids []string
	err error
}

type catalogUpdateMsg struct {
	result models.UpdateResult
	err    error
}

// RunSettings opens the launcher directly on the settings dialog.
func RunSettings(store *history.Store) error {
	m, err := newModel(store, "", false, false, config.WebConfigured())
	if err != nil {
		return err
	}
	m.settings.only = true
	m.screen = settingsScreen
	m.settings.fields[m.settings.cursor].Focus()

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("running settings: %w", err)
	}
	return final.(model).err
}

func newSettingsState() settingsState {
	state := settingsState{}
	if path, err := config.EnvPath(); err == nil {
		state.path = path
	}
	values, err := config.ReadEnvFile(state.path)
	if err != nil {
		values = map[string]string{}
	}

	placeholders := [fieldCount]string{
		fieldAPIKey:  "nvapi-…",
		fieldBaseURL: config.DefaultBaseURL,
		fieldModel:   config.DefaultModel,
		fieldSearXNG: "http://localhost:8080",
	}
	for index := range state.fields {
		input := textinput.New()
		input.Placeholder = placeholders[index]
		input.CharLimit = 240
		input.Width = 52
		input.SetValue(values[settingsKeys[index]])
		if index == fieldAPIKey {
			input.EchoMode = textinput.EchoPassword
			input.EchoCharacter = '•'
		}
		state.fields[index] = input
	}
	return state
}

func newPickerState() pickerState {
	query := textinput.New()
	query.Placeholder = "Filter by name"
	query.CharLimit = 80
	query.Width = 52
	return pickerState{query: query}
}

func (m model) openSettings() (tea.Model, tea.Cmd) {
	m.settings = newSettingsState()
	m.screen = settingsScreen
	m.settings.fields[m.settings.cursor].Focus()
	return m, textinput.Blink
}

func (m model) updateSettings(message tea.Msg, key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	if isKey {
		switch key.String() {
		case "esc":
			if m.settings.only {
				m.result.Canceled = true
				return m, tea.Quit
			}
			m.blurSettings()
			m.screen = menuScreen
			return m, nil
		case "ctrl+s":
			return m.saveSettings()
		case "up", "shift+tab":
			return m.moveSettingsCursor(-1)
		case "down", "tab":
			return m.moveSettingsCursor(1)
		case "enter":
			switch m.settings.cursor {
			case rowSearch:
				return m.openPicker()
			case rowSave:
				return m.saveSettings()
			default:
				return m.moveSettingsCursor(1)
			}
		}
	}

	if m.settings.cursor < fieldCount {
		var cmd tea.Cmd
		m.settings.fields[m.settings.cursor], cmd = m.settings.fields[m.settings.cursor].Update(message)
		return m, cmd
	}
	return m, nil
}

func (m model) moveSettingsCursor(delta int) (tea.Model, tea.Cmd) {
	m.blurSettings()
	m.settings.cursor = moveCursor(m.settings.cursor, delta, rowCount)
	if m.settings.cursor < fieldCount {
		m.settings.fields[m.settings.cursor].Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m *model) blurSettings() {
	for index := range m.settings.fields {
		m.settings.fields[index].Blur()
	}
}

func (m model) saveSettings() (tea.Model, tea.Cmd) {
	if m.settings.path == "" {
		m.settings.status = "Cannot resolve the config directory"
		return m, nil
	}
	updates := map[string]string{}
	for index, key := range settingsKeys {
		updates[key] = strings.TrimSpace(m.settings.fields[index].Value())
	}
	if err := config.WriteEnvFile(m.settings.path, updates); err != nil {
		m.settings.status = "Save failed: " + err.Error()
		return m, nil
	}
	config.ApplyEnv(updates)
	m.webAvailable = config.WebConfigured()
	if !m.webAvailable {
		m.web = false
	}
	m.settings.status = "Saved to " + m.settings.path
	if m.settings.only {
		return m, tea.Quit
	}
	return m, nil
}

func (m model) openPicker() (tea.Model, tea.Cmd) {
	path, err := config.ModelsPath()
	if err != nil {
		m.picker.status = "Cannot resolve the config directory"
	}
	catalog, fromDisk, err := models.Load(path)
	if err != nil {
		m.picker.status = "Catalog unavailable: " + err.Error()
	} else if fromDisk {
		m.picker.status = fmt.Sprintf("%d profiles from %s", len(catalog.Profiles), path)
	} else {
		m.picker.status = fmt.Sprintf("%d built-in profiles", len(catalog.Profiles))
	}

	m.picker.entries = models.Merge(catalog, m.picker.liveIDs())
	m.refreshPicker()
	m.screen = modelSearchScreen
	m.picker.query.Focus()
	return m, tea.Batch(textinput.Blink, m.fetchLiveCmd())
}

// liveIDs recovers the endpoint's model list from entries already loaded, so
// reopening or refreshing the picker does not lose availability.
func (p pickerState) liveIDs() []string {
	if !p.liveLoaded {
		return nil
	}
	ids := make([]string, 0, len(p.entries))
	for _, entry := range p.entries {
		if entry.Available {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}

func (m model) updatePicker(message tea.Msg, key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case liveModelsMsg:
		if msg.err != nil {
			m.picker.status = "Live list unavailable: " + msg.err.Error()
			m.picker.filter.AvailableOnly = false
		} else {
			catalog, _, err := m.loadCatalog()
			if err == nil {
				m.picker.entries = models.Merge(catalog, msg.ids)
			}
			// Narrow to what the endpoint serves, but only the first time so a
			// deliberate toggle is not undone by a later refresh.
			if !m.picker.liveLoaded {
				m.picker.filter.AvailableOnly = true
				m.picker.liveLoaded = true
			}
			m.picker.status = fmt.Sprintf("%d models offered by the endpoint", len(msg.ids))
		}
		m.refreshPicker()
		return m, nil
	case catalogUpdateMsg:
		if msg.err != nil {
			m.picker.status = "Update failed: " + msg.err.Error()
		} else if msg.result.Changed {
			m.picker.status = fmt.Sprintf("Catalog updated: %d profiles saved to %s", msg.result.Count, msg.result.Path)
		} else {
			m.picker.status = "Catalog already up to date"
		}
		return m.reloadPicker()
	}

	if isKey {
		switch key.String() {
		case "esc":
			m.picker.query.Blur()
			m.screen = settingsScreen
			return m, nil
		case "up":
			m.picker.cursor = moveCursor(m.picker.cursor, -1, len(m.picker.view))
			return m, nil
		case "down":
			m.picker.cursor = moveCursor(m.picker.cursor, 1, len(m.picker.view))
			return m, nil
		case "enter":
			if len(m.picker.view) == 0 {
				return m, nil
			}
			m.picker.card = m.picker.view[m.picker.cursor]
			m.picker.query.Blur()
			m.screen = modelCardScreen
			return m, nil
		case "ctrl+o":
			m.picker.order = m.picker.order.Next()
			m.refreshPicker()
			return m, nil
		case "ctrl+g":
			m.picker.filter.RequireImages = !m.picker.filter.RequireImages
			m.refreshPicker()
			return m, nil
		case "ctrl+t":
			m.picker.filter.RequireTools = !m.picker.filter.RequireTools
			m.refreshPicker()
			return m, nil
		case "ctrl+l":
			m.picker.filter.AvailableOnly = !m.picker.filter.AvailableOnly
			m.refreshPicker()
			return m, nil
		case "ctrl+n":
			m.picker.filter.IncludeUnprofiled = !m.picker.filter.IncludeUnprofiled
			m.refreshPicker()
			return m, nil
		case "ctrl+r":
			m.picker.status = "Downloading catalog…"
			return m, m.updateCatalogCmd()
		}
	}

	var cmd tea.Cmd
	m.picker.query, cmd = m.picker.query.Update(message)
	m.picker.filter.Query = m.picker.query.Value()
	m.refreshPicker()
	return m, cmd
}

func (m model) updateModelCard(key tea.KeyMsg, isKey bool) (tea.Model, tea.Cmd) {
	if !isKey {
		return m, nil
	}
	switch key.String() {
	case "enter", "a":
		m.settings.fields[fieldModel].SetValue(m.picker.card.ID)
		m.blurSettings()
		m.settings.cursor = fieldModel
		m.settings.fields[fieldModel].Focus()
		m.settings.status = "Model set to " + m.picker.card.ID + " (Ctrl+S to save)"
		m.screen = settingsScreen
		return m, textinput.Blink
	case "esc", "b":
		m.screen = modelSearchScreen
		m.picker.query.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m *model) refreshPicker() {
	view := m.picker.filter.Apply(m.picker.entries)
	models.Sort(view, m.picker.order)
	m.picker.view = view
	if m.picker.cursor >= len(view) {
		m.picker.cursor = 0
	}
}

func (m model) reloadPicker() (tea.Model, tea.Cmd) {
	catalog, _, err := m.loadCatalog()
	if err != nil {
		return m, nil
	}
	m.picker.entries = models.Merge(catalog, m.picker.liveIDs())
	m.refreshPicker()
	return m, nil
}

func (m model) loadCatalog() (models.Catalog, bool, error) {
	path, err := config.ModelsPath()
	if err != nil {
		catalog, embedErr := models.Embedded()
		return catalog, false, embedErr
	}
	return models.Load(path)
}

// backendCredentials prefers what is currently typed in the dialog so the
// live list reflects unsaved edits.
func (m model) backendCredentials() (baseURL, apiKey string) {
	baseURL = strings.TrimSpace(m.settings.fields[fieldBaseURL].Value())
	if baseURL == "" {
		baseURL = os.Getenv(config.KeyBaseURL)
	}
	if baseURL == "" {
		baseURL = config.DefaultBaseURL
	}
	apiKey = strings.TrimSpace(m.settings.fields[fieldAPIKey].Value())
	if apiKey == "" {
		apiKey = os.Getenv(config.KeyAPIKey)
	}
	return baseURL, apiKey
}

func (m model) fetchLiveCmd() tea.Cmd {
	baseURL, apiKey := m.backendCredentials()
	return func() tea.Msg {
		ids, err := models.FetchAvailable(context.Background(), baseURL, apiKey)
		return liveModelsMsg{ids: ids, err: err}
	}
}

func (m model) updateCatalogCmd() tea.Cmd {
	path, err := config.ModelsPath()
	return func() tea.Msg {
		if err != nil {
			return catalogUpdateMsg{err: err}
		}
		result, updateErr := models.Update(context.Background(), path)
		return catalogUpdateMsg{result: result, err: updateErr}
	}
}

func (m model) settingsView() string {
	var out strings.Builder
	writeLine(&out, titleStyle.Render("Settings"))
	writeLine(&out, mutedStyle.Render(m.settings.path))
	out.WriteByte('\n')

	for index := range m.settings.fields {
		label := settingsLabels[index]
		marker := "  "
		style := mutedStyle
		if m.settings.cursor == index {
			marker = "› "
			style = accentStyle
		}
		writeLine(&out, style.Render(marker+label))
		writeLine(&out, "  "+m.settings.fields[index].View())
	}
	out.WriteByte('\n')
	writeLine(&out, actionRow("Search models…", m.settings.cursor == rowSearch))
	writeLine(&out, actionRow("Save", m.settings.cursor == rowSave))
	if m.settings.status != "" {
		out.WriteByte('\n')
		writeLine(&out, accentStyle.Render(m.settings.status))
	}
	out.WriteByte('\n')
	out.WriteString(mutedStyle.Render("↑/↓ move   Enter select   Ctrl+S save   Esc back"))
	return out.String()
}

func (m model) modelSearchView() string {
	var out strings.Builder
	writeLine(&out, titleStyle.Render("Choose a model"))
	writeLine(&out, m.picker.query.View())
	writeLine(&out, mutedStyle.Render(m.pickerFilterLine()))
	out.WriteByte('\n')

	rows := m.visibleRows() - 4
	if rows < 3 {
		rows = 3
	}
	if len(m.picker.view) == 0 {
		writeLine(&out, mutedStyle.Render("No models match the current filters"))
	}
	start := visibleStart(len(m.picker.view), m.picker.cursor, rows)
	end := start + rows
	if end > len(m.picker.view) {
		end = len(m.picker.view)
	}
	for index := start; index < end; index++ {
		entry := m.picker.view[index]
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == m.picker.cursor {
			prefix = "› "
			style = selectedStyle
		}
		writeLine(&out, style.Render(prefix+m.pickerRow(entry)))
	}
	out.WriteByte('\n')
	if m.picker.status != "" {
		writeLine(&out, mutedStyle.Render(truncate(m.picker.status, m.contentWidth()-2)))
	}
	out.WriteString(mutedStyle.Render("Enter card   Ctrl+O sort   Ctrl+G images   Ctrl+T tools   Ctrl+L live   Ctrl+N unlisted   Ctrl+R update   Esc back"))
	return out.String()
}

func (m model) pickerRow(entry models.Entry) string {
	badges := ""
	if entry.Profile.ImageInputs {
		badges += "img "
	}
	if entry.Profile.ToolCalling {
		badges += "tool "
	}
	if !entry.HasProfile {
		badges += "unlisted "
	}
	if !entry.Available {
		badges += "offline "
	}
	name := truncate(entry.ID, m.contentWidth()-26)
	return fmt.Sprintf("%-*s %8s  %s", m.contentWidth()-26, name, formatTokens(entry.Profile.MaxInputTokens), strings.TrimSpace(badges))
}

func (m model) pickerFilterLine() string {
	parts := []string{"sort: " + m.picker.order.String()}
	if m.picker.filter.AvailableOnly {
		parts = append(parts, "live only")
	}
	if m.picker.filter.IncludeUnprofiled {
		parts = append(parts, "unlisted shown")
	}
	if m.picker.filter.RequireImages {
		parts = append(parts, "images")
	}
	if m.picker.filter.RequireTools {
		parts = append(parts, "tool calling")
	}
	return fmt.Sprintf("%d models   %s", len(m.picker.view), strings.Join(parts, "   "))
}

func (m model) modelCardView() string {
	entry := m.picker.card
	var out strings.Builder
	writeLine(&out, titleStyle.Render(entry.ID))
	if entry.Profile.Name != "" && entry.Profile.Name != entry.ID {
		writeLine(&out, mutedStyle.Render(entry.Profile.Name))
	}
	out.WriteByte('\n')

	if !entry.HasProfile {
		writeLine(&out, mutedStyle.Render("No capability profile for this model."))
		writeLine(&out, mutedStyle.Render("The endpoint lists it, but unlisted models are often not"))
		writeLine(&out, mutedStyle.Render("callable and may fail with a 404."))
	} else {
		writeLine(&out, cardRow("Context in", formatTokens(entry.Profile.MaxInputTokens)))
		writeLine(&out, cardRow("Max output", formatTokens(entry.Profile.MaxOutputTokens)))
		writeLine(&out, cardRow("Released", orDash(entry.Profile.ReleaseDate)))
		writeLine(&out, cardRow("Updated", orDash(entry.Profile.LastUpdated)))
		writeLine(&out, cardRow("Open weights", yesNo(entry.Profile.OpenWeights)))
		out.WriteByte('\n')
		writeLine(&out, cardRow("Image input", yesNo(entry.Profile.ImageInputs)))
		writeLine(&out, cardRow("Audio input", yesNo(entry.Profile.AudioInputs)))
		writeLine(&out, cardRow("Video input", yesNo(entry.Profile.VideoInputs)))
		writeLine(&out, cardRow("Tool calling", yesNo(entry.Profile.ToolCalling)))
		writeLine(&out, cardRow("Reasoning", yesNo(entry.Profile.ReasoningOutput)))
		writeLine(&out, cardRow("Structured out", yesNo(entry.Profile.StructuredOut)))
		writeLine(&out, cardRow("Temperature", yesNo(entry.Profile.Temperature)))
	}
	out.WriteByte('\n')
	writeLine(&out, cardRow("On endpoint", yesNo(entry.Available)))
	out.WriteByte('\n')
	out.WriteString(mutedStyle.Render("Enter accept   Esc back to list"))
	return out.String()
}

func actionRow(label string, selected bool) string {
	if selected {
		return selectedStyle.Render("› " + label)
	}
	return "  " + label
}

func cardRow(label, value string) string {
	return mutedStyle.Render(fmt.Sprintf("%-15s", label)) + value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func orDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// formatTokens abbreviates a context window for the list and card views.
func formatTokens(tokens int) string {
	switch {
	case tokens <= 0:
		return "—"
	case tokens >= 1_000_000:
		return strconv.FormatFloat(float64(tokens)/1_000_000, 'f', 1, 64) + "M"
	case tokens >= 1000:
		return strconv.Itoa(tokens/1000) + "k"
	default:
		return strconv.Itoa(tokens)
	}
}
