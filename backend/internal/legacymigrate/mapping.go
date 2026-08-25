// Package legacymigrate carries a Course from the legacy taxonomy
// classification onto the canonical Academic Catalog (D-091 §13, T5).
//
// # WHY A MAPPING FILE EXISTS AT ALL
//
// The legacy vocabulary has no Institution. A `taxonomy_terms` row is a bare
// MAJOR or SUBJECT label with an optional academic code, and nothing in it says
// which university it belongs to. The Academic Catalog, by contrast, scopes
// every Subject and Program to an Institution. That gap cannot be closed by
// inference — it is missing information, not ambiguous information — so it is
// closed by a checked-in, Founder-authored mapping.
//
// The mapping is deliberately a data file rather than a heuristic. Guessing a
// Subject from a label would silently invent academic identity for a Course a
// Student may already have purchased, which is exactly the defect the redesign
// exists to remove.
package legacymigrate

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Mappings are embedded rather than read from disk, for the same reason the
// Academic Catalog manifests are: an operator names an identifier, never a path,
// so no filesystem argument reaches this package from a request.
//
//go:embed data/*.yaml
var files embed.FS

// SubjectMapping resolves one legacy SUBJECT term onto one canonical Subject.
type SubjectMapping struct {
	// TermCode is the legacy taxonomy_terms.academic_code, matched after
	// normalization so "MATH101", "math-101" and "MATH 101" are one key.
	TermCode string `yaml:"term_code"`
	// TermLabelEn is carried for human review of the file and for the report.
	// It is NEVER used for matching: a label is prose, not identity.
	TermLabelEn string `yaml:"term_label_en"`
	// SubjectCode is the canonical Subject's official code within the
	// Institution. Matching is on the normalized form (D-093 §7).
	SubjectCode string `yaml:"subject_code"`
}

// MajorMapping resolves one legacy MAJOR term onto the Programs a migrated
// Course should target.
//
// This becomes revision-scoped audience metadata, never Course identity: a
// legacy Major is closer to "who this was for" than to "what this teaches".
type MajorMapping struct {
	TermLabelEn  string   `yaml:"term_label_en"`
	ProgramSlugs []string `yaml:"program_slugs"`
}

// SubjectCandidate is one canonical Subject a Founder may choose for a legacy
// term that cannot be resolved automatically.
type SubjectCandidate struct {
	SubjectCode string `yaml:"subject_code"`
	// Note records why this candidate is plausible. It is documentation for the
	// Founder, never an input to any decision.
	Note string `yaml:"note"`
}

// Disposition records what the Founder has said about a pending term.
//
// The distinction is real and the report must not blur it: a term nobody has
// looked at yet and a term the Founder has examined and deliberately left
// unresolved are different states of the product, even though both are equally
// fail-closed. Only the second one is finished.
type Disposition string

const (
	// DispositionAwaitingDecision is the default: the question is recorded and
	// no one has answered it.
	DispositionAwaitingDecision Disposition = "AWAITING_FOUNDER_DECISION"
	// DispositionKeepUnresolved is a Founder decision, not an absence of one.
	// The Founder has examined the candidates and determined that none may be
	// chosen without authoritative evidence, so the legacy record keeps its
	// legacy identity indefinitely. This is a terminal, accepted state.
	DispositionKeepUnresolved Disposition = "KEEP_UNRESOLVED"
)

// PendingDecision records a legacy SUBJECT term whose canonical identity is
// genuinely undecidable by this tool.
//
// It exists because "unmapped" and "the tool found several defensible answers"
// are different facts. A legacy code with no canonical counterpart and several
// plausible canonical Subjects is not missing from the mapping by oversight;
// it is a recorded product question. Declaring it here makes the report say so
// explicitly and, critically, keeps the Course out of the MIGRATE set — the
// mapping file never contains a guess.
//
// Decision records what the Founder concluded. It changes what the report SAYS
// and nothing about what the migrator DOES: an entry is fail-closed under every
// disposition, because a decision to keep a record unresolved is still a
// decision not to migrate it.
type PendingDecision struct {
	TermCode    string `yaml:"term_code"`
	TermLabelEn string `yaml:"term_label_en"`
	// CourseTitleEn is carried only so the Founder recognises the record. It is
	// never matched on: a Course title is prose, not academic identity.
	CourseTitleEn string             `yaml:"course_title_en"`
	Why           string             `yaml:"why"`
	Candidates    []SubjectCandidate `yaml:"candidates"`
	// Decision defaults to AWAITING_FOUNDER_DECISION when the file omits it, so
	// an entry can never silently claim to have been decided.
	Decision Disposition `yaml:"decision"`
	// DecidedOn dates the decision. Required whenever Decision is set, because a
	// decision no one can date is not auditable.
	DecidedOn string `yaml:"decided_on"`
	// ResolutionRequires states the authoritative evidence that would permit the
	// mapping to change later. It is what stops a terminal "unresolved" from
	// reading as "unresolvable".
	ResolutionRequires []string `yaml:"resolution_requires"`
}

