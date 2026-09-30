package instructorprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const profileColumns = `p.account_id::text, a.display_name, p.public_slug,
	p.headline_ar, p.headline_en, p.bio_ar, p.bio_en,
	p.avatar_asset_version_id::text, p.publication_state::text,
	p.published_snapshot, p.submitted_at, p.decided_at,
	p.decided_by::text, p.decision_note, p.revision,
	p.created_at, p.updated_at`

type Repository struct {
	pool *pgxpool.Pool
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type decisionKind string

const (
	decisionApprove       decisionKind = "APPROVE"
	decisionRequestChange decisionKind = "REQUEST_CHANGES"
	decisionHide          decisionKind = "HIDE"
)

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, ErrRepositoryNil
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) GetOwn(ctx context.Context, accountID string) (*Profile, error) {
	displayName, err := r.instructorAccount(ctx, r.pool, accountID)
	if err != nil {
		return nil, err
	}
	profile, err := loadProfile(ctx, r.pool, accountID)
	if errors.Is(err, ErrProfileNotFound) {
		return draftProfile(accountID, displayName), nil
	}
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (r *Repository) GetAdmin(ctx context.Context, accountID string) (*Profile, error) {
	if _, err := r.instructorAccount(ctx, r.pool, accountID); err != nil {
		return nil, err
	}
	return loadProfile(ctx, r.pool, accountID)
}

func (r *Repository) SaveDraft(ctx context.Context, request SaveDraftRequest) (*Profile, error) {
	normalized, err := normalizeDraft(request)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning instructor profile draft update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	displayName, err := r.instructorAccount(ctx, tx, request.AccountID)
	if err != nil {
		return nil, err
	}
	current, err := loadProfile(ctx, tx, request.AccountID)
	isNew := errors.Is(err, ErrProfileNotFound)
	if isNew {
		if request.ExpectedRevision != 0 {
			return nil, ErrRevisionConflict
		}
	} else if err != nil {
		return nil, err
	} else {
		if current.PublicationState == StatePendingReview {
			return nil, ErrInvalidTransition
		}
		if current.Revision != request.ExpectedRevision {
			return nil, ErrRevisionConflict
		}
	}

	if normalized.PublicSlug != "" {
		if err := ensureSlugAvailable(ctx, tx, request.AccountID, normalized.PublicSlug); err != nil {
			return nil, err
		}
	}

	if isNew {
		if err := insertProfile(ctx, tx, request.AccountID, normalized); err != nil {
			return nil, mapWriteError(err)
		}
	} else {
		if err := updateDraft(ctx, tx, request.AccountID, normalized, current.Revision); err != nil {
			return nil, mapWriteError(err)
		}
	}

	if err := syncExpertise(ctx, tx, request.AccountID, normalized.ExpertiseIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing instructor profile draft update: %w", err)
	}
	updated, err := r.GetOwn(ctx, request.AccountID)
	if err != nil {
		return nil, err
	}
	if updated.DisplayName == "" {
		updated.DisplayName = displayName
	}
	return updated, nil
}

func (r *Repository) Submit(ctx context.Context, request SubmitRequest) (*Profile, error) {
	if strings.TrimSpace(request.AccountID) == "" || request.ExpectedRevision < 1 {
		return nil, ErrInvalidInput
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning instructor profile submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := r.instructorAccount(ctx, tx, request.AccountID); err != nil {
		return nil, err
	}
	profile, err := loadProfile(ctx, tx, request.AccountID)
	if err != nil {
		return nil, err
	}
	if profile.Revision != request.ExpectedRevision {
		return nil, ErrRevisionConflict
	}
	switch profile.PublicationState {
	case StateDraft, StateChangesRequested, StatePublished, StateHidden:
	default:
		return nil, ErrInvalidTransition
	}
	if err := validateSubmission(profile); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE instructor_profiles
		SET publication_state = 'PENDING_REVIEW',
			submitted_at = now(),
			decided_at = NULL,
			decided_by = NULL,
			revision = revision + 1,
			updated_at = now()
		WHERE account_id = $1::uuid AND revision = $2
	`, request.AccountID, request.ExpectedRevision)
	if err != nil {
		return nil, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrRevisionConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing instructor profile submission: %w", err)
	}
	return r.GetOwn(ctx, request.AccountID)
}

func (r *Repository) List(ctx context.Context, request ListRequest) (ListResult, error) {
	page := request.Page
	if page < 1 {
		page = 1
	}
	limit := request.Limit
	if limit < 1 || limit > 50 {
		limit = 25
	}
	state := strings.TrimSpace(string(request.State))
	if state != "" && !request.State.Valid() {
		return ListResult{}, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT p.account_id::text, a.display_name, p.public_slug,
			p.publication_state::text, p.submitted_at, p.updated_at, p.revision
		FROM instructor_profiles p
		JOIN accounts a ON a.id = p.account_id
		WHERE a.role = 'INSTRUCTOR'
		  AND ($1 = '' OR p.publication_state::text = $1)
		ORDER BY p.submitted_at DESC NULLS LAST, p.updated_at DESC, p.account_id
		LIMIT $2 OFFSET $3
	`, state, limit+1, (page-1)*limit)
	if err != nil {
		return ListResult{}, fmt.Errorf("listing instructor profiles: %w", err)
	}
	defer rows.Close()

	items := make([]AdminListItem, 0, limit)
	for rows.Next() {
		var item AdminListItem
		var stateText string
		if err := rows.Scan(
			&item.AccountID, &item.DisplayName, &item.PublicSlug,
			&stateText, &item.SubmittedAt, &item.UpdatedAt, &item.Revision,
		); err != nil {
			return ListResult{}, fmt.Errorf("scanning instructor profile queue: %w", err)
		}
		item.PublicationState = PublicationState(stateText)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("reading instructor profile queue: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return ListResult{Items: items, Page: page, Limit: limit, HasMore: hasMore}, nil
}

func (r *Repository) Approve(ctx context.Context, request DecisionRequest) (*Profile, error) {
	return r.decide(ctx, request, decisionApprove)
}

func (r *Repository) RequestChanges(ctx context.Context, request DecisionRequest) (*Profile, error) {
	return r.decide(ctx, request, decisionRequestChange)
}

func (r *Repository) Hide(ctx context.Context, request DecisionRequest) (*Profile, error) {
	return r.decide(ctx, request, decisionHide)
}

func (r *Repository) Published(ctx context.Context, slug string) (*PublicSnapshot, string, error) {
	if !validSlug(slug) {
		return nil, "", nil
	}
	var raw []byte
	var accountID string
	err := r.pool.QueryRow(ctx, `
		SELECT p.published_snapshot, p.account_id::text
		FROM instructor_profiles p
		JOIN accounts a ON a.id = p.account_id
		WHERE p.published_snapshot->>'public_slug' = $1
		  AND p.published_slug = $1
		  AND p.public_visible
		  AND a.role = 'INSTRUCTOR'
		  AND p.published_snapshot IS NOT NULL
	`, slug).Scan(&raw, &accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("reading published instructor profile: %w", err)
	}
	var snapshot PublicSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, "", fmt.Errorf("decoding published instructor profile: %w", err)
	}
	return &snapshot, accountID, nil
}

func (r *Repository) decide(
	ctx context.Context,
	request DecisionRequest,
	kind decisionKind,
) (*Profile, error) {
	if strings.TrimSpace(request.AccountID) == "" || strings.TrimSpace(request.AdminAccountID) == "" || request.ExpectedRevision < 1 {
		return nil, ErrInvalidInput
	}
	reason := strings.TrimSpace(request.Reason)
	if utf8.RuneCountInString(reason) == 0 || utf8.RuneCountInString(reason) > 4000 {
		return nil, ErrReasonRequired
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning instructor profile decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := requireAdmin(ctx, tx, request.AdminAccountID); err != nil {
		return nil, err
	}
	profile, err := loadProfileForUpdate(ctx, tx, request.AccountID)
	if err != nil {
		return nil, err
	}
	if profile.Revision != request.ExpectedRevision {
		return nil, ErrRevisionConflict
	}

	action, nextState, snapshot, err := prepareDecision(profile, reason, kind)
	if err != nil {
		return nil, err
	}
	if snapshot != nil {
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return nil, fmt.Errorf("encoding instructor profile snapshot: %w", err)
		}
		if err := ensureSlugAvailable(ctx, tx, request.AccountID, snapshot.PublicSlug); err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE instructor_profiles
			SET publication_state = $2::instructor_profile_publication_state,
				published_snapshot = $3::jsonb,
				published_slug = $4,
				public_visible = TRUE,
				decided_at = now(),
				decided_by = $5::uuid,
				decision_note = $6,
				revision = revision + 1,
				updated_at = now()
			WHERE account_id = $1::uuid AND revision = $7
		`, request.AccountID, string(nextState), encoded, snapshot.PublicSlug, request.AdminAccountID, reason, request.ExpectedRevision)
		if err != nil {
			return nil, mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return nil, ErrRevisionConflict
		}
	} else {
		tag, err := tx.Exec(ctx, `
			UPDATE instructor_profiles
			SET publication_state = $2::instructor_profile_publication_state,
				public_visible = CASE WHEN $2::instructor_profile_publication_state = 'HIDDEN' THEN FALSE ELSE public_visible END,
				decided_at = now(),
				decided_by = $3::uuid,
				decision_note = $4,
				revision = revision + 1,
				updated_at = now()
			WHERE account_id = $1::uuid AND revision = $5
		`, request.AccountID, string(nextState), request.AdminAccountID, reason, request.ExpectedRevision)
		if err != nil {
			return nil, mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return nil, ErrRevisionConflict
		}
	}

	adminAccountID := request.AdminAccountID
	if err := catalog.WriteAuditEvent(ctx, tx, catalog.AuditEvent{
		ActorAccountID:  &adminAccountID,
		ActorRole:       "ADMIN",
		ActorDescriptor: request.AdminAccountID,
		Action:          action,
		Module:          catalog.AuditModuleCatalog,
		TargetType:      "INSTRUCTOR_PROFILE",
		TargetID:        request.AccountID,
		Reason:          reason,
		Metadata: map[string]any{
			"previous_state": string(profile.PublicationState),
			"next_state":     string(nextState),
			"revision":       profile.Revision + 1,
		},
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing instructor profile decision: %w", err)
	}
	return r.GetAdmin(ctx, request.AccountID)
}

func validateSubmission(profile *Profile) error {
	if profile.PublicSlug == nil || !validSlug(*profile.PublicSlug) {
		return &SubmissionIncompleteError{Violations: []string{"public_slug"}}
	}
	if strings.TrimSpace(profile.HeadlineAr) == "" && strings.TrimSpace(profile.HeadlineEn) == "" {
		return &SubmissionIncompleteError{Violations: []string{"headline"}}
	}
	if strings.TrimSpace(profile.BioAr) == "" && strings.TrimSpace(profile.BioEn) == "" {
		return &SubmissionIncompleteError{Violations: []string{"bio"}}
	}
	return nil
}

func prepareDecision(
	profile *Profile,
	reason string,
	kind decisionKind,
) (string, PublicationState, *PublicSnapshot, error) {
	switch kind {
	case decisionApprove:
		if profile.PublicationState != StatePendingReview {
			return "", "", nil, ErrInvalidTransition
		}
		if err := validateSubmission(profile); err != nil {
			return "", "", nil, err
		}
		snapshot := snapshotFromProfile(profile)
		return "INSTRUCTOR_PROFILE_APPROVED", StatePublished, &snapshot, nil
	case decisionRequestChange:
		if profile.PublicationState != StatePendingReview {
			return "", "", nil, ErrInvalidTransition
		}
		return "INSTRUCTOR_PROFILE_CHANGES_REQUESTED", StateChangesRequested, nil, nil
	case decisionHide:
		if profile.PublicationState == StateHidden {
			return "", "", nil, ErrInvalidTransition
		}
		return "INSTRUCTOR_PROFILE_HIDDEN", StateHidden, nil, nil
	default:
		return "", "", nil, ErrInvalidTransition
	}
}

func snapshotFromProfile(profile *Profile) PublicSnapshot {
	slug := ""
	if profile.PublicSlug != nil {
		slug = *profile.PublicSlug
	}
	return PublicSnapshot{
		PublicSlug:  slug,
		DisplayName: profile.DisplayName,
		HeadlineAr:  profile.HeadlineAr,
		HeadlineEn:  profile.HeadlineEn,
		BioAr:       profile.BioAr,
		BioEn:       profile.BioEn,
		Expertise:   append([]ExpertiseItem{}, profile.Expertise...),
		AvatarURL:   profile.AvatarURL,
	}
}

func draftProfile(accountID, displayName string) *Profile {
	return &Profile{
		AccountID:        accountID,
		DisplayName:      displayName,
		PublicationState: StateDraft,
		Expertise:        []ExpertiseItem{},
	}
}

func normalizeDraft(request SaveDraftRequest) (SaveDraftRequest, error) {
	if strings.TrimSpace(request.AccountID) == "" || request.ExpectedRevision < 0 {
		return SaveDraftRequest{}, ErrInvalidInput
	}
	normalized := request
	normalized.PublicSlug = strings.TrimSpace(request.PublicSlug)
	normalized.HeadlineAr = strings.TrimSpace(request.HeadlineAr)
	normalized.HeadlineEn = strings.TrimSpace(request.HeadlineEn)
	normalized.BioAr = strings.TrimSpace(request.BioAr)
	normalized.BioEn = strings.TrimSpace(request.BioEn)
	if normalized.PublicSlug != "" && !validSlug(normalized.PublicSlug) {
		return SaveDraftRequest{}, ErrInvalidInput
	}
	if utf8.RuneCountInString(normalized.HeadlineAr) > 120 ||
		utf8.RuneCountInString(normalized.HeadlineEn) > 120 ||
		utf8.RuneCountInString(normalized.BioAr) > 4000 ||
		utf8.RuneCountInString(normalized.BioEn) > 4000 {
		return SaveDraftRequest{}, ErrInvalidInput
	}
	var err error
	normalized.ExpertiseIDs, err = normalizeExpertiseIDs(request.ExpertiseIDs)
	if err != nil {
		return SaveDraftRequest{}, err
	}
	if len(normalized.ExpertiseIDs) > 20 {
		return SaveDraftRequest{}, ErrInvalidInput
	}
	return normalized, nil
}

func normalizeExpertiseIDs(values []string) ([]string, error) {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, ErrInvalidInput
		}
		canonical := id.String()
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	return normalized, nil
}

func validSlug(value string) bool {
	return utf8.RuneCountInString(value) >= 3 &&
		utf8.RuneCountInString(value) <= 60 &&
		slugPattern.MatchString(value)
}

func insertProfile(ctx context.Context, tx pgx.Tx, accountID string, request SaveDraftRequest) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO instructor_profiles (
			account_id, public_slug, headline_ar, headline_en, bio_ar, bio_en
		) VALUES ($1::uuid, NULLIF($2, ''), $3, $4, $5, $6)
	`, accountID, request.PublicSlug, request.HeadlineAr, request.HeadlineEn, request.BioAr, request.BioEn)
	return err
}

func updateDraft(ctx context.Context, tx pgx.Tx, accountID string, request SaveDraftRequest, revision int) error {
	tag, err := tx.Exec(ctx, `
		UPDATE instructor_profiles
		SET public_slug = NULLIF($2, ''),
			headline_ar = $3,
			headline_en = $4,
			bio_ar = $5,
			bio_en = $6,
			revision = revision + 1,
			updated_at = now()
		WHERE account_id = $1::uuid AND revision = $7
	`, accountID, request.PublicSlug, request.HeadlineAr, request.HeadlineEn, request.BioAr, request.BioEn, revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func syncExpertise(ctx context.Context, tx pgx.Tx, accountID string, subjectIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM instructor_profile_expertise WHERE account_id = $1::uuid`, accountID); err != nil {
		return fmt.Errorf("clearing instructor expertise: %w", err)
	}
	for _, subjectID := range subjectIDs {
		tag, err := tx.Exec(ctx, `
			INSERT INTO instructor_profile_expertise (account_id, subject_id)
			SELECT $1::uuid, id
			FROM subjects
			WHERE id = $2::uuid AND retired_at IS NULL
		`, accountID, subjectID)
		if err != nil {
			return mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return ErrSubjectNotFound
		}
	}
	return nil
}

func loadProfile(ctx context.Context, q rowQuerier, accountID string) (*Profile, error) {
	row := q.QueryRow(ctx, `
		SELECT `+profileColumns+`
		FROM instructor_profiles p
		JOIN accounts a ON a.id = p.account_id
		WHERE p.account_id = $1::uuid AND a.role = 'INSTRUCTOR'
	`, accountID)
	profile, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("reading instructor profile: %w", err)
	}
	profile.Expertise, err = loadExpertise(ctx, q, accountID)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func loadProfileForUpdate(ctx context.Context, tx pgx.Tx, accountID string) (*Profile, error) {
	row := tx.QueryRow(ctx, `
		SELECT `+profileColumns+`
		FROM instructor_profiles p
		JOIN accounts a ON a.id = p.account_id
		WHERE p.account_id = $1::uuid AND a.role = 'INSTRUCTOR'
		FOR UPDATE OF p
	`, accountID)
	profile, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("locking instructor profile: %w", err)
	}
	profile.Expertise, err = loadExpertise(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func scanProfile(row pgx.Row) (*Profile, error) {
	var profile Profile
	var state string
	var avatarAsset *string
	var rawSnapshot []byte
	if err := row.Scan(
		&profile.AccountID, &profile.DisplayName, &profile.PublicSlug,
		&profile.HeadlineAr, &profile.HeadlineEn, &profile.BioAr, &profile.BioEn,
		&avatarAsset, &state, &rawSnapshot, &profile.SubmittedAt, &profile.DecidedAt,
		&profile.DecidedBy, &profile.DecisionNote, &profile.Revision,
		&profile.CreatedAt, &profile.UpdatedAt,
	); err != nil {
		return nil, err
	}
	profile.PublicationState = PublicationState(state)
	profile.Expertise = []ExpertiseItem{}
	if len(rawSnapshot) > 0 {
		var snapshot PublicSnapshot
		if err := json.Unmarshal(rawSnapshot, &snapshot); err != nil {
			return nil, fmt.Errorf("decoding instructor profile snapshot: %w", err)
		}
		profile.PublishedSnapshot = &snapshot
	}
	_ = avatarAsset
	return &profile, nil
}

func loadExpertise(ctx context.Context, q rowQuerier, accountID string) ([]ExpertiseItem, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id::text, s.official_code, s.title_ar, s.title_en
		FROM instructor_profile_expertise e
		JOIN subjects s ON s.id = e.subject_id
		WHERE e.account_id = $1::uuid
		ORDER BY s.title_en, s.id
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("loading instructor expertise: %w", err)
	}
	defer rows.Close()
	items := make([]ExpertiseItem, 0)
	for rows.Next() {
		var item ExpertiseItem
		if err := rows.Scan(&item.ID, &item.OfficialCode, &item.TitleAr, &item.TitleEn); err != nil {
			return nil, fmt.Errorf("scanning instructor expertise: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading instructor expertise: %w", err)
	}
	return items, nil
}

func (r *Repository) instructorAccount(ctx context.Context, q rowQuerier, accountID string) (string, error) {
	var displayName string
	err := q.QueryRow(ctx, `
		SELECT display_name
		FROM accounts
		WHERE id = $1::uuid AND role = 'INSTRUCTOR'
	`, accountID).Scan(&displayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotInstructor
	}
	if err != nil {
		return "", fmt.Errorf("reading instructor account: %w", err)
	}
	return displayName, nil
}

func requireAdmin(ctx context.Context, tx pgx.Tx, accountID string) error {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM accounts WHERE id = $1::uuid AND role = 'ADMIN'
		)
	`, accountID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking moderation admin: %w", err)
	}
	if !exists {
		return ErrInvalidInput
	}
	return nil
}

func ensureSlugAvailable(ctx context.Context, tx pgx.Tx, accountID, slug string) error {
	if slug == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, slug); err != nil {
		return fmt.Errorf("locking instructor profile slug: %w", err)
	}
	var taken bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM instructor_profiles
			WHERE (public_slug = $1 OR published_slug = $1)
			  AND account_id <> $2::uuid
		)
	`, slug, accountID).Scan(&taken); err != nil {
		return fmt.Errorf("checking instructor profile slug: %w", err)
	}
	if taken {
		return ErrSlugTaken
	}
	return nil
}

func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			switch {
			case strings.Contains(pgErr.ConstraintName, "slug"):
				return ErrSlugTaken
			case pgErr.ConstraintName == "instructor_profiles_pkey":
				return ErrRevisionConflict
			}
		}
	}
	return err
}
