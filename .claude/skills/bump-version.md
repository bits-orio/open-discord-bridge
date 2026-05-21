# Bump Version

## When to use
When the user asks to bump the version, release a new version, or after the version in `info.json` has been changed.

## Steps

1. **Bump the version in `info.json`** if not already done. **Default to a patch bump** (e.g. `0.4.4` → `0.4.5`). Only do a minor or major bump if the user explicitly asks for one — do not infer from the diff.

   Semver components (https://semver.org/), given a version `MAJOR.MINOR.PATCH`:
   - **PATCH** — backwards-compatible bug fixes. Default. Increment the third number; reset nothing. `0.4.4` → `0.4.5`.
   - **MINOR** — backwards-compatible new functionality. Only when the user requests it. Increment the second number; reset patch to 0. `0.4.5` → `0.5.0`.
   - **MAJOR** — incompatible / breaking changes (save-format breakage, removed commands, changed remote interface, etc.). Only when the user requests it. Increment the first number; reset minor and patch to 0. `0.5.0` → `1.0.0`.

2. **Verify user-facing docs** are still accurate for the changes since the last release. Check these surfaces, in this order:
   - `README.md` — the canonical, external-facing description (mod portal viewers, GitHub readers).
   - Any in-game About/welcome panel the mod ships (e.g. a `gui/welcome.lua`), if present.

   These don't have to be identical — the README is longer and may include rationale, compatibility tables, and mod-author APIs that don't belong in a small in-game panel. An in-game panel should be a terser subset focused on what a player needs to know to play. All must be **correct**: no claim that contradicts current behavior.

   If a recent commit added a feature, removed a command, changed a panel, or expanded compatibility, update the relevant files. Do not invent or expand claims to features that have not been tested — when in doubt, keep any in-game panel conservative and let the README reflect tested-but-narrower scope. Show any doc edits to the user for approval before committing.

3. **Generate a changelog entry** at the top of `changelog.txt`:
   - Determine the previous version's git tag (format: `v<old_version>`). If no tag exists, use `git log` to find commits since the last changelog entry.
   - Collect the diff: `git log --pretty=format:"- %s" v<old_version>..HEAD` (exclude "Bump version" commits).
   - Write a new entry at the **top** of `changelog.txt` following the existing format exactly:
     ```
     ---------------------------------------------------------------------------------------------------
     Version: <new_version>
     Date: <YYYY-MM-DD>
       Features:
         - ...
       Changes:
         - ...
       Bugfixes:
         - ...
     ```
   - Only include sections (Features, Changes, Bugfixes) that have entries. Categorize each commit appropriately. Reword commit messages into clear, user-facing descriptions — don't just paste raw commit subjects.
   - Show the draft entry to the user for approval before writing it.

4. **Recreate mod symlinks** by running:
   ```bash
   ./link-mod.sh
   ```
   This removes the mod's old symlinks (matching the mod name in `info.json`) and creates new ones with the current version in both `~/factorio/mods/` and `~/.factorio/mods/`.

5. **Commit the version bump**: stage `info.json`, `changelog.txt`, and any `README.md` / `gui/welcome.lua` edits from step 2. Commit with message: `Bump version to <new_version>` (or `Release <new_version>: <one-line summary>` if substantial doc/feature work shipped — match the recent commit history's style).

6. **Release** (when the user asks): push the bump commit, then run `./tools/release.sh`. The script verifies the changelog entry, creates and pushes `v<new_version>`, and the GitHub Actions workflow takes over (build zip → GitHub release → Discord → mod portal upload).
   - If the mod-portal upload step fails (portal outage, etc.), the GH release and tag remain. Re-run the upload via the **Upload to Mod Portal** workflow (Actions tab → workflow_dispatch). The upload script is idempotent — it noops if the version is already published.
   - Required secrets on the GitHub repo: `FACTORIO_API_KEY` (scope: ModPortal: Upload Mods), and optionally `DISCORD_WEBHOOK` and `DISCORD_ANNOUNCEMENTS_WEBHOOK`.
