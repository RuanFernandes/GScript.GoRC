# Ubuntu/Debian `.deb` Release Design

**Date:** 2026-09-21

**Status:** Approved by the user for implementation

## Goal

Publish an Ubuntu/Debian amd64 `.deb` installer for GoRC in addition to the existing Linux x64 AppImage, expose both choices on the Nullborne RC download portal, and carry the new artifact through the existing GitHub Release and production deployment pipeline.

## User intent and constraints

- The AppImage remains the general Linux installer.
- The new `.deb` targets Ubuntu/Debian amd64 distributions.
- The `.deb` is a release artifact, not a replacement for the AppImage.
- The GoRC release workflow must publish and deploy the `.deb` with the other release artifacts.
- The website must make the two Linux x64 formats distinguishable and downloadable.
- The current website deployment model (VPS upload, Docker Compose rebuild, production API verification) remains in place.

## Architecture

The GoRC Linux amd64 release job will reuse the existing Wails/nfpm packaging path. It will continue to produce `nullbornes-rc-linux-x64.AppImage` and additionally normalize the generated nfpm package to `nullbornes-rc-linux-x64.deb`. The publish job and the site deployment job already consume all `nullbornes-rc-*` artifacts, so the new file will flow through those jobs without a second release channel.

The website will model the Debian package as a separate distribution target, `ubuntu/amd64`, while preserving the existing `linux/amd64` AppImage route and API contract. This avoids content negotiation or ambiguous Linux links: users explicitly choose the portable AppImage or the Ubuntu/Debian package. The target will use the canonical filename `nullbornes-rc-linux-x64.deb`, environment variable `NULLBORNES_RC_UBUNTU_AMD64_FILE`, and route `/ubuntu/amd64`.

## Release data flow

```text
GoRC tag
  -> linux-amd64 job
     -> AppImage + .deb
        -> upload-artifact
           -> publish GitHub Release
           -> deploy-site uploads both files to VPS downloads/
              -> Docker Compose rebuilds portal
                 -> /api/downloads and /ubuntu/amd64 expose the package
```

The existing runtime version environment variables continue to identify the deployed release. The website changelog will include the current 5.2.2 release entry, while later tags can still be injected by `NULLBORNES_RC_LATEST_VERSION` and `NULLBORNES_RC_RELEASE_DATE` during deployment.

## GoRC changes

- Extend `.github/workflows/release.yml` in the Linux amd64 job to invoke `task linux:create:deb ARCH=amd64` after the build metadata is set.
- Normalize the generated package to `release/nullbornes-rc-linux-x64.deb` and upload it as a release artifact alongside the AppImage.
- Add a deploy verification for the `.deb` filename in the production download API.
- Keep the existing `nfpm` metadata and Ubuntu/Debian runtime dependencies; `build/ci/set-version.mjs` already updates the package version for tagged builds.
- Document the two Linux distribution options in the GoRC release/build documentation.

## Website changes

- Add an `ubuntu` platform target with architecture `amd64` to `server.js`.
- Serve `/ubuntu` as an amd64 compatibility alias and `/ubuntu/amd64` as the canonical package route.
- Expose package metadata (`format`, size, SHA-256, availability, and download URL) through `/api/downloads` and `/update` when `platform=ubuntu&arch=amd64` is requested.
- Add `.env`/`.env.example` and installer-directory documentation for the package path.
- Add a visible Ubuntu/Debian x64 `.deb` download card while retaining the Linux x64 AppImage card.
- Add tests covering package availability, route content-disposition/content type, update metadata, and the complete download matrix.
- Add the 5.2.2 release entry to `data/changelog.json` with the existing release notes.

## Deployment behavior

The release workflow remains the only production uploader. Its glob already transfers every normalized `nullbornes-rc-*` artifact, so the `.deb` will be copied to `/opt/nullborne-rc-webpage/downloads/`, followed by the existing `docker compose up --build -d`. Production verification will require both the AppImage and `.deb` entries in `/api/downloads`, plus the existing updater checks.

The `.deb` is not committed to the website repository: release artifacts are runtime data and are uploaded by CI to the VPS download volume.

## Testing and acceptance criteria

- The website test suite proves the `.deb` target appears in the download matrix and is unavailable until its file exists.
- The website test suite proves `/ubuntu/amd64` serves the package with the expected filename and Debian content type once published.
- The website test suite proves Ubuntu update metadata resolves to `/ubuntu/amd64`.
- The GoRC workflow contains a Linux amd64 `.deb` build, upload, and production verification path without removing the AppImage path.
- The GoRC package metadata passes a local dry-run/build check when the Wails CLI and Linux GUI dependencies are available.
- The website build/test checks pass, and the production deployment verification checks both Linux formats.

## Non-goals

- No RPM, Arch, ARM64, or 32-bit `.deb` artifact is added.
- No automatic Linux updater behavior is changed; the package is a manual download option.
- No change is made to the existing AppImage filename or route.
- No separate hosting service or website deployment mechanism is introduced.