// Disposition reports the recorded disposition, defaulting to awaiting.
func (p PendingDecision) Disposition() Disposition {
	if strings.TrimSpace(string(p.Decision)) == "" {
		return DispositionAwaitingDecision
	}
	return p.Decision
}

// Mapping is one Institution's complete legacy translation.
type Mapping struct {
	ID               string            `yaml:"id"`
	Version          string            `yaml:"version"`
	InstitutionSlug  string            `yaml:"institution_slug"`
	Subjects         []SubjectMapping  `yaml:"subjects"`
	Majors           []MajorMapping    `yaml:"majors"`
	PendingDecisions []PendingDecision `yaml:"pending_decisions"`
}

// NormalizeCode mirrors the SQL academic_normalize_code so the planner can key a
// map without a round trip. The database remains the authority for identity;
// this only groups keys.
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Validate rejects a mapping that could produce an ambiguous or unusable
// migration. It is a pure function of the file, so a malformed mapping fails at
// load rather than part-way through an apply.
func (m *Mapping) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("mapping id is required")
	}
	if strings.TrimSpace(m.InstitutionSlug) == "" {
		return fmt.Errorf("mapping %s: institution_slug is required", m.ID)
	}
	seenTerm := map[string]struct{}{}
	for _, subject := range m.Subjects {
		term := NormalizeCode(subject.TermCode)
		if term == "" {
			return fmt.Errorf("mapping %s: a subject entry has no usable term_code", m.ID)
		}
		if NormalizeCode(subject.SubjectCode) == "" {
			return fmt.Errorf("mapping %s: term %s has no usable subject_code", m.ID, subject.TermCode)
		}
		// Two entries for one legacy term would make the translation itself
		// ambiguous, which is precisely what this file exists to prevent.
		if _, exists := seenTerm[term]; exists {
			return fmt.Errorf("mapping %s: legacy term %s is mapped more than once", m.ID, subject.TermCode)
		}
		seenTerm[term] = struct{}{}
	}
	seenMajor := map[string]struct{}{}
	for _, major := range m.Majors {
		label := strings.TrimSpace(major.TermLabelEn)
		if label == "" {
			return fmt.Errorf("mapping %s: a major entry has no term_label_en", m.ID)
		}
		if _, exists := seenMajor[label]; exists {
			return fmt.Errorf("mapping %s: legacy major %q is mapped more than once", m.ID, label)
		}
		seenMajor[label] = struct{}{}
		for _, slug := range major.ProgramSlugs {
			if strings.TrimSpace(slug) == "" {
				return fmt.Errorf("mapping %s: major %q has an empty program slug", m.ID, label)
			}
		}
	}
	seenPending := map[string]struct{}{}
	for _, pending := range m.PendingDecisions {
		term := NormalizeCode(pending.TermCode)
		if term == "" {
			return fmt.Errorf("mapping %s: a pending decision has no usable term_code", m.ID)
		}
		// A term cannot be both resolved and pending: that would make the file
		// itself say two different things about one legacy identity.
		if _, resolved := seenTerm[term]; resolved {
			return fmt.Errorf(
				"mapping %s: legacy term %s is both mapped and pending a Founder decision",
				m.ID, pending.TermCode)
		}
		if _, exists := seenPending[term]; exists {
			return fmt.Errorf("mapping %s: legacy term %s is pending more than once", m.ID, pending.TermCode)
		}
		seenPending[term] = struct{}{}
		if len(pending.Candidates) == 0 {
			return fmt.Errorf(
				"mapping %s: pending term %s lists no candidates; use omission for a genuinely unknown term",
				m.ID, pending.TermCode)
		}
		if strings.TrimSpace(pending.Why) == "" {
			return fmt.Errorf("mapping %s: pending term %s must state why the choice is unsafe", m.ID, pending.TermCode)
		}
		for _, candidate := range pending.Candidates {
			if NormalizeCode(candidate.SubjectCode) == "" {
				return fmt.Errorf("mapping %s: pending term %s has a candidate with no usable subject_code",
					m.ID, pending.TermCode)
			}
		}
		switch pending.Disposition() {
		case DispositionAwaitingDecision, DispositionKeepUnresolved:
		default:
			return fmt.Errorf("mapping %s: pending term %s has an unknown decision %q",
				m.ID, pending.TermCode, pending.Decision)
		}
		// A recorded decision must be datable and must say what would reopen it.
		// Without both, "the Founder decided to leave this alone" is
		// indistinguishable from "nobody has looked at it", which is the exact
		// confusion this field exists to remove.
		if pending.Disposition() != DispositionAwaitingDecision {
			if strings.TrimSpace(pending.DecidedOn) == "" {
				return fmt.Errorf("mapping %s: pending term %s records a decision with no decided_on date",
					m.ID, pending.TermCode)
			}
			if len(pending.ResolutionRequires) == 0 {
				return fmt.Errorf(
					"mapping %s: pending term %s records a decision but no resolution_requires evidence",
					m.ID, pending.TermCode)
			}
		}
	}
	return nil
}

