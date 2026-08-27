# linuxai — design notes

Companion to `CLAUDE.md`. This holds the fuller rationale, the model options,
the exact request shapes, and the build roadmap. Claude Code reads this on
demand; it is referenced from `CLAUDE.md` via `@docs/DESIGN.md`.

## Goal

A fast, terminal-first assistant for Linux and Linux-programming questions,
usable both locally and over SSH. Speed and efficiency matter more than raw
frontier quality, because most questions are quick. Images are occasional and
attached manually, not auto-captured.

## Why Go

- Compiles to a single statically linked binary with zero runtime
  dependencies. `scp` it to any box (workstation, 8xP40 server, Jetson) and run.
- Core protocol and data handling use the standard library. Pure-Go UI
  packages are allowed when they preserve static single-binary delivery and
  introduce no runtime dependency.
- Cross-compiles trivially to amd64 and arm64 from one machine.
- Fast startup, which matters for a hotkey popup.

Rust also produces clean static binaries but needs build-time crates for HTTPS.
Python can be stdlib-only but depends on a present `python3` and is slower to
start. C/C++ would require linking libcurl/OpenSSL/JSON, too much plumbing.

## Backends

Both speak the OpenAI-compatible chat completions API, so one client serves
both by swapping base URL and key.

- NVIDIA hosted free endpoints (primary):
  `https://integrate.api.nvidia.com/v1`, bearer `NVIDIA_API_KEY`.
- Local Ollama (secondary): `http://localhost:11434/v1`, dummy key.

Stream responses (SSE) so tokens print as they arrive. That is most of the
perceived speed.

## Model options (all on build.nvidia.com, free-endpoint tier)

Confirm the Free Endpoint badge per model when configuring; the free tier runs
on shared trial credits.

| Model string | Active / total | Inputs | Context | Role |
|---|---|---|---|---|
| `openai/gpt-oss-20b` | 3.6B / 21B MoE | text | 131K | Default. Current general-purpose model with native tool calling. |
| `nvidia/nemotron-nano-12b-v2-vl` | 12B | text, multi-image | 128K | Fast tier. Best OCR / document intelligence for reading pasted terminal screenshots. |
| `nvidia/nemotron-3-nano-omni` (30B-A3B) | ~3B / 30B MoE | text, image, video, audio | 256K | Efficiency champion; use if you want maximum tokens/sec. |
| `mistralai/ministral-14b-instruct-2512` | 14B | text, image (up to 10) | 262K | Light, snappy alternative. |
| `moonshotai/kimi-k2.6` | 32B / 1T MoE | text, image, video | 262K | Quality escalation for hard questions; heavier and slower. |

Recommended default wiring: `openai/gpt-oss-20b` as the daily driver,
`nemotron-nano-12b-v2-vl` as the fast toggle, `kimi-k2.6` as the escalation.

## Request shapes

Text only:

```json
{ "model": "<model>", "stream": true,
  "messages": [ { "role": "user", "content": "how do I ..." } ] }
```

Text plus image (manual attach). Downscale to ~1024 px and keep the base64
under ~180 KB for the hosted endpoint:

```json
{ "model": "<model>", "stream": true,
  "messages": [ { "role": "user", "content": [
    { "type": "text", "text": "what is wrong here?" },
    { "type": "image_url",
      "image_url": { "url": "data:image/png;base64,<...>" } }
  ] } ] }
```

Web-augmented (`--web`): advertise native `web_search` and `web_read` function
tools to the model. Continue the chat-completions loop with structured
assistant tool calls and matching tool-result messages until the model returns
a final answer or reaches a local safety limit. Without `--web`, send no tools.

## Manual image attach

- Local X11: `xclip -selection clipboard -t image/png -o`
- Local Wayland: `wl-paste --type image/png`
- Remote / no display: `--image PATH`, or watch an inbox dir like `~/.ai-inbox/`.
- These are shell-outs to system tools, not Go dependencies. Gate the clipboard
  path behind a `$DISPLAY` / `$WAYLAND_DISPLAY` check; over SSH there is no
  local clipboard.

## SearXNG integration

- Endpoint: `GET http://<searxng-host>/search?q=<query>&format=json`
- Requires `json` under `search.formats` in SearXNG `settings.yml`.
- `web_search` returns the top five titles, URLs, and snippets as untrusted
  discovery material. The model chooses follow-up queries and source reads.
- `web_read` accepts only HTTP(S) text pages, extracts readable text without
  JavaScript, and rechecks authorization after cross-origin redirects.
