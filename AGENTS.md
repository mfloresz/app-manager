## Releases & tagging

- Tags must use the `v` prefix (e.g. `v0.1.0`, `v1.2.3`) to trigger the CI release workflow in `.github/workflows/build.yml`.
- The workflow pattern is `v*` — tags like `0.1.0` (without `v`) will **not** trigger the build/release pipeline.
- The workflow builds binaries for linux-amd64, linux-arm64, linux-armv7, android-arm64, and android-armv7, then creates a GitHub Release with all artifacts attached.
- The version number must already have been updated before creating the tag.
- Use annotated tags only: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`. Never create lightweight tags.

### Release workflow (ask the agent)

When asked "create release vX.Y.Z", the agent should:

1. **Determine version** — Run `git tag -l 'v*' --sort=-v:refname | head -1` to find the current version. Use the version provided by the user (e.g. `v0.2.0`).
2. **Review changes** — Run `git log --oneline vPREV..HEAD` and `git diff --stat vPREV..HEAD` to understand what changed.
3. **Stage & commit** — `git add -A` then `git commit -m "chore: prepare release vX.Y.Z"`.
4. **Tag** — `git tag -a vX.Y.Z -m "Release vX.Y.Z"` (annotated tag only, never lightweight).
5. **Push** — `git push origin main --tags`.
6. **Generate changelog** — Write the changelog for the GitHub Release. See `## Changelog` below.

## Changelog

Release notes should use the following sections when applicable, in this order:

- **⚠️ Breaking changes** (prefixed with ⚠️) — any migration steps or config changes required.
- **## What's new** — user-facing features and improvements.
- **## Fixes** — bug fixes.
- **## Housekeeping** — internal refactoring, dependency updates, docs removal, CI changes.

Do not create empty sections. Keep entries concise and user-focused. Group related changes into a single bullet when appropriate. Avoid implementation details unless they affect users.

Every item must correspond to an actual code change — do not invent release notes.

When generating the changelog, run `git log --oneline vPREV..HEAD` and `git diff vPREV..HEAD --stat` against the previous tag. Reference the previous tag URL at the bottom (e.g. `https://github.com/mfloresz/yara/releases/tag/vPREV`).

**The changelog must always be written in English**, even if the user gave instructions in another language. The GitHub Release is public and English is the project's canonical language for release notes.
