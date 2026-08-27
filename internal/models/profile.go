// Package models provides the model catalog used by the settings dialog:
// capability profiles derived from the langchain-nvidia profile data, plus
// live availability from the backend's OpenAI-compatible /models endpoint.
package models

import "strings"

// Profile describes one model's capabilities. Field names mirror the keys in
// the upstream _profiles.py so the parsed data maps straight onto it.
type Profile struct {
	Name            string `json:"name"`
	Status          string `json:"status,omitempty"`
	ReleaseDate     string `json:"release_date,omitempty"`
	LastUpdated     string `json:"last_updated,omitempty"`
	OpenWeights     bool   `json:"open_weights"`
	MaxInputTokens  int    `json:"max_input_tokens"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	TextInputs      bool   `json:"text_inputs"`
	ImageInputs     bool   `json:"image_inputs"`
	AudioInputs     bool   `json:"audio_inputs"`
	VideoInputs     bool   `json:"video_inputs"`
	TextOutputs     bool   `json:"text_outputs"`
	ImageOutputs    bool   `json:"image_outputs"`
	AudioOutputs    bool   `json:"audio_outputs"`
	VideoOutputs    bool   `json:"video_outputs"`
	ReasoningOutput bool   `json:"reasoning_output"`
	ToolCalling     bool   `json:"tool_calling"`
	StructuredOut   bool   `json:"structured_output"`
	Attachment      bool   `json:"attachment"`
	Temperature     bool   `json:"temperature"`
}

// Deprecated reports whether upstream has marked the model as retired.
func (p Profile) Deprecated() bool {
	return strings.EqualFold(p.Status, "deprecated")
}

// Chat reports whether the model takes text in and produces text out, which
// is the only shape linuxai can actually drive.
func (p Profile) Chat() bool {
	return p.TextInputs && p.TextOutputs && p.MaxOutputTokens > 0 && !p.Deprecated()
}

// Entry is one row in the picker. Profile data and live availability come
// from independent sources, so either may be missing.
type Entry struct {
	ID         string
	Profile    Profile
	HasProfile bool
	Available  bool
}
