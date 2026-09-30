export type LearningCourseFilter = "in-progress" | "completed" | "all";

export type LearningCourseFilterCard = {
  completed: boolean;
  learningStatus: "active" | "expired";
};

export function initialLearningCourseFilter(
  cards: readonly LearningCourseFilterCard[],
): LearningCourseFilter {
  if (cards.some((card) => card.learningStatus === "active" && !card.completed)) {
    return "in-progress";
  }
  if (cards.some((card) => card.completed)) return "completed";
  return "all";
}

export function matchesLearningCourseFilter(
  card: LearningCourseFilterCard,
  filter: LearningCourseFilter,
): boolean {
  if (filter === "all") return true;
  if (filter === "completed") return card.completed;
  return card.learningStatus === "active" && !card.completed;
}
