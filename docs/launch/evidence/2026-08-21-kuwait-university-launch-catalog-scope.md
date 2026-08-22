# Kuwait University launch catalog — academic scope and curation record

**Recorded:** 2026-08-21 · **Revised:** 2026-08-22 (T2.1 scope alignment; **T2.2 Data Science & AI launch program**)
**Research date for every claim below:** 2026-08-21
**Authority:** [D-091](../../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy) §11 — Kuwait University is the only launch-data institution.
**Manifest:** `backend/internal/academic/manifest/data/kuwait-university/manifest.yaml` (`kuwait-university-launch-v1` v1.2.0)
**Provenance:** `backend/internal/academic/manifest/data/kuwait-university/sources.yaml`

This is the curation record, separate from the automated-test record in
[the MVP-F18 evidence](2026-08-21-mvp-f18-launch-catalog-data.md). It states what
was included, what was deliberately left out, and — most importantly — where the
data is silent.

> ## Revision 2 — correction of a factual error in revision 1
>
> The Founder stated that the launch teaching team covers **Mathematics,
> Cybersecurity, Data Science, and Software**. Re-researching Kuwait University
> against those four areas found that **revision 1 of this record was wrong**:
>
> 1. **Kuwait University does confer a B.Sc. in Cybersecurity.** It was initiated
>    in Summer 2024 in the Department of Computer Science, College of Science.
>    Revision 1 asserted that "Kuwait University does not expose degree Programs
>    under those exact names", and a test encoded that error. Both are corrected;
>    Cybersecurity is now a launch Program with its full 127-credit plan.
> 2. **Kuwait University does confer a B.Sc. in Data Science and Artificial
>    Intelligence (DSAI)**, in the Department of Information Science, College of
>    Life Sciences, launched for 2025/2026. It is *not* imported — see §8 — but it
>    is real and revision 1 implied otherwise.
> 3. **Kuwait University publishes an official Suggested Study Plan** for the
>    Computer Science 2024 major that places named courses in a year and a term.
>    Revision 1 concluded no placement source existed. Sixteen mappings now carry
>    authoritative level and semester.
>
> The cause was a scope error, not a sourcing error: revision 1 researched the
> programs the *persona* implied and never searched Kuwait University for the
> teaching areas by name.

---

## 1. How launch scope was determined

Scope was derived from repository authority, not from what happened to be easy
to scrape.

| Input inspected | What it established |
|---|---|
| [`docs/PROJECT_VISION.md`](../../PROJECT_VISION.md) §Primary User | The canonical persona is **Fahd, 19, Kuwait, Major: Computer Science**, whose goal is "strong results in **first-year Courses**". Computer Science and the shared first-year foundation are therefore the centre of launch scope. |
| Design report §3.1, §15 | Primary-source proof that `0410-101 Calculus I` is required verbatim by multiple engineering programs — the shared-Subject case the whole redesign exists to model. |
| D-091 §11 | Kuwait University only. |
| Live development database | **Zero** `taxonomy_terms` rows and zero seeded taxonomy anywhere in the repository. There was no existing classification to inherit or reconcile. |
| Course fixtures | Existing E2E fixtures use invented placeholder taxonomy (`Software Engineering` / `SWE101`), which is test scaffolding and deliberately contributed nothing to launch scope. |

**Resulting principle (revision 2):** cover the Programs that are the real
academic home of the Founder's four teaching areas, plus every parent Academic
Unit those Programs need. The persona remains a valid input but is no longer the
only one.

**Repository scope conflict.** [`PROJECT_VISION.md`](../../PROJECT_VISION.md) is a
persona document, not a launch-inventory register, and it was never superseded —
but it is **narrower than the Founder's stated launch scope**. The repository
records planned inventory only as "8–12 launch Courses"
([`PLAN.md`](../PLAN.md), [`specs/003-course-authoring/plan.md`](../../../specs/003-course-authoring/plan.md))
and names no teaching areas anywhere. The four teaching areas are therefore new
Founder information with no prior repository record, and the persona must not be
treated as the authority on launch scope.

---

## 2. Included

### Institution

**Kuwait University** — `kuwait-university`, country `KW`, `max_academic_level: 5`,
`has_foundation_stage: false`.

Five levels, not four, because the official Student Manual defines
`الفرقة الدراسية` by credits earned across five bands (≤29, 30–59, 60–89, 90–136,
≥137). This is manifest data; no application logic hardcodes it.

### Academic Units (9)

| Unit | Kind | Parent | Why included |
|---|---|---|---|
| College of Science | COLLEGE | — | Owns the persona's Program and the Mathematics/Physics/Chemistry/Statistics departments whose Subjects every launch plan requires |
| College of Engineering and Petroleum | COLLEGE | — | Owns both engineering launch Programs |
| Computer Science | DEPARTMENT | College of Science | Owns the B.Sc. Computer Science Program and the `0418` Subjects |
| Mathematics | DEPARTMENT | College of Science | Owns the `0410` Subjects shared by all three launch Programs |
| Physics | DEPARTMENT | College of Science | Owns the `0430` Subjects in both engineering plans |
| Chemistry | DEPARTMENT | College of Science | Owns the `0420` Subjects in both engineering plans |
| Statistics and Operations Research | DEPARTMENT | College of Science | Owns `0480-201`, a College of Science requirement in the CS 2024 plan |
| Computer Engineering | DEPARTMENT | College of Engineering and Petroleum | Owns the B.Sc. Computer Engineering Program |
| Electrical Engineering | DEPARTMENT | College of Engineering and Petroleum | Owns the B.Sc. Electrical Engineering Program |

