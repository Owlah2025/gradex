package academic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

// Student demand for an unserved Subject (D-106 §6).
//
// # WHAT A DEMAND SIGNAL IS
//
// One Student saying "teach me this Subject". It is a production-prioritisation
// input and nothing else. It grants no entitlement, reserves no seat, carries no
// price, creates no Course, and promises none. Nothing in the access,
// entitlement, enrollment, purchase, or media-playback path may ever read this
// data, and no function in this file returns anything an authorization decision
// could consume.
//
// # WHY IT IS NOT A SUBJECT REQUEST
//
// subject_requests is an Instructor asking Admin to add a Subject the catalog is
// missing; it resolves by mutating the catalog. This is a Student asking for
// teaching of a Subject that already exists; it resolves — if ever — by someone
// authoring a Course. Different actor, different resolution, different lifetime.
// Sharing one table would make "pending" mean two incompatible things.

var (
	// ErrSubjectDemandAlreadyRaised is returned when a Student already holds a
	// live signal for the Subject. It is a conflict, not a validation failure:
	// the request was well formed and the Student's intent is already recorded.
	ErrSubjectDemandAlreadyRaised = errors.New("this student already has a live demand signal for the subject")

	// ErrSubjectDemandNotFound is returned when withdrawing a signal that does
	// not exist or is already withdrawn.
	ErrSubjectDemandNotFound = errors.New("no live demand signal exists to withdraw")

	// ErrSubjectDemandNoteTooLong mirrors the database check so the API can
	// answer a validation problem rather than surfacing a constraint violation.
	ErrSubjectDemandNoteTooLong = errors.New("a demand note may not exceed 500 characters")

	// ErrSubjectDemandSubjectInvalid is returned when the supplied Subject
	// identifier is not a UUID at all.
	//
	// It exists so a malformed identifier is a client error rather than a
	// server error. Passing the raw value through to a ::uuid cast makes
	// PostgreSQL raise invalid_text_representation, which every caller would
	// see as a 500 — a typo in a request body is not a server fault, and
	// answering it as one hides real faults in the same signal.
	ErrSubjectDemandSubjectInvalid = errors.New("the subject identifier is not a valid UUID")
)

// validSubjectID guards every ::uuid cast in this file.
func validSubjectID(subjectID string) error {
	if _, err := uuid.Parse(strings.TrimSpace(subjectID)); err != nil {
		return ErrSubjectDemandSubjectInvalid
	}
	return nil
}

// subjectDemandNoteLimit matches subject_demand_signals_note_length.
const subjectDemandNoteLimit = 500

// SubjectDemandSignal is one Student's live interest in one Subject.
type SubjectDemandSignal struct {
	ID              string `json:"id"`
	SubjectID       string `json:"subject_id"`
	InstitutionID   string `json:"institution_id"`
	InstitutionSlug string `json:"institution_slug"`
	SubjectCode     string `json:"subject_code,omitempty"`
	SubjectTitleAr  string `json:"subject_title_ar"`
	SubjectTitleEn  string `json:"subject_title_en"`
	Note            string `json:"note,omitempty"`
	CreatedAt       string `json:"created_at"`
}

// SubjectDemandCount is one Subject's aggregate demand, for Admin.
type SubjectDemandCount struct {
	SubjectID       string `json:"subject_id"`
	InstitutionSlug string `json:"institution_slug"`
	// Both names are returned, never one resolved server-side by locale. Admin
	// reads this aggregate in either language, and a single localized string
	// would force the client either to re-request on a language switch or to
	// keep its own institution-name table -- and a client-side name map drifts
	// from the catalog the moment an Institution is renamed.
	InstitutionNameAr string `json:"institution_name_ar"`
	InstitutionNameEn string `json:"institution_name_en"`
	SubjectCode       string `json:"subject_code,omitempty"`
	SubjectTitleAr    string `json:"subject_title_ar"`
	SubjectTitleEn    string `json:"subject_title_en"`
	// Students is a count of distinct Students, which is what the live-unique
	// index makes true. It is not a count of clicks.
	Students int `json:"students"`
	// Served reports whether a published Course already teaches the Subject.
	// Demand against a served Subject is not a defect — it is how Gradex learns
	// the existing Course is not reaching the Students who want it.
	Served bool `json:"served"`
}

// RaiseSubjectDemand records one Student's demand for one Subject.
//
// institution_id is read from the Subject rather than accepted from the caller:
// the composite foreign key would refuse a mismatch anyway, and deriving it
// means no request shape can even express one.
func (r *Repository) RaiseSubjectDemand(
	ctx context.Context, accountID, subjectID, note string,
) (*SubjectDemandSignal, error) {
	if r == nil || r.pool == nil {
		return nil, ErrRepositoryNil
	}
	if err := validSubjectID(subjectID); err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > subjectDemandNoteLimit {
		return nil, ErrSubjectDemandNoteTooLong
	}
	var notePtr *string
	if note != "" {
		notePtr = &note
	}

	// A retired Subject is not offered anywhere, so demand cannot be raised
	// against one: the insert selects from subjects and finds no row.
	const insert = `
		INSERT INTO subject_demand_signals (account_id, subject_id, institution_id, note)
		SELECT $1::uuid, s.id, s.institution_id, $3
		FROM subjects s
		JOIN institutions i ON i.id = s.institution_id AND i.retired_at IS NULL
		WHERE s.id = $2::uuid AND s.retired_at IS NULL
		RETURNING id::text, subject_id::text, institution_id::text, COALESCE(note, ''), created_at::text`

	var signal SubjectDemandSignal
	err := r.pool.QueryRow(ctx, insert, accountID, subjectID, notePtr).Scan(
		&signal.ID, &signal.SubjectID, &signal.InstitutionID, &signal.Note, &signal.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No row inserted means the Subject does not exist, is retired, or
			// belongs to a retired Institution. All three are "not found" to a
			// caller; distinguishing them would leak catalog state.
			return nil, ErrNotFound
		}
		if pgErr := pgErrorOf(err); pgErr != nil && pgErr.Code == "23505" &&
			pgErr.ConstraintName == "subject_demand_signals_live_unique" {
			return nil, ErrSubjectDemandAlreadyRaised
		}
		return nil, fmt.Errorf("raising subject demand: %w", err)
	}
	return &signal, nil
}

