import type { ReadinessRequirement } from "./submission-readiness";
import type { AuthoringPlan } from "./authoring-plan";

export function pendingReadiness(plan: AuthoringPlan): ReadinessRequirement[] {
  return plan.sections.flatMap((section) => section.outstanding);
}

export function mediaGuidance(plan: AuthoringPlan): "ATTENTION" | "PROCESSING" | null {
  const states = plan.sections.map((section) => section.state);
  if (states.includes("ATTENTION")) return "ATTENTION";
  if (states.includes("PROCESSING")) return "PROCESSING";
  return null;
}