Every unit exists because a launch Program or a launch Subject needs it. No unit
was added for completeness.

### Programs (4)

| Program | Owning department | Why it belongs in launch scope | Founder teaching area served |
|---|---|---|---|
| **Computer Science** (B.Sc.) | Computer Science | The persona's major, and the academic home of the Software teaching area. Kuwait University publishes a labelled **Major 2024** sheet *and* an official Suggested Study Plan for it. | Software, Mathematics, Data Science |
| **Cybersecurity** (B.Sc.) | Computer Science | **Added in revision 2.** A real Kuwait University degree initiated Summer 2024, with a fully itemized 127-credit major sheet. It is the actual academic home of the Cybersecurity teaching area. | Cybersecurity, Software, Mathematics |
| **Computer Engineering** (B.Sc.) | Computer Engineering | Shares the Mathematics/Physics/Chemistry block, so Gradex's Mathematics offering reaches its Students. | Mathematics |
| **Electrical Engineering** (B.Sc.) | Electrical Engineering | Same shared block; see §9 for the explicit launch-relevance judgment. | Mathematics |

**Department is not Program — proved twice over.** The Computer Science
department owns **two** Programs (Computer Science and Cybersecurity), and the
Mathematics department owns two more that are deliberately not imported.

**Department is not Program.** Each Program is a separate row owned by a
department; the Computer Science department owns a Program named Computer
Science, and that identity is not assumed anywhere.

### Curricula (4)

Every curriculum now declares `version_label_source`, so a Gradex placeholder can
never reach a Student looking like the university's own version name. Validation
refuses a placeholder that does not explain itself.

| Curriculum | Program | Version label | Provenance |
|---|---|---|---|
| `ku-cs-2024` | Computer Science | **`2024`** | `official` — Kuwait University's own label |
| `ku-cybersecurity-current` | Cybersecurity | `current` | `gradex_translation` placeholder; effective year 2024 recorded from the programme's stated Summer 2024 start |
| `ku-cpe-current` | Computer Engineering | `current` | `gradex_translation` placeholder |
| `ku-ee-current` | Electrical Engineering | `current` | `gradex_translation` placeholder |

Kuwait University demonstrably *does* label plan versions — the Computer Science
department publishes both a "2024" sheet and an "Undergraduate Major Sheets
(2018/2019 until 2023/2024)" archive. The three placeholders are therefore a gap
in what those specific pages publish, not evidence that the university has no
labelling convention.

### Subjects (44, all with official codes)

| Code family | Owning department | Codes |
|---|---|---|
| Mathematics `0410` | Mathematics | `0410-101`, `0410-102`, `0410-111`, `0410-211`, `0410-240` |
| Chemistry `0420` | Chemistry | `0420-101`, `0420-105` |
| Physics `0430` | Physics | `0430-101`, `0430-105`, `0430-102`, `0430-107` |
| Statistics `0480` | Statistics and OR | `0480-201` |
| Computer Science core `0418` | Computer Science | `0418-101`, `0418-111`, `0418-143`, `0418-201`, `0418-220`, `0418-221`, `0418-310`, `0418-320`, `0418-491`, `0418-492` |
| **Software area `0418`** *(rev 2)* | Computer Science | `0418-321` Operating Systems, `0418-331` Computer Networks (both compulsory on the Cybersecurity sheet); `0418-301` Algorithm Design and Analysis, `0418-335` Web Programming, `0418-390` Software Engineering, `0418-470` Database Systems (CS elective pool — see §9) |
| **Security area `0418`** *(rev 2)* | Computer Science | `0418-231`, `0418-312`, `0418-348`, `0418-435`, `0418-493` — all compulsory on the Cybersecurity major sheet |
| **Cybersecurity interdisciplinary `1830`** *(rev 2)* | *(none claimed)* | `1830-351`, `1830-434`, `1830-435`, `1830-436` |
| **Data-science area `0418`** *(rev 2)* | Computer Science | `0418-365` Artificial Intelligence, `0418-466` Introduction to Machine Learning |
| University requirements | *(no owning unit claimed)* | `0310-101`, `0330-100`, `0330-102`, `9988-161`, `9988-162` |

### CurriculumSubject mappings (69)

- Computer Science 2024 — 19 (5 university, 4 College of Science, 10 CS core); **16 carry authoritative level and semester**
- **Cybersecurity current — 28** (5 university, 4 College of Science, 10 CS compulsory, 5 security compulsory, 4 interdisciplinary)
- Electrical Engineering current — 11 (the itemized Mathematics and Basic Science block)
- Computer Engineering current — 11 (the same shared block, named by title on its page)

`0410-101 Calculus I` is now **one Subject mapped into four curricula**.

