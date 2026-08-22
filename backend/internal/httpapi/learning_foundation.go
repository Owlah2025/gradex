package httpapi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// requiredLearningPolicyEndpoints must list every endpoint policy the learning
// surface depends on. A build that forgets to wire one fails foundation validation
// rather than serving that endpoint unthrottled -- which is how FR-017's playback
// half went unenforced until Tier 3 review found it (D-071).
var requiredLearningPolicyEndpoints = [...]string{
	"learning-progress-source", "learning-progress", "learning-report",
	"learning-playback-source", "learning-playback",
}

type learningEvaluator interface {
	Evaluate(context.Context, string, string, time.Time) entitlement.Decision
	EvaluateInTransaction(context.Context, pgx.Tx, string, string, time.Time) entitlement.Decision
}

type learningReadEvaluator interface {
	EvaluateRead(context.Context, string, string, time.Time) entitlement.ReadDecision
	EvaluateCourseReads(context.Context, string, time.Time) (map[string]entitlement.ReadDecision, error)
}

type learningRepository interface {
	EnrollmentForLesson(context.Context, string, string) (learning.Enrollment, error)
	LessonVideoVersion(context.Context, string) (string, error)
	SaveProgressGuarded(context.Context, learning.ProgressWrite, learning.ProgressMutationGuard) error
	// CreateReportGuarded is the only report-creation entry point the route can reach. The
	// unguarded domain form is deliberately absent from this interface: a report must never be
	// written without the current-access decision that FR-033 requires, in the same transaction.
	CreateReportGuarded(context.Context, learning.VerifiedReportBinding, learning.ReportContent, learning.ReportClock, learning.ReportMutationGuard) (learning.Report, error)
}

type learningReadRepository interface {
	ReadCourseGraph(context.Context, string) (learning.CourseGraph, error)
	ReadCourseProgress(context.Context, string, string, learning.CourseGraph) (map[string]learning.Progress, learning.CourseProgressSummary, error)
	ReadLessonProgress(context.Context, string, string) (learning.Progress, error)
	ListStudentCourseSummaries(context.Context, string) ([]learning.StudentCourseSummary, error)
	ListStudentResumeCandidates(context.Context, string) ([]learning.ResumeCandidate, error)
	EnrollmentID(context.Context, string, string) (string, error)
}

type learningMedia interface {
	IssuePlayback(context.Context, media.PlaybackRequest) (media.PlaybackAuthorization, error)
	TrustedVideoDuration(context.Context, string, string) (time.Duration, error)
	MaterialKinds(context.Context, []string) (map[string][]media.Material, error)
}

// reportContextIssuer mints and verifies the encrypted report context that binds a report to the
// exact content instance a response rendered (D-065). It is mandatory for protected learning: a
// read that cannot produce a context would render content the Student is unable to report
// accurately, so there is no no-op implementation and no permissive fallback.
//
// Minting and verification are one dependency on purpose. Two separately configured halves could
// be keyed differently, and a verifier that cannot open what this server minted is a silent outage
// of the reporting route; the accepted signer provides both under one derived key.
type reportContextIssuer interface {
	Mint(learning.ReportContextRequest) (string, error)
	Verify(token, expectedReporter, expectedSession string) (learning.VerifiedReportBinding, error)
}

// LearningReportContextIssuer is the composition-root-facing alias.
type LearningReportContextIssuer = reportContextIssuer

// LearningMedia is the narrow S4 media boundary consumed by protected
// learning. It exposes neither a signer nor storage details.
type LearningMedia = learningMedia

type LearningFoundation struct {
	repository     learningRepository
	readRepository learningReadRepository
	evaluator      learningEvaluator
	readEvaluator  learningReadEvaluator
	media          learningMedia
	reportContexts reportContextIssuer
	limiter        *ratelimit.Limiter
	policies       map[string]ratelimit.Policy
	now            func() time.Time

	// beforeProgressMutation is an unexported deterministic integration-test
	// seam. Production composition leaves it nil; it makes the authorization to
	// mutation boundary observable without changing request behavior.
	beforeProgressMutation func(context.Context)
}

type LearningFoundationOptions struct {
	Repository learningRepository
	Evaluator  learningEvaluator
	Media      learningMedia
	// ReportContexts mints exact-visible report contexts. Required; see reportContextIssuer.
	ReportContexts reportContextIssuer
	Limiter        *ratelimit.Limiter
	Policies       map[string]ratelimit.Policy
	// Now supplies the server-authoritative clock used for each request-time
	// evaluation. Production uses UTC time.Now; tests inject a fixed UTC clock
	// to exercise the expiry boundary without sleeping.
	Now func() time.Time
}

func NewLearningFoundation(options LearningFoundationOptions) (*LearningFoundation, error) {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	foundation := &LearningFoundation{
		repository:     options.Repository,
		readRepository: readRepository(options.Repository),
		evaluator:      options.Evaluator,
		readEvaluator:  readEvaluator(options.Evaluator),
		media:          options.Media,
		reportContexts: options.ReportContexts,
		limiter:        options.Limiter,
		policies:       cloneLearningPolicies(options.Policies),
		now:            now,
	}
	if err := foundation.validate(); err != nil {
		return nil, err
	}
	return foundation, nil
}

func (f *LearningFoundation) validate() error {
	if f == nil || missingLearningDependency(f.repository) {
		return errors.New("learning repository is required")
	}
	if f.readRepository == nil {
		return errors.New("learning read repository is required")
	}
	if missingLearningDependency(f.evaluator) {
		return errors.New("learning entitlement evaluator is required")
	}
	if f.readEvaluator == nil {
		return errors.New("learning read evaluator is required")
	}
	if missingLearningDependency(f.media) {
		return errors.New("learning media delivery is required")
	}
	if missingLearningDependency(f.reportContexts) {
		return errors.New("learning report context issuer is required")
	}
	if f.limiter == nil {
		return errors.New("learning rate limiter is required")
	}
	if f.now == nil {
		return errors.New("learning clock is required")
	}
	for _, endpoint := range requiredLearningPolicyEndpoints {
		policy, ok := f.policies[endpoint]
		if !ok || policy.Endpoint != endpoint {
			return fmt.Errorf("required learning endpoint policy %q is missing", endpoint)
		}
		if err := policy.Validate(); err != nil {
			return fmt.Errorf("learning endpoint policy %q: %w", endpoint, err)
		}
	}
	return nil
}

func readRepository(repository learningRepository) learningReadRepository {
	read, _ := repository.(learningReadRepository)
	return read
}

func readEvaluator(evaluator learningEvaluator) learningReadEvaluator {
	read, _ := evaluator.(learningReadEvaluator)
	return read
}

func missingLearningDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	return value.Kind() == reflect.Ptr && value.IsNil()
}

func cloneLearningPolicies(policies map[string]ratelimit.Policy) map[string]ratelimit.Policy {
	cloned := make(map[string]ratelimit.Policy, len(policies))
	for endpoint, policy := range policies {
		cloned[endpoint] = policy
	}
	return cloned
}

func WithLearningFoundation(foundation *LearningFoundation) RouterOption {
	return func(options *routerOptions) error {
		if err := foundation.validate(); err != nil {
			return fmt.Errorf("learning foundation: %w", err)
		}
		if options.learning != nil {
			return fmt.Errorf("learning foundation is already configured")
		}
		options.learning = foundation
		return nil
	}
}
