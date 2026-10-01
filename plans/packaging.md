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
| macOS | Homebrew cask | tap repo `jeffesp/homebrew-tap` |
| Linux | `.deb`, `.rpm` | attached to GitHub release |
| Windows | Scoop manifest | bucket repo `jeffesp/scoop-bucket` |

Out of scope: winget and MSI (decided not to pursue; Windows users have Scoop
or the zip), hosted apt/yum repos, Homebrew core, macOS notarization (the cask
clears quarantine instead), Windows code signing.

## Tooling

Use GoReleaser to replace the hand-rolled build/package/release steps. One
`.goreleaser.yaml` covers cross-compilation, archives, checksums, nfpm
deb/rpm, the Homebrew cask and the Scoop manifest.

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
Config is in `.goreleaser.yaml` (`homebrew_casks`, `scoops`); the release
workflow passes `TAP_GITHUB_TOKEN` to GoReleaser.
- `brews` (formulae) is deprecated in GoReleaser 2.18 in favor of
  `homebrew_casks` for prebuilt binaries, so macOS installs via a cask:
  `brew install jeffesp/tap/gish`.
- Casks are quarantined by Gatekeeper and the binary is unsigned, so the cask
  has a postflight hook that strips `com.apple.quarantine`.
- Caveats tell users to add gish to `/etc/shells` and run `chsh` (brew cannot
  do this itself; it needs sudo).
- Manual setup (one time):
  - Create public repos `jeffesp/homebrew-tap` and `jeffesp/scoop-bucket`
    (each needs an initial commit so the default branch exists).
  - Create a fine-grained PAT with contents:write on those two repos and add
    it to jeffesp/gish as the Actions secret `TAP_GITHUB_TOKEN`.
- Verify after the first tagged release: `brew install jeffesp/tap/gish`,
  `scoop bucket add jeffesp https://github.com/jeffesp/scoop-bucket` +
  `scoop install gish`.

## Documentation
- README: add an Install section (brew, scoop, deb/rpm, manual
  download) and update the release-workflow paragraph.

## Open questions
- Package license/maintainer metadata: confirm values for nfpm.
- Whether Windows arm64 is worth shipping in Scoop at first release.