- Search and read have independent budgets. Exhausting search removes that tool
  while preserving `web_read`; article summaries require a page read rather
  than relying on snippets. RSS/Atom reads expose entry URLs and HTML reads
  preserve bounded resolved links for subsequent full article reads. Continue
  requiring `web_read` through feed/index pages until an article is extracted.
- Buffer assistant content during tool-call turns and discard it as planning;
  emit only the accepted final answer. Deduplicate page URLs within one turn.
- Reviewed official documentation hosts and Wikipedia are read automatically.
  Other origins require once/session/deny approval from `/dev/tty`; without a
  controlling terminal they are denied.
- DNS results are dialed by validated public IP. Loopback, private, link-local,
  multicast, and metadata-service targets remain blocked after authorization.
- Bound tool rounds, search/read counts, response bytes, extracted text, and
  total tool context. On exhaustion, make one final request without tools so
  the model synthesizes from collected evidence. Require citations for claims
  based on web sources.
- Toggle via `--web` flag or a `/web ` prefix in the prompt.

## History and sessions

- One JSONL file per conversation:
  `~/.local/share/linuxai/chats/<id>.jsonl`, one message per line:
  `{ "role": ..., "content": ..., "image": ..., "ts": ... }`.
- A `current` pointer file (`~/.local/share/linuxai/current`) names the active
  thread. Implicit invocations continue it for five minutes after its latest
  activity, then create a fresh thread. `--new` always starts a fresh one and
  `--resume` always selects the requested thread.
- Commands: `--list` (title = first user message), `--resume <id>`,
  `--search <term>` (grep across files).
- On resume, replay prior turns into the messages array under a token budget;
  drop or summarize the oldest turns when a thread gets long.
- JSONL + grep is enough at personal scale. If full-text search is wanted later,
  `modernc.org/sqlite` is pure-Go and keeps the single-binary property.

## Hotkey trigger

Ctrl+I is the Tab byte (0x09) in terminals, so it cannot be distinguished from
Tab without the kitty keyboard protocol. Avoid it.

- Primary (remote-friendly): tmux popup.
  `bind-key g display-popup -E "linuxai"` gives an overlay input on prefix+g.
- No-tmux fallback: readline chord. `bind -x '"\C-g": linuxai'` (Ctrl+G is
  readline `abort`, safe to repurpose).
- Local desktop: a global `Super+A` via the compositor or sxhkd.

Also provide a plain `linuxai "question"` form for scripting.

Bare invocation on a TTY opens a Bubble Tea launcher. An active thread opens
the menu with Continue selected; a missing, empty, or idle thread opens the
prompt directly. The TUI exits before response streaming so output remains in
the normal terminal scrollback. Piped input and argument prompts bypass it.

## Config and .env loading

All configuration is read from environment variables:

- `NVIDIA_API_KEY` — API key for the hosted endpoint.
- `LINUXAI_BASE_URL` — override the backend base URL (default the NVIDIA one;
  set to `http://localhost:11434/v1` for Ollama).
- `LINUXAI_MODEL` — default model string.
- `LINUXAI_SEARXNG_URL` — SearXNG host for the `--web` tier.

These four are what the settings dialog edits. An optional
`~/.config/linuxai/models.json` holds an updated model catalog, and
`~/.config/linuxai/instructions.txt` overrides the built-in system prompt.

Support a `.env` file for these, but write the loader yourself in stdlib. A
runtime `.env` dependency is unnecessary; the parser is small and the shipped
binary must remain self-contained.

Loader spec:

- Search `./.env`, then `~/.config/linuxai/.env`. Load the first that exists
  (or merge both, with `./.env` winning). Missing files are not an error.
- Parse line by line: skip blank lines and lines starting with `#`. Split on the
  first `=`. Trim surrounding whitespace on key and value. Strip one layer of
  matching single or double quotes around the value. Ignore a leading `export `.
- Real process environment takes precedence: only call `os.Setenv` for a key
  that is not already set. This keeps SSH-exported vars and systemd env authoritative.
- Never log values. Redact the key if you ever print config for debugging.

Ship a committed `.env.example` with placeholder values and add `.env` to
`.gitignore`.

