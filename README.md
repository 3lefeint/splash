# Splash

A self-hosted strategy app for endurance races in rFactor 2.

Splash reads telemetry and lap data live from the driver and spotter PCs and condenses them in a web dashboard into
fuel use, tire condition, opponent strategy and the stint plan.

## Goal

At any time the team sees:

- the best next pit stop,
- the fuel-saving target needed to reach it,
- the net effect of every strategy decision.

## Releases

Backend and agent are released separately from this repository, each with its own tag series:

- `vX.Y.Z` builds and pushes the backend image to `ghcr.io`.
- `agent-vX.Y.Z` builds the Windows agent and publishes `splash-agent.exe` with a SHA-256 checksum as a GitHub release.
  The version in `agent/pyproject.toml` and `agent/src/splash_agent/__init__.py` must equal the tag, otherwise the build
  fails. The release notes come from the `## [agent-vX.Y.Z]` section of `CHANGELOG.md`.

To release, rename `## [Unreleased]` in `CHANGELOG.md` to the tag name and add a fresh `## [Unreleased]`, raise the
version where needed, commit, and tag.

## Status

Early development.
