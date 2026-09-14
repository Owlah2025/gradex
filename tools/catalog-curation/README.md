# Academic Catalog curation — Kuwaiti institution set (D-106)

How the fourteen scraped Institution manifests under
`backend/internal/academic/manifest/data/` were produced, and how to reproduce them.

## Provenance

Source is the Gradex-operated course catalogue at `ibntohamy.pages.dev`, read from its public
Firestore collections `universities` and `courses` on 2026-09-14. That is first-party Gradex data,
**not** a publication of any institution. Nothing here is officially sourced, which is why every
generated manifest declares Subjects only and tags every Arabic title `gradex_translation`.
See [D-106](../../docs/DECISIONS.md) §2.

## Files

| File | What it is |
|---|---|
| `catalog.json` | The scrape, joined university → courses. The raw input. |
| `normalize.py` | Cleans titles, splits bilingual source strings, resolves code collisions. Writes `curation.json`. |
| `translations.json` | Hand-authored bilingual titles. 196 Arabic, 16 English. Every entry is a Gradex translation. |
| `institutions.json` | Hand-authored institution slug and bilingual name per university. |
| `generate_manifests.py` | Emits `manifest.yaml` + `sources.yaml` per institution. |
| `ku_resolution.py` | Re-derives the D-106 §3 table proving all 19 scraped Kuwait University rows duplicate Subjects Kuwait University already has. Exits non-zero if any row fails to resolve, because the exclusion depends on that being total. |

## Reproduce

```
cd tools/catalog-curation
python3 normalize.py          # -> curation.json, distinct_need_{ar,en}.txt
python3 generate_manifests.py # -> backend/internal/academic/manifest/data/<slug>/
cd ../../backend && go test ./internal/academic/...
```

`normalize.py` exits non-zero if a title has no translation, so a new scrape cannot silently ship a
half-translated Subject.

## Curation decisions encoded here

- **Code collisions.** Where one institution gave two Subjects the same code, the code is recovered
  from the source document id when that id disagrees with it (`iuk` "Fund. of Digital Logic + Lab"
  was filed under `MATH 110`; its id says `CMPE 341`). Where the source cannot resolve it, both rows
  become code-less and are identified by title. Nothing is guessed.
- **Underscores are a transcription artifact.** AUK, AUM, ACM and Ktech print `CSIS 130`, never
  `CSIS_130`.
- **Ordinals belong to both titles.** The bilingual split otherwise assigned `(1)` to whichever side
  absorbed it, which produced two PAAET Subjects both titled "Visual Programming".
- **Kuwait University is excluded.** Its scraped codes are the letter form of the official numeric
  codes already imported. The letter prefix and the numeric prefix name the same department
  (`CLS`→`1800`, `ISC`→`1830`, `DSAI`→`1832`, `MATH`→`0410`, `STAT`→`0480`) and the three-digit
  course number is identical, so `DSAI 348` is `1832348`. All 19 scraped Kuwait University rows
  resolve to existing official Subjects; none is outstanding. See D-106 §3 for the full table.

## Known gaps for a later officially sourced pass

- PAAET codes are bare numbers (`101`, `254`) because that is what the source records.
- `max_academic_level: 4` and `has_foundation_stage: false` are schema defaults, not sourced claims.
  Several Kuwaiti private universities do run foundation programmes.
- Institution Arabic names are Gradex renderings, not verified official wording.
