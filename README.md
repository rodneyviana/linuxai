# linuxai

A terminal-first CLI assistant for Linux and Linux-programming questions.
Runs locally and over SSH, streams answers as they arrive, and ships as a
single static Go binary with no runtime dependencies.

See [`docs/DESIGN.md`](docs/DESIGN.md) for the full design rationale and
[`CLAUDE.md`](CLAUDE.md) for project conventions.

## Build

```bash
# Local dev / workstation (x86-64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o linuxai-amd64 ./cmd/linuxai

# Jetson Orin (arm64)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o linuxai-arm64 ./cmd/linuxai
```

Run `./test-all.sh` before committing. It runs `gofmt -l .`, `go vet ./...`,
`go test ./...`, and a static cross-compile for both amd64 and arm64,
failing on the first problem it finds.

## Tests

Every package has real unit tests (`go test ./...`), no network or live API
key required:

| Package | Covers |
|---|---|
| `internal/config` | `.env` parsing (quotes, comments, `export`), `./.env` vs `~/.config/linuxai/.env` precedence, process env always winning, `Load()` defaults and the missing-key error. |
| `internal/history` | Thread create/append/load, the `current` pointer, `--list` ordering and titles, `--search`, and the replay token budget. Runs against a temp `$HOME`, never the real `~/.local/share/linuxai`. |
| `internal/imageutil` | Downscaling math and that `ToDataURL` always produces a valid JPEG data URL under the size cap, even for images already narrower than the target width. |
| `internal/llm` | The text-only vs. multimodal `Message` JSON shapes, and `StreamChat` against an `httptest` SSE server (token delivery, auth header, non-200 errors, malformed chunks). |
| `internal/searxng` | `Search` against an `httptest` server (result capping, non-200, non-JSON content type) and `GroundingBlock` formatting. |
| `internal/mdterm` | Streaming Markdown-to-ANSI rendering (bold, inline code, fenced blocks incl. indented ones, headers, bullets), plain-text fallback, `NO_COLOR`, and byte-by-byte vs. whole-string equivalence (guards against chunk-boundary bugs). |
| `cmd/linuxai` | `parseArgs` (flags anywhere in argv, `/web ` prefix, missing-value errors) and `resolveThread` (`--new`/`--resume`/bare-continue/resume-of-a-missing-id). |

Run just the tests with `go test ./...`, or `go test ./... -v` for
per-test output.

## Configure

Copy `.env.example` to `.env` and fill in your key:

```bash
cp .env.example .env
```

`.env` is looked up first at `./.env`, then at `~/.config/linuxai/.env`.
Values already present in the process environment always win.

| Variable | Purpose |
|---|---|
| `NVIDIA_API_KEY` | API key for the NVIDIA hosted free endpoint (required unless pointing at Ollama). |
| `LINUXAI_BASE_URL` | Backend base URL. Defaults to the NVIDIA endpoint; set to `http://localhost:11434/v1` to use local Ollama instead. |
| `LINUXAI_MODEL` | Model string to use. Defaults to `qwen/qwen3.5-122b-a10b`. |
| `LINUXAI_SEARXNG_URL` | SearXNG host for the `--web` grounding tier. Must point at the port your SearXNG container actually publishes, and that instance needs `json` under `search.formats` in its `settings.yml`. |

## Use

Pass the question as plain trailing arguments, no quoting needed:

```bash
linuxai how do I list hidden files in bash
```

or pipe it on stdin:

```bash
echo "what does chmod 755 mean?" | linuxai
```

The answer streams to stdout token by token as it's generated.

NVIDIA's free-tier endpoint occasionally stalls mid-stream (emits a token,
then goes silent with no `[DONE]` and no close). If no new data arrives
for 45 seconds, linuxai aborts with `stream stalled: no data received for
45s` instead of hanging forever.

### Terminal formatting

When stdout is a real terminal, the model's Markdown is rendered live as
ANSI (bold, inline `code`, fenced code blocks, `#`/`##` headers, `-`/`*`
bullets) as tokens stream in — no waiting for the full response. Piping to
a file or another command (`linuxai ... | less`) automatically falls back
to raw Markdown, and setting `NO_COLOR` (any value) disables it explicitly.

### Flags

Flags can appear anywhere in the command; every other token is joined back
together, in order, as the prompt.

| Flag | Effect |
|---|---|
| `--new` | Start a fresh thread instead of continuing the current one. |
| `--resume <id>` | Switch the active thread to `<id>` (see `--list`), then continue it with the given prompt. |
| `--list` | Print saved threads (id, last-modified time, title = first message). No LLM call. |
| `--search <term>` | Grep every saved thread for `<term>` and print matches. No LLM call. |
| `--web` | Ground the answer with top results from your SearXNG instance. Equivalent to starting the prompt with `/web `. |
| `--image <path>` | Attach an image file (downscaled, sent as `image_url` content). |
| `--clipboard` | Attach whatever image is on the local clipboard (`xclip`/`wl-paste`; requires `$DISPLAY` or `$WAYLAND_DISPLAY`, so it only works locally, not over plain SSH). |

Examples:

```bash
linuxai --new how do I check disk usage
linuxai what about inodes specifically       # continues the same thread
linuxai --list
linuxai --resume 20260704-143347-b9c9bd one more thing about that
linuxai --web what is the latest stable kernel version
linuxai --image ~/Pictures/error.png what does this error mean
```

### History

Threads are stored as append-only JSONL under
`~/.local/share/linuxai/chats/<id>.jsonl`, one message per line. A
`~/.local/share/linuxai/current` pointer file names the active thread; a
bare invocation continues it. On each turn, prior messages are replayed into
the request under a rough token budget, dropping the oldest ones once a
thread gets long.

## Hotkey trigger

Not automated by this repo (no dotfiles are edited for you) but designed
for one-line integration:

**tmux popup (works over SSH too)** - add to `~/.tmux.conf`:

```tmux
bind-key g display-popup -E "linuxai"
```

Then `prefix + g` opens an overlay to type your question in.

**No-tmux fallback: readline chord** - add to `~/.bashrc` / `~/.zshrc`:

```bash
bind -x '"\C-g": linuxai'
```

`Ctrl+G` is readline's `abort`, which is safe to repurpose. (Never bind
`Ctrl+I`: it's the Tab byte and can't be told apart from Tab in most
terminals.)

**Local desktop:** bind a global hotkey (e.g. `Super+A`) to `linuxai` via
your compositor's shortcut settings or `sxhkd`.

**Scripting:** plain `linuxai "question"` always works, no popup needed.

## Status

Implemented: `.env` loading, config from environment, streaming chat
against the NVIDIA/Ollama backend, live Markdown-to-ANSI terminal
rendering, JSONL history with `--new`/`--list`/`--resume`/`--search`,
manual image attach (`--image`/`--clipboard`) with stdlib-only
downscaling, and `--web` SearXNG grounding.

Not automated: the hotkey trigger (tmux/readline/desktop bindings above are
provided as copy-paste snippets, not applied automatically).
