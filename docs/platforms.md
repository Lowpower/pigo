# Platforms and terminal setup

Release archives are listed in the repository README. After install, put
`pigo` on your `PATH` and run `pigo auth login <provider>`.

## Windows

Use `pigo_*_windows_amd64.zip` or `pigo_*_windows_arm64.zip`.

**bash:** the built-in `bash` tool looks for Git for Windows
(`%ProgramFiles%\Git\bin\bash.exe`, then the x86 program files path), then
`bash.exe` on `PATH`. If nothing is found, the tool errors. Set
`settings.shellPath` to an explicit executable. Legacy
`System32` / `Sysnative` WSL `bash.exe` is invoked with stdin (`-s`).

**powershell:** Windows-only built-in tool (`pwsh.exe`, then
`powershell.exe`). Docker isolation does not wrap it; it stays on the host.

**OS sandbox** does not wrap bash on Windows. **Docker** tool isolation still
works if the Docker CLI can reach a daemon; see
[tools.md](tools.md#docker-tools-in-container).

`pigo server` / `pigo client` use a Unix-domain socket (`unix://path`).

Interactive mode needs a real TTY (Windows Terminal, conhost, or similar).
Mouse copy: `--tui-mode regular` (default) leaves selection to the terminal;
`fullscreen` uses the alternate screen (`fullscreenCopyOnSelect`,
`Ctrl+X`).

Copy also writes the Windows clipboard from WSL (`WSL_DISTRO_NAME` /
`WSLENV`) via PowerShell UTF-8, in addition to OSC 52.

## Termux

There is no Termux-specific code path. Use the Linux arm64 (or amd64)
release, or `go install github.com/Lowpower/pigo/cmd/pigo@latest` if Go is
installed. Config still lives under `$HOME/.pigo/agent`.

The TUI needs a TTY. `-p` / `--mode json` / `--mode rpc` work without one.

OS sandbox wrapping needs Linux `bwrap`; if it is missing, leave sandbox
disabled (the default). Docker isolation needs a Docker CLI and daemon, which
Termux usually does not provide.

OAuth loopback is `127.0.0.1` (`PIGO_OAUTH_CALLBACK_HOST`). Device-code
logins avoid a local callback.

## tmux

pigo runs inside tmux like any other TUI. Inline terminal images are
**off** when `TMUX` is set or `TERM` starts with `tmux` or `screen` (kitty
passthrough is not assumed). Force a protocol with `PIGO_IMAGE_PROTOCOL`
(`kitty` / `iterm2` / `off`) if your outer terminal actually supports it.

OSC 8 hyperlinks and true color still follow the settings below.

## Terminal setup

| Knob | Values | Default |
| --- | --- | --- |
| `PIGO_HYPERLINKS` / `settings.terminal.hyperlinks` | `on` / `off` / `auto` | `auto` (on for a TTY) |
| `PIGO_TRUE_COLOR` / `settings.terminal.trueColor` | `on` / `off` / `auto` | `auto` |
| `PIGO_IMAGE_PROTOCOL` / `settings.terminal.images` | `kitty`, `iterm2`, `off`, `auto` | auto-detect (Kitty, Ghostty, WezTerm, Warp → kitty; iTerm → iterm2) |
| `--tui-mode` / `settings.tuiMode` | `regular` / `fullscreen` | `regular` |
| `settings.terminal.showImages` | bool | TUI image display |
| `settings.terminal.imageWidthCells` | int | 60 |

`PIGO_*` wins over `PI_*` and over `settings.json`. See
[environment.md](environment.md) and [settings.md](settings.md).

`terminal.showTerminalProgress` emits OSC 9;4 when enabled.
`terminal.clearOnShrink` clears on a smaller size.

## Shell aliases

Cobra ships `pigo completion` for bash, zsh, fish, and PowerShell:

```bash
pigo completion bash > /etc/bash_completion.d/pigo   # or source from ~/.bashrc
pigo completion zsh  > "${fpath[1]}/_pigo"
```

Short flags (rewritten before Cobra):

| Short | Long |
| --- | --- |
| `-nt` | `--no-tools` |
| `-nbt` | `--no-builtin-tools` |
| `-ns` | `--no-skills` |
| `-nc` | `--no-context-files` |
| `-ne` | `--no-extensions` |
| `-xt` | `--exclude-tools` |
| `-na` | `--no-approve` |
| `-np` | `--no-prompt-templates` |

Also: `-p` `--print`, `-c` `--continue`, `-r` `--resume`, `-n` `--name`,
`-e` `--extension`, `-t` `--tools`, `-a` `--approve`, `-v` `--version`.

Examples:

```bash
alias pigo='pigo --model anthropic/claude-sonnet-4'
alias pigo-ro='pigo -t read,grep,find,ls'
```

`settings.shellCommandPrefix` prepends a string to every **bash tool**
command (not to your interactive shell alias).
