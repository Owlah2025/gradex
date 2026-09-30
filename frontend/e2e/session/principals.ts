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

export const V2_STUDENT: SeededPrincipal = {
  email: "v2-student@example.test",
  accountID: "a7000000-0000-0000-0000-000000000001",
};

export const V2_ADMIN_TARGET: SeededPrincipal = {
  email: "v2-admin-target@example.test",
  accountID: "a7000000-0000-0000-0000-000000000002",
};

export const V2_GRANT_TARGET: SeededPrincipal = {
  email: "v2-grant-target@example.test",
  accountID: "a7000000-0000-0000-0000-000000000003",
};

export const V2_INSTRUCTOR: SeededPrincipal = {
  email: "v2-instructor@example.test",
  accountID: "a7000000-0000-0000-0000-000000000004",
};
