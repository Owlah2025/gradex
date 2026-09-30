export type SeededPrincipal = {
  email: string;
  accountID: string;
};

export const ADMIN: SeededPrincipal = {
  email: "admin@example.test",
  accountID: "a0000000-0000-0000-0000-000000000000",
};

export const INSTRUCTOR: SeededPrincipal = {
  email: "instructor@example.test",
  accountID: "a0000000-0000-0000-0000-000000000003",
};
