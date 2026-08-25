package legacymigrate

import "testing"

// The checked-in mapping must load and validate. It carries a real Founder
// pending decision, so a malformed edit to it is a build-time failure rather
// than something discovered during a migration run.
func TestCheckedInMappingLoads(t *testing.T) {
	mapping, err := Load("kuwait-university-legacy-v1")
	if err != nil {
		t.Fatalf("loading checked-in mapping: %v", err)
	}
	if mapping.InstitutionSlug != "kuwait-university" {
		t.Fatalf("institution slug = %q", mapping.InstitutionSlug)
	}
	// Normalization is the only matching authority, so a differently-spelled
	// form of the same code must resolve to the same pending decision.
	pending, waiting := mapping.PendingFor("swe-101")
	if !waiting {
		t.Fatal("SWE101 must be a recorded Founder decision, not silently unmapped")
	}
	if len(pending.Candidates) < 2 {
		t.Fatalf("a pending decision needs candidates to be a decision; got %d", len(pending.Candidates))
	}
	// The whole point of the pending list is that the tool has NOT chosen.
	if _, chosen := mapping.SubjectFor("SWE101"); chosen {
		t.Fatal("SWE101 must not also resolve to a Subject")
	}

	// The Founder decision of 2026-08-23: intentionally unresolved, not pending.
	if pending.Disposition() != DispositionKeepUnresolved {
		t.Fatalf("disposition = %q, want the recorded Founder decision", pending.Disposition())
	}
	if pending.DecidedOn == "" {
		t.Fatal("a recorded decision must be datable")
	}
	if len(pending.ResolutionRequires) == 0 {
		t.Fatal("an intentionally unresolved record must state what would reopen it")
	}
}

// An entry with no decision is still an open question, so the default must not
// silently claim the Founder has answered it.
func TestPendingDecisionDefaultsToAwaiting(t *testing.T) {
	if (PendingDecision{}).Disposition() != DispositionAwaitingDecision {
		t.Fatal("an undecided pending entry must default to awaiting a decision")
	}
}

// A recorded decision must be auditable: dated, and explicit about what would
// legitimately reopen it. Otherwise "decided" is indistinguishable from
// "nobody looked", which is the confusion the field exists to remove.
func TestMappingRejectsUndatableOrIrreversibleDecision(t *testing.T) {
	base := func() *Mapping {
		return &Mapping{
			ID: "x", InstitutionSlug: "y",
			PendingDecisions: []PendingDecision{{
				TermCode: "ABC1", Why: "conflicting evidence",
				Candidates:         []SubjectCandidate{{SubjectCode: "0418-390"}},
				Decision:           DispositionKeepUnresolved,
				DecidedOn:          "2026-08-23",
				ResolutionRequires: []string{"the official subject code"},
			}},
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("a complete recorded decision must validate: %v", err)
	}
	for name, mutate := range map[string]func(*PendingDecision){
		"no decided_on":          func(p *PendingDecision) { p.DecidedOn = "" },
		"no resolution_requires": func(p *PendingDecision) { p.ResolutionRequires = nil },
		"unknown disposition":    func(p *PendingDecision) { p.Decision = "MIGRATE_IT_ANYWAY" },
	} {
		t.Run(name, func(t *testing.T) {
			mapping := base()
			mutate(&mapping.PendingDecisions[0])
			if err := mapping.Validate(); err == nil {
				t.Fatalf("%s must be rejected at load", name)
			}
		})
	}
}

// A term cannot be both resolved and pending: the file would then say two
// different things about one legacy identity.
func TestMappingRejectsResolvedAndPendingTerm(t *testing.T) {
	mapping := &Mapping{
		ID: "x", InstitutionSlug: "y",
		Subjects: []SubjectMapping{{TermCode: "SWE101", SubjectCode: "0418-390"}},
		PendingDecisions: []PendingDecision{{
			TermCode: "swe 101", Why: "conflicting",
			Candidates: []SubjectCandidate{{SubjectCode: "0418-301"}},
		}},
	}
	if err := mapping.Validate(); err == nil {
		t.Fatal("a term that is both mapped and pending must be rejected at load")
	}
}

// A pending entry with no candidates is not a decision, it is an omission, and
// omission already has a meaning (UNMAPPED).
func TestMappingRejectsPendingWithoutCandidates(t *testing.T) {
	mapping := &Mapping{
		ID: "x", InstitutionSlug: "y",
		PendingDecisions: []PendingDecision{{TermCode: "ABC1", Why: "because"}},
	}
	if err := mapping.Validate(); err == nil {
		t.Fatal("a candidate-less pending decision must be rejected at load")
	}
}

func TestMappingRejectsPendingWithoutReason(t *testing.T) {
	mapping := &Mapping{
		ID: "x", InstitutionSlug: "y",
		PendingDecisions: []PendingDecision{{
			TermCode: "ABC1", Candidates: []SubjectCandidate{{SubjectCode: "0418-390"}},
		}},
	}
	if err := mapping.Validate(); err == nil {
		t.Fatal("a pending decision must state why the choice is unsafe")
	}
}
