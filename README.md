# ccx

[🇰🇷 한국어](README.ko.md)

A CLI wrapper that routes [Claude Code](https://docs.claude.com/en/docs/claude-code) to alternative LLM providers — z.ai GLM, Kimi, DeepSeek, MiniMax, OpenRouter, NVIDIA NIM, LM Studio, and **ChatGPT (Codex subscription)** — without touching environment variables each time.

## Install

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.sh | bash
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.ps1 | iex
```

For manual installation, download the archive for your OS/architecture from [Releases](https://github.com/channel-spoonai/ccx/releases/latest).

The install script adds `~/.local/bin` to your PATH automatically. If it isn't in your PATH yet, the script will print instructions.

**Prerequisite:** [Claude Code](https://docs.claude.com/en/docs/claude-code) CLI must be installed.

### Update

ccx keeps itself up to date automatically: it checks GitHub for new releases once a day (cached at `~/.config/ccx/update-check.json`), and when the check finds a new release, the next startup applies it automatically before the profile menu — the new version takes effect from the following run. To opt out, set `CCX_AUTO_UPDATE=0`. Non-TTY/scripted runs and installs in non-writable locations fall back to a one-line notice instead of auto-applying; dev builds (built from source) skip the update check entirely.

You can always update manually and immediately with:

```bash
ccx update
```

Re-running the install script also works — it downloads the latest binary and overwrites the existing one. This is the way to upgrade from versions too old to have `ccx update` or auto-update (v0.1.x): just run the install one-liner again.

## Usage

```bash
ccx
```

On first run, an empty menu appears. Select **+ Add new provider**, pick from the catalog, enter your API key, and it's saved to `~/.config/ccx/ccx.config.json`. From then on, use arrow keys (or number keys) to pick a profile and `claude` launches with that provider.

```text
  ccx — select a profile

   ❯ 1. GLM Coding Plan      z.ai GLM (Anthropic-compatible)
           opus   → GLM-4.7
           sonnet → GLM-4.7
           haiku  → GLM-4.5-Air
     2. Kimi (Moonshot)      Moonshot Kimi K2.5 (Anthropic-compatible)
     3. DeepSeek             DeepSeek V4 (Anthropic-compatible)
     4. MiniMax              MiniMax M2 series (Anthropic-compatible)
     5. OpenRouter           OpenRouter multi-model gateway
     6. LM Studio (local)    Local LM Studio server
     7. + Add new provider...

    ↑↓ move  Enter select  e edit  d delete  Esc cancel
```

To specify a profile directly:

```bash
ccx -xSet "GLM Coding Plan" -p "hello"
```

All arguments other than `-xSet` are passed straight through to `claude`. For example:

```bash
ccx -xSet "GLM Coding Plan" --dangerously-skip-permissions
ccx --dangerously-skip-permissions   # interactive menu, then applies the flag
```

### One-liner non-interactive mode

`-p` (Claude Code's print mode) prints the response and exits immediately. Combine with `--model` to override the model at call time:

```bash
ccx -xSet "LM Studio (local)" -p "Summarize this in 3 lines"
ccx -xSet "LM Studio (local)" --model "qwen/qwen3-coder-30b" -p "Refactoring ideas for this function"
cat src/main.go | ccx -xSet "LM Studio (local)" -p "Point out potential bugs"
```

This pattern works with any provider. All flags other than `-xSet` are forwarded verbatim to `claude`, so the full [Claude Code CLI reference](https://docs.claude.com/en/docs/claude-code/cli-reference) applies.

## Supported Providers

z.ai GLM · Kimi (Moonshot) · DeepSeek · MiniMax · OpenRouter · NVIDIA NIM · LM Studio (local) · ChatGPT (Codex) · OpenAI API

Profiles are embedded in the binary — no manual config needed. Just pick from the menu and enter your API key.

### ChatGPT (Codex) via subscription

Route Claude Code through your ChatGPT Plus/Pro/Business subscription. One-time OAuth login required.

```bash
ccx codex login                  # browser OAuth (headless: --device)
ccx -xSet "Codex"                # use after authentication
```

Model mapping (Claude Code tier → Codex model):

| Claude Code | Codex |
|---|---|
| opus   | gpt-5.6-sol |
| sonnet | gpt-5.6-terra |
| haiku  | gpt-5.6-luna |

Older IDs (`gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`) remain valid. `gpt-5.6-sol` may be unavailable on some ChatGPT plans — switch that slot to `gpt-5.6-terra` if rejected. The ChatGPT backend caps the context window at 272K regardless of the `[1m]` suffix; ccx knows this and applies `CLAUDE_CODE_AUTO_COMPACT_WINDOW=272000` automatically (see [Context windows](#context-windows)). For the full 1M window use the OpenAI API key profile below.

Status / logout: `ccx codex status` / `ccx codex logout`

### OpenAI API key (Responses API)

Prefer the pay-as-you-go OpenAI API over a ChatGPT subscription? The `openai-responses` profile routes Claude Code through the official Responses API (`https://api.openai.com/v1/responses`) with your API key — no OAuth, and no 272K cap: GPT-5.6 models get their full 1M+ context window.

```json
{
  "name": "OpenAI API",
  "auth": "openai-responses",
  "apiKey": "env:OPENAI_API_KEY",
  "models": {
    "opus": "gpt-5.6-sol[1m]",
    "sonnet": "gpt-5.6-terra[1m]",
    "haiku": "gpt-5.6-luna"
  }
}
```

`apiKey` takes a literal key or an `env:VAR` reference. Heads-up on billing: prompts over 272K input tokens are charged at OpenAI's long-context rates (2× input / 1.5× output for the whole request) — add `"env": { "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "272000" }` to the profile if you'd rather stay under that threshold.

### NVIDIA NIM (free developer tier)

[build.nvidia.com](https://build.nvidia.com/) hosts 100+ open-weight models — Nemotron 3, DeepSeek, GLM, Kimi, Llama, gpt-oss — behind one OpenAI-compatible endpoint. Sign up for the free NVIDIA Developer Program to get an `nvapi-` key (~1000 credits, ~40 RPM). Note the free tier is **account-wide, not per model**: there is no "free model" flag, and the API does not expose one.

Since NVIDIA offers no Anthropic endpoint, ccx runs its built-in `openai-chat` translation proxy (same path as lightning-mlx).

```bash
export NVIDIA_API_KEY=nvapi-...
ccx -xSet "NVIDIA NIM"
```

When you add the profile through the menu, ccx checks which models the server is actually serving and offers a curated shortlist to pick from per tier:

| Model | Context on NIM |
|---|---|
| `z-ai/glm-5.2` | 202K |
| `minimaxai/minimax-m3` | 524K |
| `nvidia/nemotron-3-ultra-550b-a55b` | 1M |
| `nvidia/nemotron-3-super-120b-a12b` | 1M |
| `deepseek-ai/deepseek-v4-flash` | 1M |
| `deepseek-ai/deepseek-v4-pro` | 262K |

The rest of the catalog is hidden: most of it can't handle Claude Code's agentic workload (tool calling over long contexts), and a good chunk isn't conversational at all (embedding, reranker, guardrail, reward, document parsing). To use a model outside the shortlist, put its ID in the profile's `models` by hand — add an `[Nk]` suffix too, since ccx only knows the windows above.

Those context numbers are **NIM's actual serving limits**, measured directly rather than taken from model cards — NVIDIA deploys with its own `--max-model-len`, so the same model differs by provider. GLM-5.2 is 1M on z.ai but 202K here; DeepSeek V4 Pro is 1M on DeepSeek but 262K here. ccx records the measured value as a suffix when you add the profile, so Claude Code compacts before overflowing.

## Context windows

Claude Code assumes a 200K context window for model IDs it doesn't recognize. That mismatches most third-party models — a 128K local model overflows hard, a 1M DeepSeek model gets underused. ccx fixes this per model:

- **Declare it in the model ID**: append `[131k]`, `[262k]`, `[1m]`-style suffixes in your profile's `models` (e.g. `"sonnet": "kimi-k2.5[262k]"`). ccx translates the suffix into what Claude Code actually understands — the official `[1m]` marker and/or a `CLAUDE_CODE_AUTO_COMPACT_WINDOW` cap — and strips it so it never reaches the provider.
- **Or let the catalog handle it**: for known models (GLM, Kimi, DeepSeek, MiniMax, GPT-5.5/5.6, and the NVIDIA NIM line-up) ccx ships accurate numbers built in; profiles without suffixes still get the right window — and the catalog's exact values (e.g. Kimi 262,144) beat what a `[262k]` floor suffix can express, so leave known models unsuffixed.
- **Auto-detection on add**: when adding OpenRouter or LM Studio profiles through the menu, ccx queries the provider for the real context length (for LM Studio, the actually-loaded context) and records it as a suffix for you. Caveat for OpenRouter: the recorded value is the smaller of the model's advertised length and its top-ranked provider's — load-balanced requests can still land on a lower-ranked provider serving less; pin a smaller `[Nk]` suffix yourself if that bites you.

The launch banner shows what was resolved, e.g. `Context: sonnet 262k (suffix) → auto-compact 262144`. Your own settings always win: an explicit `CLAUDE_CODE_AUTO_COMPACT_WINDOW` in the profile `env` or your shell disables the computed value (if both are set, the profile `env` is what reaches Claude Code). Set `CCX_CONTEXT_AUTO=0` to turn the feature off — ccx then only strips its own `[Nk]` notation from model IDs so requests stay valid, and applies no context settings (including the automatic Codex 272K cap).

Note: since the auto-compact window is a single per-session value, ccx uses the smallest window among the opus/sonnet (and top-level `model`) slots — haiku is excluded, as it only serves background calls and a tiny haiku model would needlessly cap the whole session; the banner warns instead.

## Notes

- **LM Studio quality varies by model.** Many local models don't fully support tool use or long contexts.
- Config is stored at `~/.config/ccx/ccx.config.json` with `0600` permissions. Override the path with `CCX_CONFIG`.

## Build from source

Requires Go 1.26+ (matches `go.mod`).

```bash
git clone https://github.com/channel-spoonai/ccx.git
cd ccx
./build.sh       # outputs dist/ccx-<os>-<arch>
cp dist/ccx-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') ~/.local/bin/ccx
chmod +x ~/.local/bin/ccx
```

## License

MIT