---

## 3. Deliberately excluded

| Excluded | Why |
|---|---|
| **Every other Kuwaiti institution** (AASU, AUK, GUST, AUM) | D-091 §11. They were researched to prove the schema generalizes, and the manifest registry ships exactly one manifest. A test asserts that. |
| **13 of Kuwait University's 16 colleges** | No launch Program sits in them. Adding them would be warehouse-building, not launch data. |
| **The other four College of Engineering departments** (Mechanical, Civil, Chemical, Petroleum, Industrial & Management Systems) | Real departments, but no launch Program. They join when Gradex has inventory. |
| **The CS `2018/2023` major sheet** | Kuwait University publishes it alongside the 2024 sheet. It serves no launch purpose and no migration purpose yet; importing a superseded plan would add ambiguity without a consumer. Curriculum versioning is already modelled, so it can be added later without schema change. |
| **~30 further Computer Science courses** on the department catalog page | Real Subjects, but outside the Major 2024 compulsory blocks and outside a first-year persona's launch scope. |
| **CS elective, science elective, and university elective *groups*** | The sheets describe these as choose-from-group requirements rather than enumerated courses. Modelling a group as a Subject would be false; modelling group semantics is degree-audit territory that D-091 explicitly excludes. |
| **Prerequisites, minors (`التخصص المساند`), tracks (`مسار`)** | Out of scope by D-091's boundary clause. |
| **"Software", "Software Engineering", "Data Science", "Programming", "Cybersecurity Engineering" as Programs** | Kuwait University confers no degree under any of these names. Software is a Subject area inside Computer Science and Cybersecurity; Data Science's real home is the DSAI degree (§8). The College of Engineering and Petroleum's own About page enumerates exactly **seven** B.Sc. degrees and no Cybersecurity Engineering, so the `eng.ku.edu.kw/.../cybersecprog` URL seen in search results is not treated as evidence of a conferred degree. A test asserts none of these appears as a Program. **Cybersecurity is deliberately not on this list — it is a real Kuwait University degree and is now imported.** |
| **B.Sc. Data Science and Artificial Intelligence (DSAI)** | **Real and confirmed**, College of Life Sciences / Department of Information Science, 121 credits, 4 years. Not imported because its major sheet and 8-semester plan are published only as `.docx` and its Subject codes could not be transcribed in this pass. A selectable Program with an empty Subject catalog would serve a Student worse than its absence. **This is the single highest-value follow-up.** |
| **B.Sc. Mathematics and B.Sc. Financial Mathematics** | Both real, both owned by the Mathematics Department. Not imported: Gradex's Mathematics teaching area serves Calculus/Linear Algebra to Computer Science, Cybersecurity, and engineering Students, and those Subjects are already mapped into those Programs' plans. Subject ownership is not Student audience. Add only if the Founder confirms Gradex targets Mathematics-major Students. |

---

## 4. Fields deliberately left NULL

This section exists so uncertainty is visible in the record rather than hidden
in the data.

| Field | Rows affected | Why |
|---|---|---|
| **`recommended_level`** | populated on **16 of 69**; NULL on 53 | **Populated** for the Computer Science 2024 major, from Kuwait University's own **Suggested Study Plan**, which places named courses in FRESHMAN/SOPHOMORE/JUNIOR/SENIOR (السنة الأولى—الرابعة). **NULL** everywhere else, because no cited source places those courses. Inferring "course number 300 → level 3" remains forbidden and is not done. |
| **`recommended_semester`** | populated on **16 of 69**; NULL on 53 | Same plan, same rows: Fall = 1, Spring = 2. |
| **`credits`** | populated on **58 of 69**; NULL on 11 | The 11 NULLs are the entire Computer Engineering block: its page publishes the block **total** (27 credits) without per-course values. Copying EE's per-course credits onto CpE would be assumption presented as fact. |
| **`owning_unit_id`** | NULL on 9 Subjects | The five university requirements (`0310-101`, `0330-100`, `0330-102`, `9988-161`, `9988-162`) and the four `1830-` interdisciplinary Subjects. Their sheets give codes and titles but name no owning department; inventing one would be fabricated structure. |

**Three CS 2024 mappings stay unplaced on purpose.** The Suggested Study Plan
holds slots labelled `UNIV. COMP. 1/2/3` without saying which university
requirement fills which slot, so `0310-101`, `0330-100`, and `0330-102` carry no
placement. The plan's `SCIENCE 1–4`, `CS ELECTIVE`, `UNIV. ELEC.`, and
`FREE ELEC.` slots are category placeholders and map to no Subject at all.

Validation enforces this rather than trusting it: a mapping that asserts a level
or a semester **must cite its own source**, and a manifest test fails if any
placement is populated at all.

---

## 5. Uncertainties and curation judgments

Recorded honestly so a reviewer can challenge them.

1. **`current` as a curriculum version label (CpE, EE).** Kuwait University
   publishes no plan version label on either engineering program page, while the
   CS department does publish `2024`. Rather than invent a year, the manifest
   uses `current` and carries a `version_label_note` on each row saying exactly
   that and instructing replacement once an official label is observed.

