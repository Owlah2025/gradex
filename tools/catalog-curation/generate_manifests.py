"""Generate Academic Catalog manifests from the curated ibntohamy scrape.

One manifest package per institution. Subjects only: this data carries no
sourced Academic Unit, Program, or Curriculum structure, and inventing that
structure to fill the schema would be fabrication. Units, programs and
curricula stay absent until a cited source supplies them.
"""
import json, re, os, sys, unicodedata

ROOT = "../../backend/internal/academic/manifest/data"
RETRIEVED = "2026-09-14"

curation = json.load(open("curation.json", encoding="utf-8"))
trans = json.load(open("translations.json", encoding="utf-8"))
insts = json.load(open("institutions.json", encoding="utf-8"))

def slugify(text, fallback):
    s = unicodedata.normalize("NFKD", text)
    s = re.sub(r"[^A-Za-z0-9]+", "-", s).strip("-").lower()
    s = re.sub(r"-+", "-", s)
    return s or fallback

def yaml_str(value):
    """Always quote. Titles contain ':', '&', '|', '(', and Arabic."""
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'

def resolve(rec):
    en, ar = rec["title_en"], rec["title_ar"]
    if not en:
        en = trans["en"].get(ar, "")
    if not ar:
        ar = trans["ar"].get(en, "")
    if not en or not ar:
        sys.exit(f"unresolved title for {rec['sourceCourseId']}: en={en!r} ar={ar!r}")
    # Arabic recovered from the source line itself is the site's own wording,
    # which is still Gradex's own catalogue, not the university's publication.
    return en, ar

def build(inst_key, records, meta):
    used = set()
    subjects = []
    for rec in records:
        en, ar = resolve(rec)
        code = rec["official_code"]
        base = slugify(code if code else en, rec["sourceCourseId"])
        key, n = base, 2
        while key in used:
            key, n = f"{base}-{n}", n + 1
        used.add(key)
        subjects.append({"key": key, "code": code, "en": en, "ar": ar})

    manifest_id = f'{meta["slug"]}-catalog-v1'
    lines = [
        f'# {meta["name_en"]} — Gradex catalogue import.',
        "#",
        "# PROVENANCE. Every Subject below is transcribed from the Gradex-operated",
        "# course catalogue at ibntohamy.pages.dev, which is first-party Gradex data",
        "# and not a publication of the institution. No official university calendar,",
        "# registrar page, or study plan was consulted for this manifest. Codes and",
        "# English titles are therefore reported as the Gradex catalogue records them,",
        "# and may differ from the institution's current official catalogue.",
        "#",
        "# Every Arabic title is a Gradex translation, tagged gradex_translation. None",
        "# of them is claimed to be the institution's official Arabic wording.",
        "#",
        "# No academic_units, programs, or curricula are declared. The source carries",
        "# no college, department, degree-plan, or placement data, and the schema's",
        "# optionality is the correct way to represent that absence — an invented",
        "# hierarchy would be indistinguishable from a sourced one after import.",
        "",
        f"id: {manifest_id}",
        'version: "1.0.0"',
        "description: >-",
        f'  {meta["name_en"]} Subjects transcribed from the Gradex course catalogue.',
        f"  {len(subjects)} Subjects, no curriculum structure.",
        "",
        "institution:",
        f"  key: {inst_key.lower()}",
        f'  slug: {meta["slug"]}',
        "  country_code: KW",
        f'  name_ar: {yaml_str(meta["name_ar"])}',
        f'  name_en: {yaml_str(meta["name_en"])}',
        "  # Gradex's own rendering of the institution's name. No official Arabic",
        "  # publication was cited, so this may not match the institution's wording.",
        "  name_ar_source: gradex_translation",
        "  # The schema default. No cited source states this institution's level",
        "  # bounds or whether it runs a foundation stage, so neither is asserted.",
        "  max_academic_level: 4",
        "  has_foundation_stage: false",
        "  sources: [gradex-catalogue-ibntohamy, gradex-institution-naming]",
        "",
        "subjects:",
    ]
    for s in subjects:
        lines.append(f'  - key: {s["key"]}')
        if s["code"]:
            lines.append(f'    official_code: {yaml_str(s["code"])}')
        lines.append(f'    title_ar: {yaml_str(s["ar"])}')
        lines.append(f'    title_en: {yaml_str(s["en"])}')
        lines.append("    title_ar_source: gradex_translation")
        lines.append("    sources: [gradex-catalogue-ibntohamy]")
    manifest = "\n".join(lines) + "\n"

    sources = f"""# Provenance for the {meta["name_en"]} catalogue import.
#
# Read this before treating anything in manifest.yaml as an institutional fact.
# Both sources below are Gradex's own, so this manifest asserts what Gradex
# records about the institution, not what the institution publishes. Replacing
# these citations with official registrar sources is a later curation pass and
# is the only way these Subjects become officially sourced.

sources:
  - id: gradex-catalogue-ibntohamy
    url: https://ibntohamy.pages.dev/dashboard
    title: Gradex course catalogue (ibntohamy.pages.dev), Firestore collections universities and courses
    type: gradex_operated_catalogue
    retrieved_at: "{RETRIEVED}"
    supports:
      - institution: {inst_key.lower()}
      - subject_codes: as_recorded_by_gradex
      - subject_titles_en: as_recorded_by_gradex

  - id: gradex-institution-naming
    url: https://ibntohamy.pages.dev/dashboard
    title: Gradex-supplied bilingual institution and Subject naming, curated {RETRIEVED}
    type: gradex_curation
    retrieved_at: "{RETRIEVED}"
    supports:
      - institution_names: gradex_translation
      - subject_titles_ar: gradex_translation
"""
    return manifest, sources, manifest_id, len(subjects)

by_inst = {}
for rec in curation:
    by_inst.setdefault(rec["institution"], []).append(rec)

written = []
for key, records in by_inst.items():
    if key == "ku":
        continue  # Kuwait University already has an officially sourced manifest.
    meta = insts.get(key)
    if not meta:
        sys.exit(f"no institution identity recorded for {key!r}")
    manifest, sources, mid, count = build(key, records, meta)
    directory = os.path.join(ROOT, meta["slug"])
    os.makedirs(directory, exist_ok=True)
    open(os.path.join(directory, "manifest.yaml"), "w", encoding="utf-8").write(manifest)
    open(os.path.join(directory, "sources.yaml"), "w", encoding="utf-8").write(sources)
    written.append((mid, meta["slug"], count))

for mid, slug, count in sorted(written):
    print(f"{count:4} subjects  {slug:38} {mid}")
print(f"\n{len(written)} manifests, {sum(c for _, _, c in written)} subjects")
