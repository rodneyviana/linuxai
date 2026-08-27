package models

import (
	"testing"
)

const fixtureSource = `"""Auto-generated model profiles."""

from typing import Any

_PROFILES: dict[str, dict[str, Any]] = {
    # A chat model with every capability we care about.
    "vendor/chat-model": {
        "name": "Chat Model",
        "release_date": "2025-01-01",
        "last_updated": "2025-06-01",
        "open_weights": True,
        "max_input_tokens": 128_000,
        "max_output_tokens": 4096,
        "text_inputs": True,
        "image_inputs": True,
        "audio_inputs": False,
        "video_inputs": False,
        "text_outputs": True,
        "tool_calling": True,
        "structured_output": True,
        "temperature": True,
    },
    'vendor/embed-model': {
        "name": "Embedder",
        "max_input_tokens": 8192,
        "max_output_tokens": 0,
        "text_inputs": True,
        "text_outputs": True,
    },
    "vendor/old-model": {
        "name": "Old Model",
        "status": "deprecated",
        "max_input_tokens": 4096,
        "max_output_tokens": 4096,
        "text_inputs": True,
        "text_outputs": True,
        "tool_calling": False,
        "temperature": None,
    },
    "vendor/image-gen": {
        "name": "Image Gen",
        "max_input_tokens": 512,
        "max_output_tokens": 0,
        "text_inputs": True,
        "text_outputs": False,
        "image_outputs": True,
        "tags": ["one", "two"],
    },
}
`

func TestParseProfiles(t *testing.T) {
	profiles, err := ParseProfiles([]byte(fixtureSource))
	if err != nil {
		t.Fatalf("ParseProfiles: %v", err)
	}
	if len(profiles) != 4 {
		t.Fatalf("parsed %d profiles, want 4", len(profiles))
	}

	chat := profiles["vendor/chat-model"]
	if chat.Name != "Chat Model" {
		t.Errorf("Name = %q, want %q", chat.Name, "Chat Model")
	}
	if chat.MaxInputTokens != 128000 {
		t.Errorf("MaxInputTokens = %d, want 128000", chat.MaxInputTokens)
	}
	if !chat.ImageInputs || !chat.ToolCalling || !chat.StructuredOut {
		t.Errorf("capability flags not parsed: %+v", chat)
	}
	if !chat.Chat() {
		t.Error("chat model should be usable for chat")
	}

	if profiles["vendor/embed-model"].Chat() {
		t.Error("a model with no output tokens is not usable for chat")
	}
	if !profiles["vendor/old-model"].Deprecated() {
		t.Error("status deprecated not detected")
	}
	if profiles["vendor/old-model"].Chat() {
		t.Error("deprecated models must be excluded")
	}
	if profiles["vendor/image-gen"].Chat() {
		t.Error("a model with no text output is not usable for chat")
	}
}

func TestParseProfilesRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"no assignment": "PROFILES = {}\n",
		"not a literal": "_PROFILES = {\"a\": compute()}\n",
		"unterminated":  "_PROFILES = {\"a\": {\"name\": \"x\"\n",
		"empty":         "_PROFILES = {}\n",
	}
	for name, source := range cases {
		if _, err := ParseProfiles([]byte(source)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