2. **Computer Engineering's Subject codes were resolved, not transcribed.** The
   CpE page names its Mathematics and Basic Science courses by title (Calculus
   I–III, Linear Algebra, Differential Equations, General Physics I/II with
   laboratories, General Chemistry I with laboratory) and cites the
   university-wide code scheme including `0410-101` itself. The codes were
   therefore resolved through the same university-wide scheme the EE page
   itemizes. This is a curation judgment, not a transcription, and it is the
   reason CpE credits are left NULL.

3. **Requirement-kind mapping for the engineering block.** The engineering pages
   label it "Mathematics and Basic Science", a category distinct from "College
   of Engineering Requirements". It is recorded as `COLLEGE_REQUIREMENT` because
   it is required of the College's programs and sits outside the major
   department. Kuwait University's own `التخصص المساند` (supporting
   specialisation) means something narrower — 24 credits outside the major field
   — so `SUPPORTING` would have been the less accurate choice.

4. **Two Kuwait University sources disagree about parts of the CS curriculum.**
   The department course catalog lists `0418-141`/`0418-142 Computer Programming
   I/II`, `0418-211 Theory of Computation I`, and `0418-221 Computer Systems`;
   the Major 2024 sheet lists `0418-143 Fund. of Computer Programming (4)`,
   `0418-310 Theory of Computation`, and `0418-320 Principles of Computer
   Systems`. The catalog page also gives `0418-143` as a 3-credit elective while
   the 2024 sheet makes it a 4-credit core course. **Both are recorded here; the
   Major 2024 sheet was preferred** for the 2024 curriculum because it is the
   authoritative plan for that curriculum version. The catalog page most likely
   still reflects the older `2018/2023` plan. This should be re-checked before
   the CS plan is relied on for Student-facing discovery in T6.

5. **Code display format varies by page.** The Major 2024 sheet prints `0410101`;
   the course catalog and both engineering pages print `0418-111` / `0410-101`.
   The dashed form is stored as the official display code because it is the form
   a Student sees on a plan and on program pages. Normalization to `0410101`
   exists only for identity and dedupe.

---

## 6. Gradex-supplied translations

Kuwait University publishes official Arabic for the institution, its colleges,
and its departments in the Student Manual, so those are marked `official`.

**Every one of the 27 Subject titles carries `title_ar_source: gradex_translation`.**
The cited pages publish these course titles in English; the Arabic
course-description pages were not used as a title source. Gradex therefore
supplies the Arabic, and the manifest says so rather than presenting Gradex
wording as the university's official Arabic. Validation requires the declaration,
a test asserts every Subject declares `gradex_translation`, and the importer
records the provenance in each Subject's audit metadata.

If official Arabic course titles are later transcribed from Kuwait University's
Arabic course-description pages, those entries should flip to `official`.

---

## 7. Sources used

All primary. No competitor, aggregator, or encyclopaedia source contributed any
academic fact; a test fails the build if one appears.

