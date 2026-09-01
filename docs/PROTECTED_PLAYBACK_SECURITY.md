# Protected Playback: what it protects, and what it does not

> Scope: protected Student Lesson video. Public previews and Course materials have their own,
> separate contracts — see [BUSINESS_RULES.md](BUSINESS_RULES.md).

## This is not DRM

Gradex has **no DRM**. There is no licence server, no key exchange, no protected media path, and no
Widevine / FairPlay / PlayReady integration. Anyone reading this document should take that as the
operating assumption rather than as a caveat.

A browser application cannot prevent a Student who is *entitled to watch* from capturing what they
are watching. Specifically, none of the following are prevented, and no part of Gradex should be
described as preventing them:

- operating-system screenshots and screen recording;
- OBS, Zoom, and every other desktop or GPU capture tool;
- browser extensions and modified browsers;
- a phone camera pointed at the screen.

Claims to the contrary must not appear in Gradex UI copy, marketing, or Instructor-facing material.
Techniques that pretend otherwise — DevTools detection, trapping `F12`, console clearing, blocking
keys page-wide — are deliberately **not** implemented: they break real Students' browsers, harm
accessibility, and stop nobody.

What Gradex does instead is make unauthorized access hard, casual copying inconvenient, and **leaks
attributable to the account that made them**.

## The layers

```
Entitlement authorization          — evaluated per request, immediately before signing
        +
Short-lived playback session       — HMAC-signed, Student-bound, expiring
        +
Protected HLS manifest             — rendered by the API, never a storage URL
        +
Expiring signed segments           — presigned private-object URLs, direct from storage
        +
Student-specific dynamic watermark — server-issued identity drawn over the picture
        +
Picture-in-Picture disabled        — so the picture cannot be shown without the watermark
        +
Basic browser download deterrence  — no native controls, no context menu, no drag
        +
Playback issuance rate limiting    — per Student and per source address, fail-closed
```

The first four layers are access control: they decide **who may watch, what exactly, and for how
long**. The last four are deterrence and attribution: they do not decide access, and they are not
security boundaries.

## The watermark

Implementation: [`backend/internal/media/delivery_watermark.go`](../backend/internal/media/delivery_watermark.go),
[`frontend/src/components/learning/video-watermark.tsx`](../frontend/src/components/learning/video-watermark.tsx).

Every value drawn is decided **server-side**, on the playback authorization, and only after identity
and entitlement have already been established. The client never supplies, selects, or influences a
watermark field.

| Shown | Why |
|---|---|
| `Ahmed E.` | Shortened display name — enough for the Student to recognise themselves and for a recipient of a leak to recognise a colleague, without publishing a paying Student's full legal name over every frame. |
| `ah***@example.com` | Correspondence address with the local part masked. The domain narrows the population; the masked local part is no longer deliverable. |
| `7K2F` | Attribution code. HMAC-SHA256 over the Account ID under the `gradex:s4:playback-watermark-code:v1` domain-separation tag, truncated to four Crockford base32 characters (no `I`, `L`, `O`, `U`, so it can be transcribed off a recording without guessing). |
| `03:42` | Coarse local wall clock, hours and minutes. Updated about once a minute. A visible deterrent only — the browser's clock is never sent to the backend and is never treated as evidence. |

**Never shown:** raw Account UUIDs, authentication or session identifiers, playback-session tokens,
storage keys or Asset Version identifiers, full email addresses, or any secret.

### How a code maps back to a Student

The code is a one-way derivation under a server-held key, so it is not reversible on the client and
knowing another Student's code grants nothing. Attribution runs the other way: given a code
recovered from a leaked recording, recompute the derivation over candidate Accounts and match. The
masked identifier and shortened name on the same frame settle any residual ambiguity from the
four-character truncation.

The code is stable per Account and independent of Lesson, session and time, which is what makes a
**single captured frame** attributable.

### Key management

The derivation reuses the existing delivery signing key under its own domain-separation tag, exactly
as the Lab Material buyer tag and the playback session already do. **No new secret, no new
environment variable, and no new deployment step.** Domain separation is what makes sharing the key
safe: each tag produces values used for nothing else, and no tag can be made to produce another's.

### Admin review

Admin review playback deliberately carries **no** watermark. `PlaybackAuthorization.Watermark` is a
pointer and is omitted from the response entirely, so an Admin reviewing a submitted Lesson is never
handed a Student identity to impersonate.

## Why Picture-in-Picture is disabled

