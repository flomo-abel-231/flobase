# Releasing FloBase

A release is a git tag. Pushing `vMAJOR.MINOR.PATCH` runs the [Release workflow](.github/workflows/release.yml), which:

1. builds the image for `linux/amd64` and `linux/arm64` and pushes it to `ghcr.io/flomo-abel-231/flobase` as `0.2.0`, `0.2` and `latest`;
2. creates a GitHub Release with `docker-compose.yml`, `install.sh`, `upgrade.sh` and `env.example`, plus notes generated from merged pull requests.

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

5. Watch the run under **Actions → Release**. It takes about ten minutes.
6. Check the release page lists the four files, then try it on a test server:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install.sh | sudo FLOBASE_VERSION=0.2.0 bash
   ```

## The first release only

GitHub can create the container package as private. After the first release, open **Packages → flobase → Package settings** on GitHub and make sure the visibility is **Public**; otherwise installs fail to pull the image.

## Changes that need care

- **Platform database changes** go in a new file in `control-plane/internal/store/migrations/`. They run on start-up, so they must work on the previous release's data and be safe to run twice. `upgrade.sh` dumps the database before each upgrade, but a rollback restores the previous binary, not the old schema, so prefer adding columns over changing or dropping them.
- **New engine or service image versions** (in `control-plane/internal/provisioner/`) apply to projects created after the update; existing projects keep their containers.
- **A bad release:** tag a fixed patch release right away. Instances on the bad version update to it on their next check, or users run `sudo sh /data/flobase/upgrade.sh`.