| id | Source | Type |
|---|---|---|
| `ku-colleges-index` | [Colleges of Kuwait University](https://www.ku.edu.kw/academic/colleges) | Official university page |
| `ku-student-manual` | [دليل الطالب — عمادة القبول والتسجيل](https://portal.ku.edu.kw/manuals/admission/en/student_manual.pdf) | Official university manual |
| `ku-coep-home` | [College of Engineering and Petroleum](https://eng.ku.edu.kw/) | Official college page |
| `ku-science-departments` | [College of Science](https://sci.ku.edu.kw/) | Official college page |
| `ku-cpe-undergraduate` | [Undergraduate Program — Computer Engineering](https://engineering.ku.edu.kw/cpe/undergraduates/undergraduate-program) | Official program page |
| `ku-ee-undergraduate` | [Undergraduate Program — Electrical Engineering](https://engineering.ku.edu.kw/ee/undergraduates/undergraduate-program) | Official program page |
| `ku-cs-department` | [Undergraduate — Computer Science Department](https://www.cs.ku.edu.kw/undergraduate/) | Official department page |
| `ku-cs-major-2024` | [Computer Science Major 2024](https://www.cs.ku.edu.kw/computer-science-major-2024-kuwait-university/) | Official major sheet |
| `ku-cs-courses-en` | [Courses — Computer Science Department](https://www.cs.ku.edu.kw/undergraduate/courses-en/) | Official course catalog |

Baims contributed nothing to this manifest. It informed discovery vocabulary
during the T1 design research only, which D-091 and the T2 instruction both
require.

---

## 8. Founder teaching-area matrix (T2.1)

| Gradex teaching area | Real KU Academic Unit | Real KU Program(s) | Relevant official Subjects | Manifest coverage | Status |
|---|---|---|---|---|---|
| **Mathematics** | College of Science → Mathematics Department | B.Sc. Mathematics, B.Sc. Financial Mathematics *(neither imported)* | `0410-101` Calculus I, `0410-102` Calculus II, `0410-111` Linear Algebra, `0410-211` Calculus III, `0410-240` Ordinary Differential Equation | All five present and mapped into CS, Cybersecurity, CpE, EE plans | `ALREADY_COVERED` — as **Subjects**, not as a target Program |
| **Software** | College of Science → Computer Science Department | B.Sc. Computer Science; B.Sc. Cybersecurity | `0418-143`, `0418-201`, `0418-220`, `0418-221`, `0418-301`, `0418-320`, `0418-321`, `0418-331`, `0418-335`, `0418-390`, `0418-470` | All present; core mapped, elective-pool members unmapped | `ALREADY_COVERED` — `NO_OFFICIAL_PROGRAM — SUBJECT_AREA_ONLY` for the name "Software" |
| **Cybersecurity** | College of Science → Computer Science Department | **B.Sc. Cybersecurity** *(real, Summer 2024)* | `0418-231`, `0418-312`, `0418-348`, `0418-435`, `0418-493`, plus `1830-351`, `1830-434`, `1830-435`, `1830-436` | **Added in revision 2**: Program, 127-credit plan, 28 mappings | `ALREADY_COVERED` *(was `MANIFEST_EXPANSION_REQUIRED`)* |
| **Data Science** | College of Life Sciences → Department of Information Science | **B.Sc. Data Science and Artificial Intelligence (DSAI)** *(real, 2025/2026, 121 credits)* — **not imported** | `0418-365` Artificial Intelligence, `0418-466` Introduction to Machine Learning, `0480-201` Statistics for Science and Engineering | Subjects present; **DSAI Program absent** | `FOUNDER_COURSE_SELECTION_REQUIRED` + `MANIFEST_EXPANSION_REQUIRED` |

**The Data Science gap is the one open item.** DSAI is confirmed real, but its
major sheet, 8-semester plan, and dependency chart are published only as `.docx`
and `.pdf` attachments whose Subject codes were not transcribed in this pass. Two
options for the Founder:

1. Transcribe the DSAI major sheet and import the Program with its Subjects. This
   is the complete answer and the recommended one.
2. Ship launch without DSAI. Data-science Students at Kuwait University could not
   select their own Program during T3 onboarding, and Gradex's data-science
   Courses would reach them only through Computer Science and Cybersecurity
   Subjects.

Adding the Program without its Subjects was rejected: a selectable Program with
an empty catalog is worse for a Student than an honestly shorter list.

## 9. Launch-relevance judgments (T2.1)

**Electrical Engineering — retained, reclassified.** EE entered the manifest in
revision 1 chiefly because its program page is the only source that itemizes the
shared Mathematics/Physics/Chemistry block with exact codes and credits. That is
research usefulness, not automatically product relevance. The launch-relevance
question is separate and does have an answer: Gradex's **Mathematics** teaching
area serves Calculus and Linear Algebra, and EE Students take exactly those
Subjects. EE therefore stays as a genuine Mathematics-area audience. It is *not*
a Program Gradex builds program-specific Courses for, and no such claim is made.
Per the established `absence != delete` rule, nothing was retired; removing it
would be a Founder decision, not a research consequence.

**Mathematics Programs — deliberately absent.** The Mathematics Department owns
B.Sc. Mathematics and B.Sc. Financial Mathematics. Gradex's Mathematics offering
teaches Calculus to Computer Science, Cybersecurity, and engineering Students, so
the Subjects are what matter; **Subject ownership is not Student audience**. Add
these Programs only if the Founder confirms Gradex targets Mathematics-major
Students specifically.

**CS elective-pool Subjects — present but unmapped.** `0418-301`, `0418-335`,
`0418-390`, `0418-470`, `0418-365`, and `0418-466` are official Kuwait University
courses in the Software and Data Science teaching areas. Kuwait University
publishes its CS elective requirement as a credit total ("CS Elective Courses,
30 credits") rather than enumerating which elective belongs to which plan, so
these are canonical Subjects with no curriculum mapping. An Instructor can select
them in T4; T6 curriculum-based ranking will not reach them until Kuwait
University publishes per-course elective placement. That is the honest shape.

## 10. CS source conflict — RESOLVED (T2.1)

Revision 1 recorded a conflict between the CS Major 2024 sheet and the CS course
catalogue page (`0418-141/142` vs `0418-143`; `0418-211` vs `0418-310`;
`0418-143` as a 3-credit elective vs a 4-credit core course).

**The Cybersecurity major sheet resolves it.** That sheet is an independent,
current, official Kuwait University plan, and it lists — verbatim —
`0418-143 Fundamentals of Computer Programming (4)`, `0418-310 Theory of
Computation`, and `0418-320 Principles of Computer Systems` as compulsory. Two
independent current major sheets agree against the course catalogue page.

**Conclusion:** the course catalogue page is **stale**, reflecting the
pre-2024 plan. Kuwait University itself confirms two plan generations exist by
publishing an "Undergraduate Major Sheets (2018/2019 until 2023/2024)" archive
alongside the 2024 sheet. This is a curriculum-version difference, not an
unresolved official contradiction. No Subject remains flagged
`SOURCE_CONFLICT_REVIEW_BEFORE_T6`.

## 11. Academic level — resolved for Computer Science (T2.1)

Revision 1 concluded no placement source existed. That was wrong; the search had
not gone past the major-sheet page.

**Kuwait University publishes an official Suggested Study Plan** for the Computer
Science 2024 major at
<https://www.cs.ku.edu.kw/computer-science-major-2024-kuwait-university-2/>.
It is organised as four named years — `FRESHMAN YEAR / السنة الأولى` through
`SENIOR YEAR / السنة الرابعة` — each with a Fall and a Spring semester, and it
places named courses in them (Freshman Fall: `CS101, CS111, UNIV. COMP. 1,
ENG161, MATH101`; Freshman Spring: `CS143, SCIENCE 1, UNIV. COMP. 2, ENG162,
MATH102`; and so on through Senior Spring).

**Curation step, recorded rather than hidden:** the plan writes courses in short
form (`CS101`, `MATH102`, `ENG161`, `STAT201`). These resolve to this
university's full codes by prefix — `CS`=`0418`, `MATH`=`0410`, `ENG`=`9988`,
`STAT`=`0480` — corroborated by the Major 2024 sheet itself, which lists the same
courses under the full codes. That is a resolution, not a transcription.

**Semantic caution.** The plan's *recommended year* and the Student Manual's
credit-derived `الفرقة الدراسية` are different concepts that happen to share a
scale. The plan says when to take a course; the manual measures where a Student
stands, across **five** bands. `recommended_level` stores the former only. No
Student standing is derived anywhere.

**Result: `PARTIAL`.** One of four launch curricula has authoritative placement.

| Curriculum | Level classification |
|---|---|
| Computer Science 2024 | `AUTHORITATIVE_LEVEL_AVAILABLE` **and** `AUTHORITATIVE_SEMESTER_AVAILABLE` |
| Cybersecurity current | `NO_AUTHORITATIVE_PLACEMENT` |
| Computer Engineering current | `NO_AUTHORITATIVE_PLACEMENT` |
| Electrical Engineering current | `NO_AUTHORITATIVE_PLACEMENT` |

### T6 consequence — stated, not hidden

T3 can collect `المستوى الدراسي` truthfully. **T6 can promise meaningful
level-specific ranking only to Computer Science Students.** For Cybersecurity,
Computer Engineering, and Electrical Engineering Students, ranking must fall back
from "my curriculum at my level" to "my curriculum at any level" — which the
tiered design already does by construction.

**Recommendation: option A now, option B before T6.** Keep Level in the Student
profile and let ranking fall back where placement is absent (A); in parallel,
pursue authoritative placement for the other three curricula (B) — the DSAI
programme's published 8-semester plan proves Kuwait University does produce such
documents, so the Cybersecurity and engineering equivalents are worth requesting.
**Option C — a Gradex-invented "recommended level" — is not implemented and needs
explicit Founder approval before anyone builds it.**

---

## 12. Revision 3 — T2.2 Data Science & Artificial Intelligence (2026-08-22)

### 12.1 The honest history of this dataset

Recorded as a sequence, not rewritten as though DSAI had always been present.

| Stage | What the record said about Data Science | Status |
|---|---|---|
| **v1.0 (T2)** | "Kuwait University does not expose degree Programs under those exact names, so none was invented." A test encoded it. | **Factually wrong.** A scope error: research followed the repository persona and never searched Kuwait University for the Founder's teaching areas by name. |
| **v1.1 (T2.1)** | Discovered the **B.Sc. Data Science and Artificial Intelligence** is real, in the Department of Information Science, College of Life Sciences. Left it **out** of the manifest because its major sheet was published only as `.docx` and its Subject codes were untranscribed. Flagged as the highest-value follow-up. | Correct discovery, deliberate deferral. |
| **v1.2 (T2.2)** | **Founder Decision 1: include it.** The `.docx` documents were extracted and the full official curriculum transcribed from the English major sheet, the Arabic major sheet, and the 8-semester plan. | **Resolved.** |

### 12.2 Founder decisions applied

**Decision 1 — Data Science included.** DSAI is a real launch teaching area and a
real Kuwait University degree. A Kuwait University Data Science Student can now
select their own Program.

**Decision 2 — Mathematics majors excluded.** B.Sc. Mathematics and B.Sc.
Financial Mathematics are academically real and deliberately outside current
commercial targeting. **This is product scope, not missing academic knowledge.**
The Mathematics *department* and its canonical Subjects stay, because Gradex
teaches Calculus and Linear Algebra to Students in the five launch Programs.
Subject ownership is not target audience. A test asserts both halves.

### 12.3 Official DSAI structure

```
Kuwait University
└── College of Life Sciences / كلية العلوم الحياتية        [official]
    └── Information Science / قسم علوم المعلومات           [official]
        └── B.Sc. Data Science and Artificial Intelligence
            / علم البيانات والذكاء الاصطناعي                [official Arabic]
```

No `Data Science Department`, `AI Department`, or `Computing College` was
invented; a test asserts none exists.

### 12.4 Curriculum version

Kuwait University publishes **no plan version label** for DSAI. Its documents
carry dates — major sheets *12 Nov 2024*, 8-semester plan *6 Apr 2025* — rather
than a version name, unlike the Computer Science "2024" sheet. The programme
launched for academic year 2025/2026, which is recorded as
`effective_from_year: 2025`. The label therefore uses the established Gradex
placeholder mechanism: `version_label: current`,
`version_label_source: gradex_translation`, with a note stating it must never be
shown to a Student as the university's own version name. **`2025/26` was not
adopted as a label because no official source publishes it as one.**

### 12.5 The code-scheme question, resolved

The 8-semester plan writes courses in short form — `ISC 101`, `MATH 111`,
`DSAI 102`, `CLS 109`, `ELU 126`, `STAT 210`, `HIS 100` — which initially looked
like a separate College of Life Sciences namespace. **The official major sheet
prints the full numeric codes and resolves it**: `MATH 111` is `0410-111`,
`MATH 101` is `0410-101`, `HIS 100` is `0330-100`, `ISC 101` is `1830-101`,
`DSAI 102` is `1832-102`, `STAT 210` is `0480-210`. Kuwait University's numeric
scheme is university-wide, so **shared-Subject reuse applies** and no parallel
copies were minted. Code families: `1830-` Information Science, `1832-` the DSAI
programme, `1800-` College of Life Sciences general education.

### 12.6 Shared Subject reuse

Three canonical Subjects were **reused, not duplicated**:

| Code | Title | Curricula served |
|---|---|---|
| `0410-101` | Calculus I | **All five** launch Programs |
| `0410-111` | Linear Algebra | CS, Cybersecurity, DSAI, CpE, EE |
| `0330-100` | Modern and Cont. Hist. of Kuwait | CS, Cybersecurity, DSAI |

**The strongest architecture proof in the dataset:** the Computer Science plan
places `0410-101` in **Year 1, Semester 1**; the DSAI plan places the *same
Subject row* in **Year 1, Semester 2**. One canonical Subject, two official
sequencings — which is exactly why placement lives on the CurriculumSubject
mapping rather than on the Subject.

### 12.7 Level and semester placement

The official 8-semester plan places every course it names by code. The rule,
recorded rather than inferred: **semester 1–8 → year = ceil(semester / 2), term
1 = the year's first semester.** Its `xxx` elective slots name no course, so
nothing is placed for them.

| Curriculum | Placed | Total | Classification |
|---|---|---|---|
| Data Science and AI | **33** | 43 | `AUTHORITATIVE_LEVEL_AVAILABLE` + `AUTHORITATIVE_SEMESTER_AVAILABLE` |
| Computer Science 2024 | 16 | 19 | `AUTHORITATIVE_LEVEL_AVAILABLE` + `AUTHORITATIVE_SEMESTER_AVAILABLE` |
| Cybersecurity / CpE / EE | 0 | 28 / 11 / 11 | `NO_AUTHORITATIVE_PLACEMENT` |

**T6 consequence, restated:** level-specific ranking can now be promised to
**Computer Science and Data Science & AI** Students. Cybersecurity, Computer
Engineering, and Electrical Engineering Students still fall back to curriculum
relevance. Level personalization remains **`PARTIAL`** — but two of five
Programs now have it, up from one of four.

### 12.8 Elective treatment — no degree-audit logic

Kuwait University enumerates the DSAI programme electives by code and says
"select 2 or 3". The T1 model expresses no choose-N semantics and **this pass
built none**. The seven enumerated electives are canonical Subjects mapped as
`MAJOR_ELECTIVE`, which states availability without claiming a Student must take
each one. Requirement *groups* — "General Elective (GE)", "Free Elective",
"DSAI Elective 1" — created **no** Subjects.

The **Readiness Program / البرنامج التحضيري** (`0410-091`, `9988-097`,
`9988-107`) is credit-bearing and coded, so its courses are real Subjects. The T1
`requirement_kind` enum has no readiness category and adding one would be a
schema migration this pass forbids, so they map as `SUPPORTING` — the nearest
existing category. **That choice is recorded, not hidden.** Its existence did not
flip `has_foundation_stage`: a programme-level readiness block is not a
university-wide foundation year.

### 12.9 Deliberately excluded from the DSAI import

| Excluded | Why |
|---|---|
| ~11 general-education **elective options** (Environment and Society, Food Safety and Society, Deafness and Sign Language, Critical Thinking Skills, Leadership Development, …) | Real coded Subjects, but outside every Gradex teaching area. Warehouse-building. |
| 7 **General Computing electives** (`1830-243/341/334/421/440/443/444`) | Real, and adjacent to the Software teaching area, but a programme-specific elective pool. Software is already covered by 11 Computer Science Subjects. Addable later without schema change. |
| B.Sc. **Information Science** | The same department confers it, but it is not a Founder launch teaching area. |

### 12.10 Arabic provenance

The **official Arabic major sheet** is a genuine quality win: all 40 new DSAI
Subjects and the Programme name carry Kuwait University's own Arabic, marked
`official`.

Two **existing** Subjects were upgraded from `gradex_translation` to `official`:
`0410-111` (الجبر الخطي) and `0330-100` (تاريخ الكويت الحديث والمعاصر). In both
cases the Gradex translation matched the university's published wording exactly,
so **the strings did not change — only the provenance claim did**, from "Gradex
supplied this" to "the university publishes this".

`0410-101` was **not** changed. The DSAI Arabic sheet titles it `الحسبان`
("Calculus") while the College of Science and engineering sheets title it
"Calculus I". Same code, different display conventions across colleges; changing
a Subject shared by five Programs on one college's wording would be churn with a
real downside. Recorded as a title-variance observation, not a correction.

### 12.11 Remaining uncertainties

1. **`0480-423` Multivariate Analysis credits conflict.** The English major sheet
   prints `3(3-0-3)`; the Arabic sheet prints `4(3-3-4)` for the same code.
   Neither value is asserted — credits are **NULL** — and the conflict is
   recorded here rather than resolved by preference.
2. **The four Cybersecurity `1830-` Subjects still carry no owning unit.** T2.2
   established that `1830-` is Information Science, so their owner is now known.
   Assigning it would be an ownership change, which the importer **correctly**
   refuses as an identity rebind. Deferred as an explicit follow-up requiring a
   deliberate resolution — not an oversight, and not a reason to weaken the
   guard.

### 12.12 Updated Founder teaching-area matrix

| Teaching area | Real KU unit | Real KU Program(s) | Manifest coverage | Status |
|---|---|---|---|---|
| **Mathematics** | College of Science → Mathematics | B.Sc. Mathematics, B.Sc. Financial Mathematics *(deliberately excluded)* | 5 Math Subjects mapped into **all five** launch curricula | `ALREADY_COVERED` — Subjects, not a target Program *(Founder Decision 2)* |
| **Software** | College of Science → Computer Science | B.Sc. Computer Science; B.Sc. Cybersecurity | 11 Subjects | `ALREADY_COVERED` · `NO_OFFICIAL_PROGRAM — SUBJECT_AREA_ONLY` for the name "Software" |
| **Cybersecurity** | College of Science → Computer Science | **B.Sc. Cybersecurity** | Program + 127-credit plan + 28 mappings | `ALREADY_COVERED` |
| **Data Science** | College of Life Sciences → Information Science | **B.Sc. Data Science and Artificial Intelligence** | **Program + full plan + 43 mappings + 40 Subjects** | `ALREADY_COVERED` *(was `FOUNDER_COURSE_SELECTION_REQUIRED`)* |

**All four Founder teaching areas are now covered.**

### 12.13 Launch Program set — final

1. B.Sc. Computer Science · 2. B.Sc. Cybersecurity · 3. **B.Sc. Data Science and
Artificial Intelligence** · 4. B.Sc. Computer Engineering · 5. B.Sc. Electrical
Engineering

Verified absent by test: `Software Engineering`, `Cybersecurity Engineering`,
standalone `Data Science`, `Programming`, `Mathematics`, `Financial Mathematics`.

**Electrical Engineering retained** on the T2.1 reasoning the Founder accepted:
EE Students are a genuine audience for the launch Mathematics Subjects. No
EE-specific commercial inventory was added merely because the Program exists.

### 12.14 Counts

| Entity | v1.1.0 | **v1.2.0** |
|---|---|---|
| Institutions | 1 | **1** |
| Academic Units | 9 | **11** |
| Programs | 4 | **5** |
| Curricula | 4 | **5** |
| Subjects | 44 | **84** |
| CurriculumSubject mappings | 69 | **112** |
| Sources | 16 | **20** |

### 12.15 Update-path proof — no reset

Run against the **existing v1.1 catalog**, never a truncated one:

```text
dry-run   create=87 update=0 noop=131 drift=0    → subjects still 44: nothing written
apply     create=87 update=0 noop=131 drift=0    → 11 units, 5 programs, 5 curricula, 84 subjects, 112 mappings
apply #2  create=0  update=0 noop=218 drift=0    → identical
```

**Identity stability verified by diff:** `0410-101`, `0410-111`, `0330-100` and
all four pre-existing Programs kept their exact database identifiers. **Zero
retirements** — `absence != delete` held throughout.

### 12.16 Sources added

| id | Source |
|---|---|
| `ku-dsai-major-sheet-en` | [Data Science & AI Major Sheet (English), 12 Nov 2024](https://cls.ku.edu.kw/sites/default/files/2025-05/Data%20Science%20%26%20AI%20Major%20Sheet%20English%20-%2012%20Nov%2024.docx) |
| `ku-dsai-major-sheet-ar` | [صحائف تخرج برنامج علم البيانات والذكاء الاصطناعي, 12 Nov 2024](https://cls.ku.edu.kw/sites/default/files/2025-05/Data%20Science%20%26%20AI%20Major%20Sheet%20Arabic%20-%2012%20Nov%2024_0.docx) |
| `ku-dsai-8-semester-plan` | [Data Science & AI — 8 Semester Plan, 6 Apr 2025](https://www.ku.edu.kw/sites/default/files/2025-05/Data%20Science%20%26%20AI%20-%208%20Semester%20Plan-%206-April-2025.docx) |
| `ku-dsai-dependency-chart` | [DS Dependency Chart, 17 Mar 2025](https://www.ku.edu.kw/sites/default/files/2025-05/DS%20Dependency%20Chart%20-%2017%20Mar%202025.pdf) |

All primary Kuwait University documents. Baims contributed nothing.
