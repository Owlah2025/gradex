# D5 Mutation Report

Date: 2026-07-28
Scope: S2 D5, T037 only
Rule: each mutation was applied independently to the working tree, its named real-PostgreSQL test
was run, the expected failure was captured, and the mutation was restored before the next run.

## Results

| Mutation | Failing proof | Observed failure | What it proves | What it does not prove |
|---|---|---|---|---|
| Move the Course pointer/lifecycle write after the approval transaction and inject failure before that auto-commit write | `TestD5ApprovalRollbackIsAtomicAfterEveryLoadBearingStage/after_pointer` | The old pointer remained while the old revision was `SUPERSEDED`, the candidate was `APPROVED`, and approval audit/outbox evidence committed. | The pointer is load-bearing and must commit atomically with both revision states and approval evidence. | It does not prove candidate uniqueness, graph read consistency, dependency revalidation, or rejection safety. |
| Remove partial unique index `course_revisions_one_active_candidate` | `TestD5ActiveCandidateUniqueIndexRejectsDirectConcurrentInserts` | Both direct concurrent `DRAFT` inserts succeeded; expected one success and one SQLSTATE `23505`. | The database invariant independently prevents duplicate active candidates without relying on the Course lock or repository pre-check. | It does not prove `CreateCandidate` cloning, idempotent Course-lock serialization, or approval behavior. |
| Make the Section child loader select the greatest `revision_number` instead of the captured revision ID | `TestD5LiveReadersObserveCompleteOldOrNewGraph` | A reader returned old Course/taxonomy/preview fields combined with new Section/Lesson/video/file fields. | Every child query must use the one captured `live_revision_id`; the test detects a mixed generation during approval. | It does not prove approval rollback, candidate uniqueness, or future S3/S5 routes consume this repository seam. |
| Bypass the shared approval-time `ValidateCourseForSubmission` call | `TestD5ApprovalRevalidatesEveryDependencyClass/completeness` | Approval succeeded after the submitted candidate graph was deleted; expected `SubmissionValidationError`. | Approval cannot rely on submission-time validation and must call the shared transaction-bound revalidator. The restored full matrix covers completeness, owner, taxonomy, video, preview, Resource, and Lab Material. | The completeness subcase alone does not prove each dependency subcase; that comes from the restored matrix, not this single mutant run. |
| Supersede the committed live revision inside Published-candidate rejection | `TestD5PublishedCandidateRejectionPreservesLiveStateAndAccess` | Rejection left the live revision `SUPERSEDED`; expected it to remain `APPROVED`. | Rejection is candidate-only and the snapshot test detects changes to live lifecycle/pointer/graph/access evidence. | It does not prove approval serialization or the first-publication `CHANGES_REQUESTED` path. |
| Allocate new Section and Lesson stable identities during candidate cloning | `TestD5CandidateCloneIsDeepAtomicAndIdentityStable` | Candidate authored-graph fingerprint differed only in stable Section/Lesson identities. | Unchanged logical Sections/Lessons retain identity across revisions while version-row and Lesson-file row IDs are newly allocated. | It does not prove the external-resource clone boundary by itself; the restored test's external row-count assertions provide that separate proof. |

## Restored-state gate

After all six mutations were restored:

```text
go test -tags=integration ./internal/catalog -run 'TestD5' -count=1
go test -race -tags=integration ./internal/catalog -run '^TestD5ExactFourRaces$' -count=1
```

Both commands must be rerun green on the frozen implementation range before review.
