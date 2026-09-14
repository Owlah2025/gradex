"""Resolve every scraped Kuwait University row to the official Subject it duplicates.

This regenerates the table in D-106 §3. It exists because the claim "all nineteen
scraped rows already exist" is the reason Kuwait University is excluded from the
import, and a reviewer must be able to re-derive that rather than take it on faith.

An earlier draft of D-106 claimed four rows had no counterpart. That came from an
exact-title comparison, which cannot see the source's abbreviations
("Prog & Problem solving" against "Programming and Problem Solving"). Matching on
department prefix plus course number is the correct comparison and leaves nothing
outstanding.

Usage:
    python3 ku_resolution.py official_subjects.txt

where official_subjects.txt holds one "code_normalized|title_en" line per Subject,
as produced by:
    SELECT code_normalized, title_en FROM subjects WHERE retired_at IS NULL;
"""
import re
import sys

# Kuwait University's numeric scheme encodes the owning department in the leading
# digits. The Gradex catalogue's letter prefix names the same department, and the
# trailing three-digit course number is identical on both sides.
DEPARTMENT_PREFIX = {
    "CLS": "1800",   # College of Life Sciences
    "ISC": "1830",   # Information Science
    "DSAI": "1832",  # Data Science and Artificial Intelligence
    "MATH": "0410",  # Mathematics
    "STAT": "0480",  # Statistics and Operations Research
}

# The nineteen Kuwait University rows the scrape carries, as the source records
# them. Kept literal so this script proves the claim without needing the scrape.
SCRAPED = [
    ("CLS 109", "Statistics"),
    ("DSAI 102", "Intro. to data science and AI"),
    ("DSAI 200", "Introduction to Artificial Intelligence"),
    ("DSAI 242", "Data Acquisition & Management"),
    ("DSAI 343", "Data Mining"),
    ("DSAI 345", "Data Analytics and Visualization"),
    ("DSAI 346", "Data and Artificial Intelligence Ethics"),
    ("DSAI 348", "Machine Learning"),
    ("DSAI 446", "Deep Learning"),
    ("ISC 112", "Discrete Structures for Information Sciences"),
    ("ISC 140", "Prog & Problem solving"),
    ("ISC 151", "Information Security and Cryptography"),
    ("ISC 220", "Database Systems I"),
    ("ISC 244", "Application Development & Programming"),
    ("ISC 245", "Data Structures & Algorithms"),
    ("MATH 101", "Calculus"),
    ("MATH 111", "Linear Algebra"),
    ("STAT 210", "Introduction to Prob"),
    ("STAT 240", "Statistical Methods"),
]


def load_official(path):
    official = {}
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            line = line.rstrip("\n")
            if "|" not in line:
                continue
            code, title = line.split("|", 1)
            official[code.strip()] = title.strip()
    return official


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    official = load_official(sys.argv[1])

    rows, unresolved = [], []
    for code, title in SCRAPED:
        match = re.match(r"^([A-Z]+)\s*(\d{3})$", code)
        if not match:
            unresolved.append((code, title, "unparseable code"))
            continue
        letters, number = match.group(1), match.group(2)
        prefix = DEPARTMENT_PREFIX.get(letters)
        if prefix is None:
            unresolved.append((code, title, f"no department mapping for {letters!r}"))
            continue
        candidate = prefix + number
        if candidate in official:
            rows.append((code, candidate, title, official[candidate]))
        else:
            unresolved.append((code, title, f"expected {candidate}, absent"))

    print(f"| scraped | official | source title | official title |")
    print(f"|---|---|---|---|")
    for code, candidate, title, official_title in rows:
        print(f"| `{code}` | `{candidate}` | {title} | {official_title} |")

    print(f"\nresolved {len(rows)} of {len(SCRAPED)}; unresolved {len(unresolved)}")
    for code, title, why in unresolved:
        print(f"  UNRESOLVED {code} {title!r}: {why}")

    # The exclusion in D-106 §3 depends on this being total. A partial result
    # means some scraped row is genuinely new and the decision needs revisiting.
    if unresolved:
        sys.exit(1)


if __name__ == "__main__":
    main()
