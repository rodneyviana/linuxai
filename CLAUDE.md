# linuxai

A terminal-first CLI assistant for Linux and Linux-programming questions.
Runs locally and over SSH. You trigger it with a hotkey, type a prompt, and it
streams an answer. It can optionally take a manually attached image and
optionally let the model search through self-hosted SearXNG and read guarded
public web pages.

Full rationale, model options, and message shapes: see @docs/DESIGN.md

## Stack (do not change without discussion)

- Language: **Go**. Third-party modules are allowed only when they can be
  compiled into the shipped static binary and introduce no runtime dependency.
  - HTTP: `net/http`. JSON: `encoding/json`. Images: `encoding/base64`.
- Ship as a single static binary. Always build with `CGO_ENABLED=0`.
- The whole point is "scp one file to any box and run", so nothing may
  introduce a runtime dependency (no cgo, no interpreter, no shared libs).

## Build & run

```bash
# Local dev / workstation + P40 server (x86-64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o linuxai-amd64 ./cmd/linuxai
# Jetson Orin (arm64)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o linuxai-arm64 ./cmd/linuxai

go test ./...
gofmt -l .   # must be clean

# Regenerate the embedded model capability catalog (internal/models/profiles.json).
# Downloads the upstream profiles and prunes them to chat-capable models.
go run ./internal/models/gen
```

## Backends (both OpenAI-compatible, streaming via SSE)

- Primary: NVIDIA hosted free endpoints.
  - Base URL: `https://integrate.api.nvidia.com/v1`
  - Auth: `Authorization: Bearer $NVIDIA_API_KEY`
- Secondary: local Ollama.
  - Base URL: `http://localhost:11434/v1`, dummy key.
- One client, selected by config/flag. Never hardcode a base URL or key.

## Config and secrets

- All config comes from environment variables (`NVIDIA_API_KEY`, base URL,
  default model, SearXNG host, etc.).
- Support loading a `.env` file, but implement it with a small stdlib parser.
  **Do not add a runtime `.env` dependency.** See DESIGN.md for the loader spec.
- Load order: real process env wins; `.env` only fills values not already set.
  Look for `./.env` first, then `~/.config/linuxai/.env`.
- `.env` holds the API key, so it must be gitignored. Commit `.env.example`
  with placeholders instead.

## Default models (NIM model strings)

- Default: `openai/gpt-oss-20b`
- Fast / screenshot-OCR tier: `nvidia/nemotron-nano-12b-v2-vl`
- Quality escalation: `moonshotai/kimi-k2.6`
- Light alternative: `mistralai/ministral-14b-instruct-2512`

## Feature specs (see DESIGN.md for detail)

- Hotkey trigger: **never bind Ctrl+I** (it is the Tab byte in terminals).
  Primary is a tmux popup; fallbacks are a readline chord and a desktop key.
- Manual image attach only (no auto screen capture). Read the clipboard when
  local, accept `--image PATH` when remote. Downscale before sending.
- Optional `--web` exposes native `web_search` and `web_read` tools to the
  model. Search uses a self-hosted SearXNG JSON endpoint. Page reads are
  text-only, size-bounded, SSRF-resistant, and require user authorization for
  origins outside the reviewed documentation whitelist.
- History: append-only JSONL per session under
  `~/.local/share/linuxai/chats/`, with a `current` pointer for the active
  thread. Commands: bare (continue), `--new`, `--list`, `--resume <id>`,
  `--search <term>`.

## Hard constraints / gotchas

- `-v` is `--version`. Verbose is `--verbose` / `-V`; never reassign `-v`.
- Model capabilities are not available from any NVIDIA API. They come from the
  embedded catalog; `/v1/models` only supplies which IDs the account may call,
  and it over-reports (unmatched IDs often 404).

- Over plain SSH there is no local clipboard and no graphical screen. Do not
  assume either exists; gate clipboard/screenshot code behind a local check.
- NVIDIA's hosted endpoint caps inline base64 images (~180 KB). Downscale to
  about 1024 px wide, or use the asset-upload path for larger images.
- SearXNG ships with JSON output disabled. It must be enabled in `settings.yml`
  under `search.formats` before the JSON endpoint works.
- If running under WSL, work in WSL-native paths (`~/...`), not `/mnt/c/...`.

## Conventions

- Small functions, standard `gofmt`, and no dependency that prevents a static
  `CGO_ENABLED=0` build or requires files/libraries beside the binary.
- Any generated prose (commit messages, summaries) should read naturally and
  avoid em-dashes.
