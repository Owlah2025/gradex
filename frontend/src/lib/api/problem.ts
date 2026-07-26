export type ProblemViolation = {
  code: string;
  detail?: string;
  location?: string;
  pointer?: string;
  parameter?: string;
};

export type ApiProblem = {
  type: string;
  title: string;
  status: number;
  detail?: string;
  code: string;
  request_id?: string;
  errors?: ProblemViolation[];
};

export class ProblemError extends Error {
  constructor(public readonly problem: ApiProblem) {
    super(problem.detail ?? problem.title);
    this.name = "ProblemError";
  }
}

export function isProblem(value: unknown): value is ApiProblem {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.type === "string" &&
    typeof candidate.title === "string" &&
    typeof candidate.status === "number" &&
    typeof candidate.code === "string"
  );
}
