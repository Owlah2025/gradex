# 2026-10-14 production release `57ccedd` and D-118 contact-reminder campaign

Status: **DEPLOYED AND SENT.** Approved explicitly by the Product Owner ("انشر وابعت للطلاب") after a
test copy reached the Product Owner's inbox.

| Item | Value |
|---|---|
| Released SHA | `57ccedda8a3aea8035f410a90afc5e55493b5ea3` (previous `e54b4473cdcc46fe066819d928dcf8d6ca8f899e`) |
| Kind | application-only, schema `58\|false` unchanged; `host.sh apply-release` + `verify` passed |
| Window | 10:21:41Z preflight → 10:22:04Z done; counts identical before/after (`33\|32\|31\|33\|2`) |
| Safety dump | `backups/pre-57ccedda8a3aea8035f410a90afc5e55493b5ea3-20261014T102141Z.dump` (1 245 698 bytes, `pg_restore --list` OK) |
| Review | `agy` `gemini-3.8-flash-high`, range `e54b447..57ccedd`: **VERDICT: APPROVE**, 0 findings |
| CI | [run 38521058108](https://github.com/Owlah2025/gradex/actions/runs/38521058108) success |
| Canonical E2E | 644 passed, 0 failed, 0 flaky, 3 skipped (643/643 development tests reported). The two `s13` failures seen on `29678db` were caused by a local test email written into the shared Mailpit mid-run; `s13` passed alone and in this full run |
| Test copy | one copy via `-test-to` from a throwaway container of the staged image, production sender; Resend id `01a139c6-f068-7231-a7aa-e9ec9a944b36`; no table written |

## Campaign `whatsapp-contact-2026-10`

- Dry run: 30 eligible (2 already with a number, 28 without); 3 excluded (unverified or inactive).
- Send 10:22:29Z: 30 outbox events queued, 30 audit rows `CONTACT_REMINDER_EMAIL_QUEUED`.
- Delivery: **30 / 30 `ACCEPTED`** by Resend; 0 worker errors; 0 restarts.
- Re-running the same campaign sends nothing (audit-guarded).

## Observations

One real purchase request arrived after D-117 went live; it carries a WhatsApp contact snapshot (0 requests
without contact since cutover). Two Students had added a number before the email.

## Open

- `scripts/expose-guard.sh` entry for `cmd/contact-reminder/main.go` still awaits explicit human review.