// SubjectFor returns the canonical Subject code a legacy term translates to.
func (m *Mapping) SubjectFor(termCode string) (string, bool) {
	key := NormalizeCode(termCode)
	if key == "" {
		return "", false
	}
	for _, subject := range m.Subjects {
		if NormalizeCode(subject.TermCode) == key {
			return subject.SubjectCode, true
		}
	}
	return "", false
}

// PendingFor reports whether a legacy term is a recorded, unresolved Founder
// decision. Matching is on the normalized code only, exactly as SubjectFor.
func (m *Mapping) PendingFor(termCode string) (PendingDecision, bool) {
	key := NormalizeCode(termCode)
	if key == "" {
		return PendingDecision{}, false
	}
	for _, pending := range m.PendingDecisions {
		if NormalizeCode(pending.TermCode) == key {
			return pending, true
		}
	}
	return PendingDecision{}, false
}

// CandidateCodes lists a pending decision's candidate Subject codes in file
// order, which is the order the Founder wrote them.
func (p PendingDecision) CandidateCodes() []string {
	codes := make([]string, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		codes = append(codes, candidate.SubjectCode)
	}
	return codes
}

// ProgramsFor returns the Program slugs a legacy Major translates to. A Major
// with no mapping is not an error: the migrated Course simply keeps the
// automatic audience its Subject already implies.
func (m *Mapping) ProgramsFor(labelEn string) []string {
	for _, major := range m.Majors {
		if strings.TrimSpace(major.TermLabelEn) == strings.TrimSpace(labelEn) {
			return major.ProgramSlugs
		}
	}
	return nil
}

// Available lists every embedded mapping identifier, sorted for determinism.
func Available() ([]string, error) {
	entries, err := files.ReadDir("data")
	if err != nil {
		return nil, fmt.Errorf("reading embedded mappings: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(entry.Name(), ".yaml"))
	}
	sort.Strings(ids)
	return ids, nil
}

// Load resolves a mapping identifier to its embedded, validated content.
func Load(id string) (*Mapping, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("mapping identifier is required")
	}
	// Only an exact embedded name resolves, so no traversal is expressible.
	available, err := Available()
	if err != nil {
		return nil, err
	}
	known := false
	for _, candidate := range available {
		if candidate == id {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("unknown legacy mapping %q", id)
	}
	raw, err := files.ReadFile("data/" + id + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("reading legacy mapping %s: %w", id, err)
	}
	var mapping Mapping
	if err := yaml.Unmarshal(raw, &mapping); err != nil {
		return nil, fmt.Errorf("parsing legacy mapping %s: %w", id, err)
	}
	if err := mapping.Validate(); err != nil {
		return nil, err
	}
	return &mapping, nil
}