The watermark is a DOM layer over the media surface. Browser Picture-in-Picture presents the bare
`<video>` element in a window the page does not draw into, so the picture would play on **without**
the watermark — which is exactly the surface a leak would be recorded from.

The control, the `I` keyboard shortcut, and the shared behaviour helpers are all removed, and the
element carries `disablePictureInPicture` so the browser's own affordance on the video is gone too.

**Fullscreen is unaffected.** It presents the whole player container, watermark included, which is
why the watermark is mounted inside that container rather than beside it.

## Student experience is a constraint, not a trade-off

Gradex is a teaching product. The watermark is sized so a Student forgets it is there:

- roughly 10–12px, opacity ~0.16, occupying a tiny fraction of the picture;
- six edge zones — never the centre of the lecture, never the control bar;
- moves once every 35–55 seconds, as a brief fade rather than continuous motion;
- `pointer-events: none`, `user-select: none`, `aria-hidden` — it cannot take a click, be selected,
  be focused, or be read out by a screen reader;
- no layout shift, no rebuffering, no HLS reinitialisation, and no network traffic when it moves;
- works in both RTL and LTR layouts, using logical `start`/`end` insets.

Because six consecutive moves visit all six zones, a recording long enough to matter carries the
identity in every corner and both middles — cropping one region removes only one sixth of the
occurrences.

## Deliberately not implemented

| Not done | Why |
|---|---|
| DRM (Widevine / FairPlay / PlayReady) | Out of scope for this tranche. See the migration path below. |
| Screenshot / screen-recording blocking | Impossible in a browser. Any implementation would be theatre. |
| DevTools detection, `F12` traps, console clearing | Theatre. Breaks real browsers, blocks nobody. |
| Page-wide right-click or keyboard blocking | Harms accessibility and normal use. Context-menu suppression is scoped to the media surface only. |
| Device fingerprinting | Invasive, and explicitly out of scope. |
| IP address as Student identity | Mobile networks change constantly; it would lock out legitimate Students. |
| Forced logout on concurrent use | Would punish ordinary multi-device study. |
| Concurrent-playback limits | See below. |
| Proxying video bytes through the API | Would discard the presigned-URL architecture for no security gain. |
| `no-store` on immutable HLS segments | Segments are already capability-gated by expiring signatures; `no-store` would only harm CDN and storage behaviour. |

## Open follow-up: concurrent playback

Not implemented in this tranche, deliberately.

Gradex has an authentication `sessions` table, but **no server-side registry of active *playback*
sessions** — playback sessions are stateless signed tokens that are never persisted. Enforcing a
"maximum N simultaneous streams per account" policy would therefore require new stateful
infrastructure: an active-stream store, a heartbeat or renewal protocol from the player, and
eviction semantics. Building that here would risk exactly the failures the policy must avoid —
cutting off a Student whose mobile network changed, or whose previous tab was closed without a
clean release.

It should be designed on its own, with graceful replacement rather than refusal, and it must never
be built on device fingerprinting or IP identity.

## Future DRM migration path (not implemented)

Should account sharing or redistribution become a measured commercial problem rather than a
theoretical one:

1. **Measure first.** Use watermark attribution from real leaks to establish that redistribution is
   actually happening and at what scale. DRM is a large, recurring cost; it should answer an
   observed problem.
2. **Add concurrent-stream limits before DRM.** Cheaper, less invasive, and addresses account
   sharing — which is the more likely commercial leak — where DRM does not.
3. **Encrypt at packaging time.** Move from plain HLS to CMAF/fMP4 with common encryption (CENC),
   producing one packaged asset consumable by all three DRM systems. This is the largest piece of
   work and is where the existing transcoding pipeline changes.
4. **Introduce a licence service**, keyed to the *same* entitlement evaluator that already governs
   playback authorization, so there is one access decision point and not two.
5. **Widevine (Chrome/Android/Edge), PlayReady (Windows/Xbox), FairPlay (Safari/iOS)** via the
   browser's Encrypted Media Extensions. hls.js already supports EME, so the player changes less
   than the pipeline does.
6. **Keep the watermark.** DRM stops the file from being copied; it does not stop a camera pointed
   at a screen. Watermarking remains the only attribution mechanism, and the two are complementary
   rather than alternatives.

Expect DRM to add per-licence vendor cost, a hard dependency on a third-party licence service,
noticeably worse playback compatibility on older devices, and a new class of "the video will not
play" support burden. None of that is worth paying before step 1 says it is.