// WithdrawSubjectDemand retires a Student's own live signal.
//
// Scoped to the account on purpose: there is no shape of this call that
// withdraws another Student's signal. The row is marked withdrawn rather than
// deleted so the history survives a Student changing their mind.
func (r *Repository) WithdrawSubjectDemand(ctx context.Context, accountID, subjectID string) error {
	if r == nil || r.pool == nil {
		return ErrRepositoryNil
	}
	if err := validSubjectID(subjectID); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE subject_demand_signals SET withdrawn_at = now()
		WHERE account_id = $1::uuid AND subject_id = $2::uuid AND withdrawn_at IS NULL`,
		accountID, subjectID)
	if err != nil {
		return fmt.Errorf("withdrawing subject demand: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSubjectDemandNotFound
	}
	return nil
}

// ListOwnSubjectDemand returns the Student's own live signals.
func (r *Repository) ListOwnSubjectDemand(
	ctx context.Context, accountID string,
) ([]SubjectDemandSignal, error) {
	if r == nil || r.pool == nil {
		return nil, ErrRepositoryNil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.subject_id::text, d.institution_id::text, i.slug,
			COALESCE(s.official_code, ''), s.title_ar, s.title_en,
			COALESCE(d.note, ''), d.created_at::text
		FROM subject_demand_signals d
		JOIN subjects s ON s.id = d.subject_id
		JOIN institutions i ON i.id = d.institution_id
		WHERE d.account_id = $1::uuid AND d.withdrawn_at IS NULL
		ORDER BY d.created_at DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("listing own subject demand: %w", err)
	}
	defer rows.Close()
	signals := []SubjectDemandSignal{}
	for rows.Next() {
		var signal SubjectDemandSignal
		if err := rows.Scan(&signal.ID, &signal.SubjectID, &signal.InstitutionID,
			&signal.InstitutionSlug, &signal.SubjectCode, &signal.SubjectTitleAr,
			&signal.SubjectTitleEn, &signal.Note, &signal.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning own subject demand: %w", err)
		}
		signals = append(signals, signal)
	}
	return signals, rows.Err()
}

// ListSubjectDemandCounts returns aggregate demand for Admin, highest first.
//
// The served flag composes catalogpublic.PublishedOnly rather than restating the
// lifecycle condition. Publication is one rule with one owner: a second copy
// here would keep answering the old question the day that rule changes, and
// Admin would prioritise production against a definition of "already taught"
// that the catalogue no longer uses.
//
// Individual Students are deliberately not returned here. Admin needs to know
// what to build; a per-Student roster is a different question with a different
// privacy weight, and nothing in the prioritisation workflow needs one.
func (r *Repository) ListSubjectDemandCounts(
	ctx context.Context, institutionSlug string, limit int,
) ([]SubjectDemandCount, error) {
	if r == nil || r.pool == nil {
		return nil, ErrRepositoryNil
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	arguments := []any{limit}
	filter := ""
	if slug := strings.TrimSpace(institutionSlug); slug != "" {
		arguments = append(arguments, slug)
		filter = " AND i.slug = $2"
	}

	rows, err := r.pool.Query(ctx, `
		SELECT d.subject_id::text, i.slug, i.name_ar, i.name_en,
			COALESCE(s.official_code, ''), s.title_ar, s.title_en,
			count(*)::int,
			EXISTS (
				SELECT 1 FROM courses c
				JOIN course_revisions cr ON cr.course_id = c.id
				WHERE c.subject_id = s.id AND `+catalogpublic.PublishedOnly("c", "cr")+`
			)
		FROM subject_demand_signals d
		JOIN subjects s ON s.id = d.subject_id
		JOIN institutions i ON i.id = d.institution_id
		WHERE d.withdrawn_at IS NULL`+filter+`
		GROUP BY d.subject_id, i.slug, i.name_ar, i.name_en, s.id, s.official_code, s.title_ar, s.title_en
		ORDER BY count(*) DESC, s.title_en ASC
		LIMIT $1`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("listing subject demand counts: %w", err)
	}
	defer rows.Close()
	counts := []SubjectDemandCount{}
	for rows.Next() {
		var count SubjectDemandCount
		if err := rows.Scan(&count.SubjectID, &count.InstitutionSlug,
			&count.InstitutionNameAr, &count.InstitutionNameEn,
			&count.SubjectCode, &count.SubjectTitleAr, &count.SubjectTitleEn,
			&count.Students, &count.Served); err != nil {
			return nil, fmt.Errorf("scanning subject demand count: %w", err)
		}
		counts = append(counts, count)
	}
	return counts, rows.Err()
}
