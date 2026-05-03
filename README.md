# ccx

[🇰🇷 한국어](README.ko.md)

A CLI wrapper that routes [Claude Code](https://docs.claude.com/en/docs/claude-code) to alternative LLM providers — z.ai GLM, Kimi, DeepSeek, MiniMax, OpenRouter, LM Studio, and **ChatGPT (Codex subscription)** — without touching environment variables each time.

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

If ccx is already installed, update to the latest release with:

```bash
ccx update
```

ccx checks GitHub for new releases once a day and shows a one-line notice in the menu header when an update is available (cached at `~/.config/ccx/update-check.json`).

Re-running the install script also works — it downloads the latest binary and overwrites the existing one.

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

z.ai GLM · Kimi (Moonshot) · DeepSeek · MiniMax · OpenRouter · LM Studio (local) · ChatGPT (Codex)

Profiles are embedded in the binary — no manual config needed. Just pick from the menu and enter your API key.

### ChatGPT (Codex) via subscription

Route Claude Code through your ChatGPT Plus/Pro/Business subscription. One-time OAuth login required.

```bash
ccx codex login                  # browser OAuth (headless: --device)
ccx -xSet "ChatGPT (Codex)"      # use after authentication
```

Model mapping (Claude Code tier → Codex model):

| Claude Code | Codex |
|---|---|
| opus   | gpt-5.5 |
| sonnet | gpt-5.4 |
| haiku  | gpt-5.4-mini |

Status / logout: `ccx codex status` / `ccx codex logout`

## Notes

- **LM Studio quality varies by model.** Many local models don't fully support tool use or long contexts.
- Config is stored at `~/.config/ccx/ccx.config.json` with `0600` permissions. Override the path with `CCX_CONFIG`.

## Build from source

Requires Go 1.21+.

```bash
git clone https://github.com/channel-spoonai/ccx.git
cd ccx
./build.sh       # outputs dist/ccx-<os>-<arch>
cp dist/ccx-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') ~/.local/bin/ccx
chmod +x ~/.local/bin/ccx
```

## License

MIT
