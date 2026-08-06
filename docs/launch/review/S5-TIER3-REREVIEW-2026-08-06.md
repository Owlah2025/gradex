# S5 Independent Tier 3 Rereview — 2026-08-06

Retained record of the independent Tier 3 rereview that made T078 eligible for closure. It is the
evidence T078 rests on, so it is checked in rather than left in a transcript.

## Independence

The reviewer was **independent of the builder**. Claude authored the S5 slice under
[D-036](../../DECISIONS.md#d-036--claude-builds-s1b3-and-agy-reviews) and did **not** review it — a builder
reviewing its own range is a self-check, not a review, and cannot close a slice. The reviewer did not
modify the repository and did not close T078; closure is recorded separately by the builder in a
documentation-only commit, which is what this record accompanies.

**Provenance of this record.** The verdict and findings below were transmitted to the builder by the
product owner. The builder did not retrieve the reviewer's output directly and does not claim to have
observed the review being performed. What is verifiable in this repository — the frozen SHA, the clean
tree and index, the 23 classified commits, the absence of a merge commit, and the hosted run conclusions —
was independently re-checked by the builder before closure and is recorded in the T077 and T078 entries in
[`tasks.md`](../../../specs/007-protected-learning/tasks.md).

## Reviewed target

```text
Branch:            s5-attempt-2-20260801
Frozen candidate:  41373a865bf4dc310f9b9b20139daecbb65767e0
Reviewed range:    9c8348a1..41373a865bf4dc310f9b9b20139daecbb65767e0
Commits in range:  23, all classified; no merge commit
Working tree:      clean          Index: clean
Baseline SHA-256:  e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  (0 bytes)
```

## Hosted CI on the reviewed candidate

Run [31100802602](https://github.com/Owlah2025/gradex/actions/runs/31100802602), commit
`41373a865bf4dc310f9b9b20139daecbb65767e0`:

| Job | Conclusion |
| --- | --- |
| Backend | success |
| Frontend | success |
| Migrations | success |
| Admission Integration | success |
| Guards | success |
| S5 T075 Rendered Evidence | success |

Retained artifact:

```text
Name:        gradex-s5-t075-rendered-41373a865bf4dc310f9b9b20139daecbb65767e0
Artifact ID: 8967489099
Size:        1,935,837 bytes
Expiration:  2026-11-04
FAILED in name: no
```

## Prior findings — resolution confirmed

The reviewer confirmed the three findings from the previous review of superseded head `f5985c7`:

| Finding | Severity | State |
| --- | --- | --- |
| H-1 — playback issuance implemented no rate limit | High | **Resolved** |
| M-1 — hosted Admission Integration omitted `./internal/learning` | Medium | **Resolved** |
| M-2 — T075 closure cited an unretrievable artifact | Medium | **Resolved transparently** |

**No unresolved Critical finding. No unresolved High finding.** All remaining findings are Medium or Low
and non-blocking.

## Retained non-blocking follow-ups

Severities are the reviewer's and are not restated or downgraded here.

| ID | Severity | Finding |
| --- | --- | --- |
| F-1 | Medium | Playback rate limiting lacks an attributable per-Student/per-source monitoring signal. |
| F-2 | Medium | `docs/launch/STATUS.md` materially understates S5 delivery state. |
| F-3 | Low | The exact 30th/31st quota boundary is proven compositionally rather than by one end-to-end boundary test. |
| F-4 | Low | Protected-learning policy maps are assembled in three places. |
| F-5 | Low | Student-denied requests still consume the source bucket, because source limiting runs first. |
| F-6 | Low | The builder continued after a red hosted run, though the diagnosed recovery was narrow and independently accepted. |
| F-7 | Low | Several other integration-tagged packages remain outside hosted CI. |

Previously disclosed and still open:

- stale report-throttle comments;
- `completed_at` bypassing the injected clock;
- defense-in-depth CSRF asymmetry on learning mutations.

**Only `F-2` is resolved in the closure pass**, because it is a truthfulness defect in the status record
itself. Every other item above remains open and tracked. S5 does not reopen because they remain, and
closure does not assert they are fixed.

## Verdict

The reviewer's verdict, preserved verbatim:

```text
VERDICT: APPROVE
```

Eligibility statement, preserved verbatim:

```text
T078 is eligible for closure on frozen HEAD 41373a865bf4dc310f9b9b20139daecbb65767e0.
```

This is the literal verdict line, not a paraphrase and not an inference from favourable prose. An
implied verdict is `UNAVAILABLE`, not approval.

## Reviewed candidate versus closure record

```text
Reviewed candidate:      41373a865bf4dc310f9b9b20139daecbb65767e0
Independent verdict:     VERDICT: APPROVE
Closure-record commit:   recorded in tasks.md T078 and docs/launch/daily/2026-08-06.md
```

The commit that records this verdict is **not** part of the reviewed range and was **not** reviewed. A
verdict can only be recorded after it exists, so the recording commit necessarily follows the range it
cites. That commit is documentation and evidence only, is confined to the four paths authorized by
[D-072](../../DECISIONS.md#d-072--t078-closes-on-hosted-ci-plus-an-independent-tier-3-approve-and-the-closure-commit-is-not-the-reviewed-candidate),
and changes no production behaviour. A closure commit reaching outside that boundary would change what
was approved and would require a fresh review.
