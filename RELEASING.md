# Releasing FloBase

A release is a git tag. Pushing `vMAJOR.MINOR.PATCH` runs the [Release workflow](.github/workflows/release.yml), which:

1. builds the server image for `linux/amd64` and `linux/arm64` and pushes it to `ghcr.io/flomo-abel-231/flobase` as `0.2.0`, `0.2` and `latest`, and creates a **draft** GitHub Release with `docker-compose.yml`, `install.sh`, `upgrade.sh`, `env.example` and the CLI install scripts;
2. builds the `flobase` CLI for macOS, Linux and Windows on x64 and ARM64, with `checksums.txt`;
3. builds FloBase Desktop: `FloBase-macOS.dmg` (universal, on a macOS runner), `FloBase-Windows-x64-setup.exe` and `FloBase-Windows-arm64.zip` (on a Windows runner);
4. publishes the release once every file is attached, with notes generated from merged pull requests.

If any build fails, the release stays a draft: fix the problem, delete the draft and the tag, and tag again.

Every self-hosted FloBase with automatic updates on installs the release within six hours. A release is therefore a deploy to every user: only tag what passed CI.

## Steps

1. Make sure CI is green on `main`.
2. Pick the version with [semantic versioning](https://semver.org): patch for fixes, minor for features, major for changes that need users to act.
3. Add a section for it at the top of [CHANGELOG.md](CHANGELOG.md) and commit.
4. Tag and push:

   ```bash
   git tag v0.2.0
   git push origin main v0.2.0
   ```

5. Watch the run under **Actions → Release**. It takes about fifteen minutes.
6. Check the release page lists every file (server files, six CLI archives, checksums, the macOS disk image and the two Windows downloads), then try it on a test server:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install.sh | sudo FLOBASE_VERSION=0.2.0 bash
   ```

## The first release only

GitHub can create the container package as private. After the first release, open **Packages → flobase → Package settings** on GitHub and make sure the visibility is **Public**; otherwise installs fail to pull the image.

## Signing the desktop apps

FloBase Desktop is not yet signed, so macOS (Privacy & Security → Open Anyway) and Windows (SmartScreen → Run anyway) ask users to confirm the first open; [docs/local.md](docs/local.md) explains this. To remove those prompts:

- **macOS:** join the Apple Developer Program, create a Developer ID Application certificate, and add `codesign --options runtime` and `xcrun notarytool submit --wait` plus `xcrun stapler staple` steps to the `desktop-macos` job, with the certificate and an app-specific password as repository secrets.
- **Windows:** buy a code-signing certificate (or use Azure Trusted Signing) and sign `FloBase.exe` and the installer with `signtool` in the `desktop-windows` job.

## Changes that need care

- **Platform database changes** go in a new file in `control-plane/internal/store/migrations/`. They run on start-up, so they must work on the previous release's data and be safe to run twice. `upgrade.sh` dumps the database before each upgrade, but a rollback restores the previous binary, not the old schema, so prefer adding columns over changing or dropping them.
- **New engine or service image versions** (in `control-plane/internal/provisioner/`) apply to projects created after the update; existing projects keep their containers.
- **A bad release:** tag a fixed patch release right away. Instances on the bad version update to it on their next check, or users run `sudo sh /data/flobase/upgrade.sh`.
