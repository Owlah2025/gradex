import type { SessionResolution, SessionView } from "./session";

/**
 * Whether this browser may actually exercise Student subject-demand authority.
 *
 * # WHY `session !== null` IS NOT THIS QUESTION
 *
 * "Signed in" and "may register demand" are different facts, and treating the
 * first as the second renders an actionable control to principals the server
 * will refuse. An Instructor and an Admin are both signed in and neither holds
 * CapLearningAccess; a Student mid password-change holds nothing but the change
 * itself; a historical pending device session remains restricted until a fresh
 * password login. Each of those would press a button and receive a 403.
 *
 * # THIS MIRRORS THE SERVER, IT DOES NOT REPLACE IT
 *
 * The backend remains authoritative: `/me/subject-demand` sits behind
 * `CapLearningAccess`, which `Authorize` grants to RoleStudent alone, and
 * `AuthorizeSessionDevice` then narrows by device trust. Nothing here can grant
 * anything. The only job is to avoid offering an action that will be refused,
 * so this predicate is deliberately no *wider* than the server's rule. Where the
 * two could disagree it errs toward not offering the control.
 *
 * # ANONYMOUS IS NOT "NOT ELIGIBLE"
 *
 * A visitor with no session is a different case entirely and is handled by the
 * caller, not here: they see the Request call to action and are routed through
 * the sign-in journey carrying their intent. `null` in means "no session", for
 * which this returns false and the caller renders the auth-return affordance
 * rather than hiding the control.
 */
export function canRegisterSubjectDemand(session: SessionView | null): boolean {
  if (session === null) return false;

  // Only a Student holds CapLearningAccess. Authorize() denies it to every
  // other role rather than scoping it, so an Instructor or Admin is refused
  // outright regardless of anything else about the session.
  if (session.role !== "STUDENT") return false;

  // A restricted principal is refused every capability except changing its
  // password and signing out.
  if (session.password_change_required) return false;

  // Device policy is Student-only and narrows, never widens. TRUSTED is the
  // ordinary state; NOT_APPLICABLE covers a session the policy does not govern.
  // PENDING_DEVICE_TRUST is restricted to device self-service, and
  // LEGACY_UNBOUND is denied CapLearningAccess specifically — which is exactly
  // this route. A missing field means an older server that predates the policy,
  // which `deviceTrustState()` already reads as not applicable.
  const trust = session.device_trust?.state ?? "NOT_APPLICABLE";
  return trust === "TRUSTED" || trust === "NOT_APPLICABLE";
}

/**
 * What the demand control should render for this visitor.
 *
 * Four outcomes rather than a boolean, because each middle case is a real state
 * with its own correct affordance and collapsing any of them into a neighbour
 * is wrong:
 *
 *   * UNRESOLVED — the session question has not been answered yet. Distinct
 *     from ANONYMOUS because `SessionView` is `null` for both, and treating it
 *     as anonymous flashes "Sign in to request" at a Student who is already
 *     signed in, every time the page loads.
 *   * ANONYMOUS — no session. Still invited to act, and routed through the
 *     sign-in journey carrying their intent.
 *   * ELIGIBLE_STUDENT — holds the authority the server will accept.
 *   * INELIGIBLE — signed in and will be refused. Offering an action that
 *     cannot succeed is the defect this distinction exists to prevent.
 */
export type SubjectDemandAudience =
  | "UNRESOLVED"
  | "ANONYMOUS"
  | "ELIGIBLE_STUDENT"
  | "INELIGIBLE";

export function subjectDemandAudience(
  session: SessionView | null,
  resolution: SessionResolution,
): SubjectDemandAudience {
  if (resolution === "UNRESOLVED") return "UNRESOLVED";
  if (session === null) return "ANONYMOUS";
  return canRegisterSubjectDemand(session) ? "ELIGIBLE_STUDENT" : "INELIGIBLE";
}
