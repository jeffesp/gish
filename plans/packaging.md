# Packaging plan

Goal: installable packages for macOS, Linux and Windows, produced from the
existing tag-triggered release workflow (`.github/workflows/release.yml`).

## Current state

- `release.yml` builds static binaries (CGO off) for linux/darwin/windows x amd64/arm64.
- Version is stamped via `-X main.Version=vX.Y.Z`.
- Releases publish tar.gz/zip archives plus `SHA256SUMS.txt` to GitHub.

## Targets

| Platform | Format | Distribution |
|----------|--------|--------------|
| macOS | Homebrew formula | tap repo `jeffesp/homebrew-tap` |
| Linux | `.deb`, `.rpm` | attached to GitHub release |
| Windows | Scoop manifest, winget manifest | bucket repo `jeffesp/scoop-bucket`; PR to `microsoft/winget-pkgs` |
| Windows (later) | `.msi` via WiX | GitHub release, only if requested |

Out of scope for now: hosted apt/yum repos, Homebrew core, macOS notarization
(not needed for brew-downloaded tarballs), Windows code signing.

## Tooling

Use GoReleaser to replace the hand-rolled build/package/release steps. One
`.goreleaser.yaml` covers cross-compilation, archives, checksums, nfpm
deb/rpm, the Homebrew formula and the Scoop manifest. Winget is submitted
separately.

## Steps

### 1. GoReleaser: archives, checksums, deb, rpm
- Add `.goreleaser.yaml`:
  - `builds`: same targets as today, `CGO_ENABLED=0`, `-trimpath`,
    ldflags `-s -w -X main.Version=v{{.Version}}`.
  - `archives`: tar.gz (zip for windows), keep naming close to the current
    `gish_<version>_<os>_<arch>` scheme.
  - `checksum`: `SHA256SUMS.txt`.
  - `nfpms`: formats `deb` and `rpm`, installs to `/usr/bin/gish`, includes
    LICENSE and README, maintainer/homepage/description filled in.
- nfpm scripts (`packaging/postinst.sh`, `packaging/prerm.sh`):
  - postinst: append `/usr/bin/gish` to `/etc/shells` if not already present.
  - prerm (on remove only, not upgrade): remove that line from `/etc/shells`.
  - Scripts must be POSIX sh and idempotent. Both deb and rpm pass different
    arguments (deb: `remove|upgrade`, rpm: `0|1`), so handle both.
- Rewrite `release.yml` to run `goreleaser release --clean` on tag push,
  keeping the `--version` smoke test (run against the linux/amd64 build).
- Verify locally with `goreleaser release --snapshot --clean`, then install
  the deb in a Debian container and the rpm in a Fedora container; check
  `gish --version` and `/etc/shells` before/after install and remove.

### 2. Homebrew tap and Scoop bucket
- Create `jeffesp/homebrew-tap` and `jeffesp/scoop-bucket` repos.
- Add a fine-grained PAT (contents:write on those two repos) as a repo secret
  for the release workflow.
- GoReleaser `brews` and `scoops` sections push formula/manifest on release.
- Formula `caveats`: tell users to add `$(brew --prefix)/bin/gish` to
  `/etc/shells` and run `chsh` (brew cannot touch `/etc/shells` itself; needs sudo).
- Verify: `brew install jeffesp/tap/gish`, `scoop bucket add` + `scoop install gish`.

### 3. winget
- Write the manifest (portable zip installer type) and submit a PR to
  `microsoft/winget-pkgs` for the first release, using `wingetcreate`.
- Later releases: `wingetcreate update` in the release workflow, or accept
  the winget-releaser action if it proves low-maintenance.

### 4. MSI (deferred)
- Only if there is demand. WiX in a `windows-latest` job; needs a stable
  UpgradeCode GUID, PATH registration, and ideally code signing to avoid
  SmartScreen warnings.

## Documentation
- README: add an Install section (brew, scoop, winget, deb/rpm, manual
  download) and update the release-workflow paragraph.

## Open questions
- Package license/maintainer metadata: confirm values for nfpm.
- Whether Windows arm64 is worth shipping in winget/Scoop at first release.
