# Course thumbnail verification — 2026-09-06

Implementation: [architecture, API, storage and deployment notes](../../course-thumbnails.md).
This is local implementation/test evidence, not an independent release approval
or a production deployment. Checks ran against the working tree, including the
pre-existing landing-page changes; those changes were preserved.

## Checks

| Check | Result |
|---|---|
| Backend `go build ./...` | Passed |
| Backend `go vet ./...` and `go vet -tags=integration ./...` | Passed |
| Backend `go test -race ./...` | Passed |
| Integration: catalog, media, catalogpublic, db, httpapi, storage | All six packages passed |
| Final thumbnail ownership, publication, personalized catalogue and rollback-preflight tests | Passed |
| Frontend lint | Passed; no ESLint warnings/errors |
| Frontend typecheck | Passed |
| Frontend `npm test` | 719 passed, zero failed |
| Thumbnail browser selection | Four passed |
| Frontend `npm run build:clean` | Passed; 18 static pages generated |
| `git diff --check` | Passed |

The clean build used
`npm_config_node_options="--no-network-family-autoselection --dns-result-order=ipv4first"`
after the environment's default DNS selection timed out fetching existing
Google Fonts. No font source, landing-page styling, or production configuration
was changed for this workaround.

The integration fixtures applied the real migration chain through schema 33 to
disposable PostgreSQL databases. The migration suite exercised up/down/up with no
thumbnail history. The thumbnail test also verified that rollback preflight
refuses existing thumbnail history without dirtying the schema marker. No
production database was migrated.

## Behavioral coverage

Backend coverage includes real JPEG/PNG/WebP completion, optimized decodable
derivatives, JPEG/PNG/WebP orientation, metadata removal, SVG/type spoofing,
truncated images, empty/oversized files, tiny/extreme dimensions, pixel-count
bounds, animated PNG rejection, signed content length, course/asset ownership,
unready and wrong-purpose assets, candidate isolation, exact-revision approval,
request changes, replacement/removal, idempotency and stale selections, null
legacy covers, ordinary/personalized public projections, authorized image
delivery, approval rollback at five injected stages, audit evidence, and
cleanup retaining rejected/approved history. Storage tests cover versioned and
unversioned providers and per-object deletion failures.

Browser command, from `frontend/`:

```sh
npm run test:e2e:media-authoring -- --grep 'C an Instructor|thumbnail cards|thumbnail failure'
```

The long authoring journey uses real PostgreSQL, MinIO, API, worker, presigned
PUTs, browser file selection, processing, course submission, Admin review and
approval. It proves a replacement is not public while its candidate is draft or
pending, and becomes public after approval. It also exercises removal, upload
blocking, reload persistence, and Arabic/mobile authoring.

The other three browser tests isolate HTTP failure/reconciliation and the
English/Arabic carousel's custom/absent/broken-image behavior. Their API boundary
fixtures are deliberate; they are UI tests, not additional real-storage claims.

## Screenshots inspected

Screenshots use a deterministic raster test pattern so cropping is visible.
Instructor/Admin pages used 1280 × 720 and 1280 × 900 desktop viewports;
carousel checks used 1440 × 1000. Mobile checks used 390 × 844. Element captures
are cropped from those actual browser renders.

| Surface | Screenshot |
|---|---|
| New draft, empty control | [Empty](thumbnail-instructor-empty.png) |
| Uploaded candidate preview | [Preview](thumbnail-instructor-preview.png) |
| Replacement processing, existing cover preserved | [Replacement](thumbnail-instructor-replacement.png) |
| Arabic Instructor control | [Arabic](thumbnail-instructor-arabic.png) |
| Mobile Instructor control | [Mobile](thumbnail-instructor-mobile.png) |
| Admin's exact candidate image | [Admin review](thumbnail-admin-candidate.png) |
| Real public catalogue card after approval | [Public card](thumbnail-public-desktop.png) |
| Real public catalogue page | [Public page](thumbnail-public-page.png) |
| English carousel, desktop | [English desktop](thumbnail-carousel-en-desktop.png) |
| Arabic carousel, desktop | [Arabic desktop](thumbnail-carousel-ar-desktop.png) |
| English carousel, mobile | [English mobile](thumbnail-carousel-en-mobile.png) |
| Arabic carousel, mobile | [Arabic mobile](thumbnail-carousel-ar-mobile.png) |
| English generated fallback after image failure | [English fallback](thumbnail-fallback-en.png) |
| Arabic generated fallback after image failure | [Arabic fallback](thumbnail-fallback-ar.png) |

## Review and release boundary

Code/test/documentation guard passes ran. Fixes included bounded upload signing,
exact-source orientation handling, explicit selection recovery, retry/error
handling, per-object deletion failure propagation, and the Admin failure label.
An independent reviewer has not approved this new implementation range.

Before rollout, use a backend compatible with schema 33, confirm the deployed
Instructor-upload mode and storage deletion/version-listing permissions, and
follow the existing release gates. Cached public derivatives may remain readable
for 24 hours; draft and historical revision references retain their assets.
