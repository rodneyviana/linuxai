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
- Everything the client needs is in the standard library: `net/http` (HTTPS with
  TLS built in), `encoding/json`, `encoding/base64`. No third-party packages.
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
| `qwen/qwen3.5-122b-a10b` | 10B / 122B MoE | text, image, video | 262K | Default. Best balance of speed and quality; strong at code. |
| `nvidia/nemotron-nano-12b-v2-vl` | 12B | text, multi-image | 128K | Fast tier. Best OCR / document intelligence for reading pasted terminal screenshots. |
| `nvidia/nemotron-3-nano-omni` (30B-A3B) | ~3B / 30B MoE | text, image, video, audio | 256K | Efficiency champion; use if you want maximum tokens/sec. |
| `mistralai/ministral-14b-instruct-2512` | 14B | text, image (up to 10) | 262K | Light, snappy alternative. |
| `moonshotai/kimi-k2.6` | 32B / 1T MoE | text, image, video | 262K | Quality escalation for hard questions; heavier and slower. |

Recommended default wiring: `qwen3.5-122b-a10b` as the daily driver,
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

Web-augmented (`--web`): fetch top SearXNG results and prepend a grounding
block with each result's title, url, and snippet, then the user's question,
with an instruction to use and cite the sources.

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
- Take top 3-5 results (title, url, content). If snippets are thin, optionally
  fetch the full text of the single top result before answering.
- Toggle via `--web` flag or a `/web ` prefix in the prompt.

## History and sessions

- One JSONL file per conversation:
  `~/.local/share/linuxai/chats/<id>.jsonl`, one message per line:
  `{ "role": ..., "content": ..., "image": ..., "ts": ... }`.
- A `current` pointer file (`~/.local/share/linuxai/current`) names the active
  thread. Bare invocation continues it; `--new` starts a fresh one and
  repoints.
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

## Config and .env loading

All configuration is read from environment variables:

- `NVIDIA_API_KEY` — API key for the hosted endpoint.
- `LINUXAI_BASE_URL` — override the backend base URL (default the NVIDIA one;
  set to `http://localhost:11434/v1` for Ollama).
- `LINUXAI_MODEL` — default model string.
- `LINUXAI_SEARXNG_URL` — SearXNG host for the `--web` tier.

Support a `.env` file for these, but write the loader yourself in stdlib. Do
**not** add `github.com/joho/godotenv` or any module; a `.env` parser is about
30 lines and pulling a dependency would break the single-static-binary rule.

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

## Build roadmap

1. Minimal binary: load `.env` (stdlib), read `NVIDIA_API_KEY`, take prompt
   (arg/stdin), stream the answer. Text only.
2. Config: backend base URL, key, model, and a fast/quality toggle, all from
   env / `.env`.
3. History: JSONL sessions, `current` pointer, `--new`/`--list`/`--resume`/`--search`.
4. Image attach: clipboard or `--image`, downscale, `image_url` block.
5. SearXNG `--web` tier.
6. Trigger: tmux `display-popup` binding plus the plain command form.

## Environment

- Run Claude Code inside the WSL Ubuntu terminal, or natively on the Ubuntu
  workstation. Work in WSL-native paths if on WSL.
- Put secrets in `.env` (gitignored) or export them in the shell. `.env` at the
  project root is easiest for dev; `~/.config/linuxai/.env` for installed use.
