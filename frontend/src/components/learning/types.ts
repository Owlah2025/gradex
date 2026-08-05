export type LearningLocale = "ar" | "en";

export type LearningProgress = {
  max_position_seconds: number;
  last_position_seconds: number;
  completed: boolean;
};

export type LearningLesson = {
  id: string;
  title_ar: string;
  title_en: string;
  progress?: LearningProgress;
};
