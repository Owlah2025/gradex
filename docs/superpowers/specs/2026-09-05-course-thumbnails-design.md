# Course thumbnails

Optional course thumbnails follow the existing candidate/live revision workflow. A
candidate copies its live thumbnail reference; replacing or clearing that reference
never changes the live revision. Approval publishes the exact candidate by the
existing transactional pointer switch.

Reuse media_assets, media_asset_versions, upload_intents, exact-version completion,
and the private S3-compatible storage client. Add a THUMBNAIL purpose and revision
origin binding. Only fully decoded, bounded raster images may produce immutable
card and large JPEG derivatives. Original uploads are never served. Accept JPEG,
PNG and static WebP, at most 5 MiB and 16 million pixels, maximum side 8192;
require an oriented image of at least 800 by 450. Recommend 16:9 and 1200 by 675.
Variants fit within 800 by 800 and 1440 by 1440 without upscaling. Keeping the
source ratio lets existing fixed-height cards apply their unchanged center crop.

Public image routes verify the current published revision and expose only its
ready thumbnail. Authenticated Instructor and Admin image routes verify the
requested revision. URLs include immutable asset identity. Public derivatives may be cached for 24 hours under immutable URLs; new requests
revalidate live authority. Private previews are no-store.

Instructor controls use existing buttons, borders, focus styles and dictionaries.
The post-create draft studio is the upload stage because an owned course and exact
revision must exist before an upload intent. Upload, processing, replacement,
removal and failures are visible; pending or unresolved selection blocks submit.
Admin review compares live and candidate covers. Generated subject art remains
the public fallback, including on image failure. Detail layout is unchanged.

Cleanup retires unreferenced thumbnail assets after a grace period and retries
object deletion. Every revision reference, including historical revisions, keeps
the asset alive. Database metadata and audit evidence are retained.

Implementation order: schema/media pipeline; revision attachment and publication
validation; authorized delivery/public projection; bilingual authoring/review/card
UI; orphan collection; backend/frontend/E2E tests and rendered verification.
