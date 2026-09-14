"""Normalize scraped course rows into Subject curation records.

Emits curation.json: one record per (institution, subject) with the English and
Arabic titles that could be recovered from the source, plus an explicit list of
what a human still has to author. Nothing is invented here.
"""
import json, re, collections, unicodedata

AR = re.compile(r"[؀-ۿݐ-ݿﭐ-﷿ﹰ-﻿]")

# Source typos observed on ibntohamy.pages.dev. Each is a transcription fix,
# never a retitling: the pipe forms are Roman numerals typed as pipes.
TITLE_FIXES = {
    "Calculus |": "Calculus I",
    "Calculus ||": "Calculus II",
    "Calculus |||": "Calculus III",
    "Calculus 3": "Calculus III",
    "Database Managemet System": "Database Management System",
    "Engineering_Statistics": "Engineering Statistics",
}

def clean(s):
    s = unicodedata.normalize("NFKC", s or "")
    s = s.replace("‏", "").replace("‎", "")
    s = re.sub(r"\s+", " ", s).strip()
    return s

def split_bilingual(title):
    """Return (english, arabic). Either may be empty."""
    t = clean(title)
    if not AR.search(t):
        return t, ""
    if not re.search(r"[A-Za-z]", t):
        return "", t
    # Mixed. Find the contiguous Arabic run (plus Arabic-adjacent punctuation)
    # and treat the remainder as the English title.
    spans = [m.span() for m in re.finditer(r"[؀-ۿݐ-ݿ\s()0-9،؛؟]+", t)
             if AR.search(t[m.start():m.end()])]
    if not spans:
        return t, ""
    start = min(s for s, _ in spans)
    end = max(e for _, e in spans)
    arabic = clean(t[start:end])
    english = clean(t[:start] + " " + t[end:])
    return english, arabic


def rebalance_ordinal(en, ar):
    """Keep a trailing "(N)" ordinal on both titles, not just the one the
    Arabic run happened to absorb."""
    if not en or not ar:
        return en, ar
    m = re.search(r"\((\d+)\)", ar)
    if not m or re.search(r"\((\d+)\)|\b(I{1,3}|IV|V)\b|\d", en):
        return en, ar
    ordinal = m.group(1)
    ar = clean(re.sub(r"\s*\(\d+\)\s*", " ", ar)) + f" ({ordinal})"
    return f"{en} ({ordinal})", ar

cat = json.load(open("catalog.json"))
records, notes = [], []

for u in cat:
    if u["sourceId"] == "__unaffiliated__":
        continue
    seen_code = {}
    rows = []
    for c in u["courses"]:
        raw = clean(c["title"])
        raw = TITLE_FIXES.get(raw, raw)
        en, ar = split_bilingual(raw)
        en, ar = rebalance_ordinal(en, ar)
        code = clean(c["code"]).replace("_", " ")
        code = re.sub(r"\s+", " ", code).strip()
        rows.append({"src": c, "code": code, "en": en, "ar": ar, "raw": raw})

    # Code collisions inside one institution would violate
    # subjects_institution_code_unique. Two passes: first recover codes the
    # source itself contradicts (the document id embeds the real code), then
    # demote whatever still collides. Order matters — a row whose code was
    # only colliding because of another row's typo keeps its code.
    def norm(code):
        return code.upper().replace(" ", "").replace(".", "").replace("-", "")

    def groups(rs):
        g = collections.defaultdict(list)
        for r in rs:
            if r["code"]:
                g[norm(r["code"])].append(r)
        return {k: v for k, v in g.items() if len(v) > 1}

    for key, group in list(groups(rows).items()):
        for r in group:
            m = re.search(r"_([A-Za-z]{2,5})[_ ]?(\d{3})$", r["src"]["id"])
            if m and norm(m.group(1) + m.group(2)) != key:
                fixed = f"{m.group(1).upper()} {m.group(2)}"
                notes.append(f'{u["sourceId"]}: "{r["en"] or r["raw"]}" code {r["code"]} -> {fixed} (recovered from document id {r["src"]["id"]})')
                r["code"] = fixed

    for key, group in groups(rows).items():
        for r in group:
            notes.append(f'{u["sourceId"]}: "{r["en"] or r["raw"]}" dropped colliding code {r["code"]} -> code-less (unresolvable from source)')
            r["code"] = ""

    for r in rows:
        records.append({
            "institution": u["sourceId"],
            "institutionName": u["name"],
            "sourceCourseId": r["src"]["id"],
            "official_code": r["code"],
            "title_en": r["en"],
            "title_ar": r["ar"],
            "needs": [k for k in ("title_en", "title_ar") if not r[k.split("_")[1] == "en" and "en" or "ar"]],
            "year": r["src"]["year"],
            "rawTitle": r["raw"],
        })

for rec in records:
    rec["needs"] = [f for f in ("title_en", "title_ar") if not rec[f]]

json.dump(records, open("curation.json", "w"), ensure_ascii=False, indent=2)

need_ar = [r for r in records if "title_ar" in r["needs"]]
need_en = [r for r in records if "title_en" in r["needs"]]
print(f"subject records: {len(records)}")
print(f"already bilingual from source: {len(records)-len(need_ar)-len(need_en)}")
print(f"need Arabic authored: {len(need_ar)}  (distinct: {len({r['title_en'] for r in need_ar})})")
print(f"need English authored: {len(need_en)}  (distinct: {len({r['title_ar'] for r in need_en})})")
print(f"code-less subjects: {sum(1 for r in records if not r['official_code'])}")
print("\n-- collision / typo notes --")
for n in notes:
    print(" ", n)
open("distinct_need_ar.txt","w",encoding="utf-8").write("\n".join(sorted({r["title_en"] for r in need_ar})))
open("distinct_need_en.txt","w",encoding="utf-8").write("\n".join(sorted({r["title_ar"] for r in need_en})))
