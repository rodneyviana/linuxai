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

## Versioning

`linuxai --version` prints the build's version, derived from
`git describe --tags --always --dirty` at build time (e.g. `v0.4.0`, or
`v0.4.0-3-gabc1234` for commits since the last tag, or a bare commit hash
like `07fe69e` before any tag exists). A plain `go build` with no
`-ldflags` reports `dev`.

To cut a release, tag it and rebuild/repackage:

```bash
git tag v0.4.0
git push origin v0.4.0
./scripts/package.sh
```

## Install (self-extracting installer)

If [`makeself`](https://makeself.io/) is installed, `scripts/package.sh`
computes the version, cross-compiles both architectures with it baked in
via `-ldflags`, and bundles everything into a single self-extracting
installer named after that version:

```bash
./scripts/package.sh                # writes ./linuxai-installer-<version>.run
```

Run the installer on the target box (`scp` it over for remote installs):

```bash
./linuxai-installer-<version>.run
```

It detects the machine's architecture (`uname -m`) and:

- installs the matching binary to `~/.local/bin/linuxai` (no sudo needed;
  warns if `~/.local/bin` isn't on your `PATH`) and prints the version
  it just installed
- creates `~/.config/linuxai/.env` from the template, but never overwrites
  an existing one
- prints a reminder to fill in `NVIDIA_API_KEY` before first use

`scripts/install.sh` is the script makeself runs after extraction, in case
you want to read or adapt it directly instead of going through `.run`.

## Tests

Every package has real unit tests (`go test ./...`), no network or live API
key required:

| Package | Covers |
|---|---|
| `internal/config` | `.env` parsing (quotes, comments, `export`), `./.env` vs `~/.config/linuxai/.env` precedence, process env always winning, `Load()` defaults and the missing-key error. |
| `internal/history` | Thread create/append/load, the `current` pointer, `--list` ordering and titles, `--search`, and the replay token budget. Runs against a temp `$HOME`, never the real `~/.local/share/linuxai`. |
| `internal/imageutil` | Downscaling math and that `ToDataURL` always produces a valid JPEG data URL under the size cap, even for images already narrower than the target width. |
| `internal/llm` | Text, image, assistant-tool-call, and tool-result message shapes; SSE text delivery; fragmented native tool-call assembly; authentication and error handling. |
| `internal/searxng` | Search-tool queries against an `httptest` server, including result caps and non-200/non-JSON errors. |
| `internal/mdterm` | Streaming Markdown-to-ANSI rendering (text styles, code, headers, bullets, tables, and Unicode LaTeX math), plain-text fallback, `NO_COLOR`, and byte-by-byte vs. whole-string equivalence (guards against chunk-boundary bugs). |
| `internal/tui` | Launcher start screens, prompt submission, new-chat navigation, and conditional web-search toggling. |
| `internal/webagent` | Native tool definitions, bounded search/read loops, structured tool results, activity output, and once/session/deny consent behavior. |
| `internal/webread` | Origin trust rules, consent-before-fetch, redirect reauthorization, private-address blocking, MIME/size limits, and readable HTML extraction. |
| `cmd/linuxai` | Long and short flag parsing, help output, system-message construction, prompt/image handling, and thread resolution for explicit, active, empty, idle, missing, and resumed threads. |

Run just the tests with `go test ./...`, or `go test ./... -v` for
per-test output.

## Configure

Copy `.env.example` to `.env` and fill in your key:

```bash
cp .env.example .env
```

`.env` is looked up first at `./.env`, then at `~/.config/linuxai/.env`.
Values already present in the process environment always win.

The user config directory is `${XDG_CONFIG_HOME:-~/.config}/linuxai`. Put
custom assistant instructions in `instructions.txt` there, next to the user
`.env`:

```text
~/.config/linuxai/instructions.txt
```

If that file is missing or blank, linuxai uses this built-in instruction:

```text
Only answer questions about operating systems, especially Linux if no OS is specified, and programming. Do not be verbose unless required. If a question is outside this scope, do not apologize or give only a generic refusal. Briefly explain that you can help with operating systems, Linux, command-line tools, system administration, software development, debugging, and programming, give one or two relevant examples, and suggest a computing-related way to reframe the question when natural.
```

| Variable | Purpose |
|---|---|
| `NVIDIA_API_KEY` | API key for the NVIDIA hosted free endpoint (required unless pointing at Ollama). |
| `LINUXAI_BASE_URL` | Backend base URL. Defaults to the NVIDIA endpoint; set to `http://localhost:11434/v1` to use local Ollama instead. |
| `LINUXAI_MODEL` | Model string to use. Defaults to `openai/gpt-oss-20b`. |
| `LINUXAI_SEARXNG_URL` | SearXNG host used by the `web_search` tool. Required to enable `-w`/`--web`; the TUI disables its web toggle when absent. Must point at the published port, and the instance needs `json` under `search.formats` in `settings.yml`. |

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

Run `linuxai` with no prompt in a terminal to open the interactive launcher.
An active thread opens a menu with Continue selected; a missing, empty, or
five-minute-idle thread opens the prompt directly. The launcher can start a
new chat, resume or search history, and toggle web search. It closes before the
answer streams, so the response remains normal selectable terminal output.

<p align="center">
  <img src="linuxai-dialog.jpg" alt="linuxai new-thread prompt with web search toggle" width="760">
</p>
<p align="center"><em>New-thread prompt with the optional web-search toggle.</em></p>

<p align="center">
  <img src="linux-ai-dialog-2.jpg" alt="linuxai launcher menu showing the active thread and navigation choices" width="760">
</p>
<p align="center"><em>Active-thread menu with continue, new chat, resume, and history search actions.</em></p>

Prompt keys are `Ctrl+S` to send, `Ctrl+N` for a new chat, `Ctrl+W` to toggle
web search, and `Esc` to return to the menu. Argument prompts and piped input
never open the launcher.

### Web tools

`-w`/`--web` exposes two native tools to the model: `web_search` queries the
configured SearXNG instance, and `web_read` extracts readable text from a
selected source. The model can refine searches, inspect more than one source,
and stop when it has enough evidence. Search activity is printed to stderr;
normal answer output remains clean when redirected. Without `--web`, neither
tool is sent to the backend.

Search is discovery only. Requests to read or summarize an article must use
`web_read`; search snippets are never treated as the article. Search and read
budgets are independent, so exhausting searches removes `web_search` but keeps
`web_read` available for the selected results. RSS and Atom feeds are extracted
as entry lists, and HTML pages include a bounded set of resolved links. For an
article summary, linuxai keeps requiring `web_read` through feed/index pages
until the chosen full article is extracted or access fails.

Assistant text produced alongside tool calls is treated as internal planning
and is not printed. Duplicate page URLs are not fetched twice, and only the
accepted final answer is sent to the Markdown renderer or saved in history.

Reading reviewed official documentation hosts (including kernel.org, major
Linux distributions, GNU, man7.org, Go, Python, Rust, MDN, and Wikipedia) is
automatic. Other origins prompt on `/dev/tty` with three choices: allow this
URL once, allow the origin for this invocation, or deny. Piped prompts still
use the controlling terminal for approval. If no interactive terminal exists,
an unknown origin is denied instead of blocking. Redirects to a different
origin are authorized separately.

The reader accepts only public HTTP(S) text content. It blocks loopback,
private, link-local, and metadata-service addresses even after approval; sends
no cookies or credentials; executes no JavaScript; and limits redirects,
response size, extracted text, searches, page reads, tool rounds, and total web
context. When a tool budget is exhausted, linuxai disables the tools for one
final synthesis turn instead of failing the request. Search snippets and
fetched pages are marked as untrusted data, and the model is instructed to read
important sources and cite their URLs.

Native tool calling must be supported by the selected OpenAI-compatible model
and endpoint. A backend that rejects `tools` returns its capability error
instead of silently falling back to an unreliable text protocol.

NVIDIA's free-tier endpoint occasionally stalls mid-stream (emits a token,
then goes silent with no `[DONE]` and no close). If no new data arrives
for 45 seconds, linuxai aborts with `stream stalled: no data received for
45s` instead of hanging forever.

### Terminal formatting

When stdout is a real terminal, the model's Markdown is rendered live as
ANSI (bold, italic, inline `code`, fenced code blocks, headers, bullets, and
aligned pipe tables) as tokens stream in. Inline `$...$` or `\(...\)` and
display `$$...$$` or `\[...\]` math are converted to readable Unicode,
including common Greek letters, operators, relations, arrows, sets, fractions,
square roots, and simple
superscripts/subscripts. Unsupported commands remain visible, and scripts
without a complete Unicode mapping fall back to `^(...)` or `_(...)`; this is
terminal formatting, not full TeX typesetting. Math inside code spans or fenced
blocks stays literal. Piping to a file or another command (`linuxai ... | less`)
automatically falls back to raw Markdown, and setting `NO_COLOR` (any value)
disables it explicitly.

### Flags

Flags can appear anywhere in the command; every other token is joined back
together, in order, as the prompt.

| Flags | Effect |
|---|---|
| `-n`, `--new`, `--new-thread` | Start a fresh thread instead of applying the idle-time rule. |
| `-r`, `--resume <id>` | Switch the active thread to `<id>` (see `--list`), then continue it with the given prompt. |
| `-l`, `--list` | Print saved threads (id, last-modified time, title = first message). No LLM call. |
| `-s`, `--search <term>` | Grep every saved thread for `<term>` and print matches. No LLM call. |
| `-w`, `--web` | Enable model-driven `web_search` and guarded `web_read`. Equivalent to starting the prompt with `/web `. |
| `-i`, `--image <path>` | Attach an image file (downscaled, sent as `image_url` content). |
| `-c`, `--clipboard` | Attach whatever image is on the local clipboard (`xclip`/`wl-paste`; requires `$DISPLAY` or `$WAYLAND_DISPLAY`, so it only works locally, not over plain SSH). |
| `-v`, `--version` | Print the build's version and exit. No LLM call. |
| `-h`, `--help` | Print usage, options, and examples. No configuration or LLM call. |

Examples:

```bash
linuxai -n how do I check disk usage
linuxai what about inodes specifically       # continues the same thread
linuxai -l
linuxai -r 20260704-143347-b9c9bd one more thing about that
linuxai -w what is the latest stable kernel version
linuxai -i ~/Pictures/error.png what does this error mean
```

### History

Threads are stored as append-only JSONL under
`~/.local/share/linuxai/chats/<id>.jsonl`, one message per line. A
`~/.local/share/linuxai/current` pointer file names the active thread. Implicit
invocations continue it when its last activity was within five minutes and
start a new thread otherwise. Explicit `--new` and `--resume` always win. On
each turn, prior messages are replayed into the request under a rough token
budget, dropping the oldest ones once a thread gets long.

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
rendering with Unicode LaTeX math, configurable system instructions, an
interactive bare-command launcher, JSONL history with
`--new`/`--list`/`--resume`/`--search`,
manual image attach (`--image`/`--clipboard`) with stdlib-only
downscaling, and bounded `--web` search/read tools with per-origin consent.

Not automated: the hotkey trigger (tmux/readline/desktop bindings above are
provided as copy-paste snippets, not applied automatically).