Writing the file back is supported too, for the settings dialog. The writer
rewrites `~/.config/linuxai/.env` in place: existing comments, ordering, and
keys it does not manage are preserved, changed keys are edited where they sit
(keeping any `export ` prefix), and new keys are appended. The file is created
`0600` inside a `0700` directory because it holds the API key. Values are
quoted only when they contain whitespace or `#`, matching what the loader can
actually parse back; the loader strips one layer of quotes and does not process
escapes, so the writer must not emit any.

## Settings dialog and model catalog

`linuxai --config`, or Settings in the launcher, opens a dialog over the four
`.env` keys. The API key field is masked while editing. Saving writes the file
and calls `os.Setenv` so the change applies to the running process.

The model field has a browser behind it. Two independent sources feed it:

- **Capabilities** come from a catalog derived from the langchain-nvidia
  `_profiles.py` data (itself generated from models.dev). A pruned copy, holding
  only chat-capable non-deprecated models, is `go:embed`ed so the binary stays
  self-contained. `Ctrl+R` re-downloads the upstream file, parses it, and writes
  `~/.config/linuxai/models.json` only when the content actually changed; that
  file then takes precedence over the embedded copy.
- **Availability** comes from the backend's own OpenAI-compatible `/models`
  endpoint, which works for Ollama as well as NVIDIA.

Both are needed because neither is sufficient. NVIDIA's API reports only
`id`, `object`, `created`, and `owned_by` for each model, with no context
window, modality, or tool-calling information anywhere in the documented API.
The capability data on build.nvidia.com is behind an AWS WAF bot challenge and
a per-deploy URL hash, so it is not a usable source.

Conversely, appearing in `/models` does not mean a model is callable: the
endpoint advertises many IDs that return a 404 about a missing function for a
given account. Only a minority of live IDs have an exact match in the profile
catalog. So the browser shows the intersection by default, treats the
unmatched remainder as opt-in behind `Ctrl+N`, badges them `unlisted`, and says
on their card that capabilities are unknown and the model may 404. A 404 from
the backend also points the user at `linuxai --config`.

Profile IDs and live IDs are matched exactly. Fuzzy prefix matching was
measured and recovered only one additional model out of dozens, which does not
justify the risk of attaching the wrong context window to a model.

The Python literal is parsed by a small stdlib recursive-descent parser that
accepts dicts, lists, strings, ints, floats, `True`/`False`/`None`, comments,
and trailing commas, and rejects everything else. Regenerate the embedded
baseline with `go run ./internal/models/gen`.

## Verbose tracing and token usage

`-V`/`--verbose` sets a trace writer on the LLM client. `-v` was already
`--version`, so verbose takes the capital short form rather than breaking an
existing flag.

Tracing also switches on `stream_options: {"include_usage": true}`, which makes
the backend emit a final SSE chunk carrying `usage` with an empty `choices`
array. It is opt-in rather than always-on because not every OpenAI-compatible
server tolerates the field. The web agent accumulates usage across every round
it drives, so the summary covers the whole turn.

Trace lines go to stderr while the answer streams to stdout. Since the answer
carries no trailing newline until it finishes, the command wraps the trace
writer so a trace line emitted mid-stream is preceded by a newline instead of
being appended to the answer text.

An assistant turn that yields neither content nor tool calls is an error, not a
successful empty answer. Returning it as success made the command print nothing
and exit zero, which looked like a crash. The check must require both to be
empty: a turn that only requests tools legitimately has no content.

## Build roadmap

1. Minimal binary: load `.env` (stdlib), read `NVIDIA_API_KEY`, take prompt
   (arg/stdin), stream the answer. Text only.
2. Config: backend base URL, key, model, and a fast/quality toggle, all from
   env / `.env`.
3. History: JSONL sessions, `current` pointer, `--new`/`--list`/`--resume`/`--search`.
4. Image attach: clipboard or `--image`, downscale, `image_url` block.
5. SearXNG `--web` tier.
6. Trigger: tmux `display-popup` binding plus the plain command form.
7. Interactive launcher: Bubble Tea menu, prompt, thread picker, and history
  search while retaining noninteractive argument and pipe workflows.
8. Settings dialog: in-place `.env` editing with a masked key, plus a model
  browser backed by the embedded capability catalog and the live model list.
9. Verbose mode: per-request tracing and token usage on stderr.

## Environment

- Run Claude Code inside the WSL Ubuntu terminal, or natively on the Ubuntu
  workstation. Work in WSL-native paths if on WSL.
- Put secrets in `.env` (gitignored) or export them in the shell. `.env` at the
  project root is easiest for dev; `~/.config/linuxai/.env` for installed use.
