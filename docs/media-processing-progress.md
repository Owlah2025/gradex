# Video processing progress

Two different things are measured while an Instructor uploads a lesson video,
and they are never merged into one number.

| Measure | Who measures it | Where it comes from | When it is shown |
|---|---|---|---|
| **Upload progress** | The browser | `XMLHttpRequest.upload.onprogress` on the direct PUT to private object storage | While bytes are being sent |
| **Processing progress** | The worker | FFmpeg's structured `-progress` stream, against the ffprobe duration of the exact stored object | While the Asset Version is `PROCESSING` |

The bytes never pass through the Go API, so the API cannot observe the upload;
the browser never sees the transcode, so it cannot observe the processing. Each
measure is reported by the only party that can actually take it.

Nothing is estimated. There is no timer anywhere that advances a bar, and a
transcode that stalls stops advancing rather than creeping toward a number
nobody measured. See
[D-098](DECISIONS.md#d-098--video-processing-reports-real-persisted-progress).

## Where the percentage comes from

The processor renders the HLS ladder one rung at a time, each rung over the full
duration of the source. FFmpeg is invoked with `-progress pipe:1 -nostats`, which
emits `key=value` blocks terminated by `progress=continue` or `progress=end`. The
worker reads `out_time_us` — the media time written so far — and maps it onto the
whole attempt:

```
percent = (finished rungs + this rung's fraction) / total rungs × 100
```

`ffprobe` supplies the duration, from the same exact object version the transcode
reads. FFmpeg's human-oriented stderr status line is never parsed: it is a
presentation format FFmpeg is free to change, and a structured stream already
exists. The parser is `scanFFmpegProgress` in `internal/media/progress.go` and is
tested against a fixture of real FFmpeg output.

The transcoding phase is capped below 100. Reaching 100 belongs to the attempt
that finished, not to the last rung that is nearly done.

## Stages

Only the two phases the pipeline actually has are representable:

| Stage | What is happening | Measured |
|---|---|---|
| `TRANSCODING` | The HLS ladder is being encoded | Yes |
| `PACKAGING` | The finished output is being uploaded to private storage | No — it reports the point transcoding reached |

Quarantine, scanning, and validation are separate **Asset Version states**, not
processing stages. They are already visible in `state` and are deliberately not
restated here as fabricated percentages. The client renders them from `state`
with stage-only copy and an indeterminate progress bar.

## Persistence

Progress lives on `media_asset_versions` (migration `0034_media_processing_progress`):

| Column | Meaning |
|---|---|
| `processing_stage` | `TRANSCODING` or `PACKAGING`; constrained, so an unknown stage cannot be stored |
| `processing_progress_percent` | `SMALLINT`, constrained to 0–100 |
| `processing_updated_at` | When the observation was taken |
| `processing_attempt_token` | The operation identity of the attempt these numbers describe |

All four are written together or not at all. Existing rows are left null: an
asset that was already `READY` has nothing to report, and one that was mid-flight
reports nothing until its next attempt writes an observation — which is the truth,
rather than a fabricated zero.

It is not a side table because one in-flight attempt over one immutable object is
a property of that Asset Version, read on the same status query the authoring
client already polls.

**Progress is never evidence.** `READY` still requires the successful processing
attempt and trusted duration the lifecycle trigger demands. These columns only
describe how far an attempt has got.

## Invariants

- `0 ≤ percent ≤ 100`, enforced by a CHECK constraint and clamped in the writer.
- Monotonic within one attempt. A jittery or late report never moves the bar
  backwards; the writer holds the previous value and the UPDATE's own `WHERE`
  clause refuses a lower one.
- A new attempt resets the observation under a new `processing_attempt_token`, so
  a retry starts from zero rather than inheriting an abandoned percentage.
- A write from an abandoned attempt matches no row, so a worker that wakes up
  after being replaced changes nothing.
- `READY` sets 100 in the same statement that makes the version deliverable. No
  reader ever sees a deliverable asset reporting partial progress.
- `PROCESS_FAILED` retains the last measured point — it says how far the attempt
  got — while the state makes it unambiguous that nothing is still running.
- An Admin retry clears the observation outright, along with the state.

## Throttling

A real transcode emits thousands of progress blocks. An observation is persisted
only when:

- the percentage advances by at least **1 point**, **or**
- **1 second** has elapsed since the last write, **or**
- the stage changes.

The first and last observation of an attempt always write. A one-hour transcode
therefore costs tens of writes rather than thousands, and the UI still updates
about as often as a person can perceive.

## API

Progress rides the existing status route rather than a new endpoint:

```
GET /api/v1/media/assets/{assetVersionID}
```

```json
{
  "asset_version_id": "…",
  "state": "PROCESSING",
  "deliverable": false,
  "processing_stage": "TRANSCODING",
  "processing_progress_percent": 42,
  "processing_updated_at": "2026-09-06T10:31:04Z"
}
```

The three fields are additive and nullable. All three are null together until an
attempt has measured something, so a client can distinguish "no observation yet"
— an indeterminate bar — from "0% done" — a determinate one at zero.

Authorization is unchanged and is the asset's own: the owning Instructor or an
Admin. Progress is asset state, so it is behind exactly the gate the asset is
behind; another Instructor learns nothing.

## Frontend

`useProcessingWatch` (`src/components/instructor/use-processing-watch.ts`) is the
single poll loop, shared by the lesson-video and public-preview controls.

- Keyed on the **asset version identifier**, so several lessons processing at
  once each watch their own asset. There is no global percentage anywhere.
- Polls roughly every 1.5 s while the asset is genuinely processing, and stops at
  `READY` or any failure state. A settled asset is never polled again.
- Unmounting, or changing the watched asset, aborts the loop in flight; a late
  response is discarded rather than rendered.
- Bounded overall, so a stuck worker cannot leave a request loop running for the
  life of the page. Reaching the bound simply stops watching — it never reports a
  failure the server did not.

No WebSocket or SSE was introduced: the route was already polled, and one more
transport for one more field is a poor trade.

### Reload recovery

The upload completion is durable and the worker runs independently of the tab, so
a reload lands on a lesson whose video is mid-transcode with nothing in the
browser that remembers it. Recovery therefore reads the server, not local state:
the control derives its phase from the selected asset's projected media state,
and the watch re-reads the persisted observation and picks the run back up. There
is no "stuck at 0%" after a refresh, because the percentage was never in the tab.

### Accessibility

The bar is a `role="progressbar"`. When a measure exists — bytes while uploading,
the worker's observation while processing — it carries `aria-valuemin`,
`aria-valuemax`, `aria-valuenow`, and `aria-valuetext`. When none exists, the
value attributes are **omitted entirely** rather than set to zero: an
indeterminate progressbar must not claim a position it does not have. The stage
is named in text beside the number, so "42%" says what is at 42%, and all of it is
localized in English and Arabic.
