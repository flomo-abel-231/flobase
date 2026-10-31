# Running FloBase on your computer

Develop against FloBase on your own Mac or PC: the same dashboard, APIs and databases as a server, on `localhost`. There are two ways in, and both install everything FloBase needs on first start, including a Docker engine.

| | FloBase Desktop | flobase CLI |
| --- | --- | --- |
| What | An app with Start, Stop and Update buttons | A command for terminals and scripts |
| macOS | Intel and Apple silicon (M1–M4), macOS 11+ | Intel and Apple silicon, macOS 11+ |
| Windows | Windows 10 (22H2) or 11, x64 and ARM64 | Windows 10 (22H2) or 11, x64 and ARM64 |
| Linux | — | x64 and ARM64 |

Plan on 8 GB of memory or more and 20 GB of free disk; each running project database uses its plan's memory.

## What gets installed

FloBase's projects are real Postgres, MySQL and MongoDB-compatible databases running in containers, so FloBase needs a Docker engine. On first start it uses the one already on your computer, starts it if it is stopped, or installs one:

| System | Already have Docker? | No Docker |
| --- | --- | --- |
| macOS | Docker Desktop or OrbStack is used and started as needed | FloBase installs its own engine for your user: [Colima](https://github.com/abiosoft/colima) on [Lima](https://lima-vm.io) with Apple's Virtualization framework, plus the Docker CLI and Compose. The programs go in `~/.flobase/runtime` and the VM in `~/.colima/flobase`. No admin password, no Docker Desktop licence, and your own `docker` command keeps pointing where it did. Needs macOS 13 or later. |
| Windows | Docker Desktop is used and started as needed | FloBase installs WSL 2, then [Docker Desktop](https://www.docker.com/products/docker-desktop/) with `winget`. Windows asks for administrator rights, and once for a restart after WSL is installed. |
| Linux | Docker Engine is used | FloBase installs Docker Engine with [Docker's script](https://get.docker.com), asking for your sudo password, and adds you to the `docker` group (sign out and in once). |

FloBase asks before installing anything that needs your agreement. Docker Desktop is free for personal use, education, open-source projects and businesses with fewer than 250 employees and less than $10 million in revenue; larger companies need a [paid Docker subscription](https://www.docker.com/pricing/). The macOS engine FloBase installs has no such terms.

The first start also downloads FloBase and the Postgres images, about 2 GB, so it takes a few minutes. Later starts take seconds.

## FloBase Desktop

Download from the [latest release](https://github.com/flomo-abel-231/flobase/releases/latest):

- **macOS:** [FloBase-macOS.dmg](https://github.com/flomo-abel-231/flobase/releases/latest/download/FloBase-macOS.dmg), one app for Intel and Apple silicon. Open it and drag FloBase to Applications.
- **Windows x64:** [FloBase-Windows-x64-setup.exe](https://github.com/flomo-abel-231/flobase/releases/latest/download/FloBase-Windows-x64-setup.exe).
- **Windows on ARM:** [FloBase-Windows-arm64.zip](https://github.com/flomo-abel-231/flobase/releases/latest/download/FloBase-Windows-arm64.zip); unzip and run `FloBase.exe`.

Open FloBase. It installs what is missing, starts FloBase, and opens the dashboard in your browser, already signed in. The window shows the dashboard and API addresses, and has buttons to stop FloBase, update it, copy your API token, read logs and open its files. FloBase keeps running when you close the window; click **Stop FloBase** to give its memory back.

### First open on macOS and Windows

FloBase's builds are not yet signed with an Apple or Microsoft certificate, so the first open needs one extra click:

- **macOS:** if macOS says it cannot check FloBase for malicious software, open **System Settings → Privacy & Security**, scroll to the message about FloBase and click **Open Anyway**.
- **Windows:** if SmartScreen shows "Windows protected your PC", click **More info → Run anyway**.

## flobase CLI

macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install-cli.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install-cli.ps1 | iex
```

The script checks the download's checksum, installs `flobase` (to `~/.local/bin`, or `%LOCALAPPDATA%\FloBase\bin` on Windows, both added to your PATH) and runs `flobase start`.

| Command | What it does |
| --- | --- |
| `flobase setup` | Install what FloBase needs, including a Docker engine, without starting it |
| `flobase start` | Set up on first use, start FloBase and its project databases, open the dashboard (`--no-open` to skip) |
| `flobase stop` | Stop FloBase and its project databases, and FloBase's own engine on macOS; data is kept |
| `flobase status` | Whether FloBase runs, its version, URLs and API token |
| `flobase open` | Open the dashboard, signed in |
| `flobase logs` | Recent control-plane and gateway logs (`--lines N`) |
| `flobase update` | Move to the latest FloBase release |
| `flobase uninstall` | Remove FloBase, keeping data; `--all` also deletes every project, its data and `~/.flobase` |

`FLOBASE_YES=1` answers yes to every question, for scripts and CI.

## Using it

- Dashboard: <http://localhost:8080>. Sign in with the API token from `flobase status` or the app's **Copy API token** button.
- Project APIs: `http://localhost:8000/project/<ref>`, for the Supabase client libraries.
- Direct and pooled database connections: shown on each project's **Configuration → Connect** page.

Files live in `~/.flobase` (`%USERPROFILE%\.flobase` on Windows): the release's `docker-compose.yml`, `.env` with your secrets, and on macOS the engine's programs. On macOS FloBase's engine VM lives in `~/.colima/flobase`; `flobase stop` shuts it down to give its memory back, and `flobase uninstall --all` deletes it with its data disk and download cache. Set `FLOBASE_LOCAL_DIR` to use another folder.

## Updates

FloBase on your computer updates when you ask: click **Check for updates** in the app, or run `flobase update`. Download a newer FloBase Desktop or rerun the CLI install script to update the app or CLI itself. Projects and data are kept across updates.

## Troubleshooting

| Problem | What to do |
| --- | --- |
| "Another FloBase is running on this Docker engine" | A server install or a source checkout's `make up` holds the same ports. Stop it with the command in the message, then start again. |
| Ports 8080 or 8000 are in use by something else | Set `FLOBASE_PORT` and `FLOBASE_GATEWAY_PORT` in `~/.flobase/.env`, then `flobase start`. |
| macOS: the engine does not start | Check macOS is 13 or later, then `flobase logs`; delete `~/.flobase/runtime` and start again to reinstall the engine. |
| Windows: Docker Desktop does not start | Open Docker Desktop once, accept its terms, then start FloBase again. |
| Anything else | `flobase logs`, then [open an issue](https://github.com/flomo-abel-231/flobase/issues) with the output. |
