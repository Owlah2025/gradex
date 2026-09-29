import assert from "node:assert/strict";
import test from "node:test";
import { ProblemError } from "./problem";
import {
  accessReasonMessage,
  auditActionLabel,
  auditModuleLabel,
  buildAdminAccountQuery,
  buildAdminAuditQuery,
  provenanceLabel,
  isRecentAuthRequired,
} from "./admin-operations";

test("account directory query builder keeps filters explicit and stable", () => {
  const query = new URLSearchParams(buildAdminAccountQuery({
    q: " alice@example.com ",
    role: "STUDENT",
    status: "ACTIVE",
    institutionId: "institution-1",
    joinedFrom: "2026-09-01",
    joinedTo: "2026-09-29",
    page: 2,
    limit: 25,
  }));
  assert.equal(query.get("q"), "alice@example.com");
  assert.equal(query.get("role"), "STUDENT");
  assert.equal(query.get("status"), "ACTIVE");
  assert.equal(query.get("institutionId"), "institution-1");
  assert.match(query.get("joinedFrom") ?? "", /^2026-09-01T00:00:00[+-]\d{2}:\d{2}$/);
  assert.match(query.get("joinedTo") ?? "", /^2026-09-30T00:00:00[+-]\d{2}:\d{2}$/);
  assert.equal(query.get("page"), "2");
  assert.equal(query.get("limit"), "25");
});

test("empty filters do not leak empty query parameters", () => {
  assert.equal(buildAdminAccountQuery({}), "");
  assert.equal(buildAdminAuditQuery({ actor: "  " }), "");
});

test("audit query builder supports human actor and server-side target filters", () => {
  const query = new URLSearchParams(buildAdminAuditQuery({
    actor: "Admin User",
    targetType: "ACCOUNT",
    targetId: "account-1",
    action: "ADMIN_USER_VIEWED",
    module: "IDENTITY_AND_ACCESS",
    from: "2026-09-01",
    to: "2026-09-29",
    asOf: "2026-09-29T12:00:00Z",
    page: 1,
    limit: 20,
  }));
  assert.equal(query.get("actor"), "Admin User");
  assert.equal(query.get("targetType"), "ACCOUNT");
  assert.equal(query.get("targetId"), "account-1");
  assert.equal(query.get("action"), "ADMIN_USER_VIEWED");
  assert.equal(query.get("module"), "IDENTITY_AND_ACCESS");
  assert.match(query.get("from") ?? "", /^2026-09-01T00:00:00[+-]\d{2}:\d{2}$/);
  assert.match(query.get("to") ?? "", /^2026-09-30T00:00:00[+-]\d{2}:\d{2}$/);
  assert.equal(query.get("asOf"), "2026-09-29T12:00:00Z");
  assert.equal(query.get("page"), "1");
  assert.equal(query.get("limit"), "20");
});

test("known audit labels localize while unknown codes remain visible", () => {
  const labels = {
    actions: { ADMIN_USER_VIEWED: "Viewed an account" },
    modules: { IDENTITY_AND_ACCESS: "Identity & access" },
    unknownTarget: "Unknown target",
  };
  assert.equal(auditActionLabel("ADMIN_USER_VIEWED", labels), "Viewed an account");
  assert.equal(auditActionLabel("FUTURE_ACTION", labels), "FUTURE_ACTION");
  assert.equal(auditModuleLabel("IDENTITY_AND_ACCESS", labels), "Identity & access");
  assert.equal(auditModuleLabel("FUTURE_MODULE", labels), "FUTURE_MODULE");
});

test("recent-auth problems prompt a fresh sign-in", () => {
  const error = new ProblemError({
    type: "https://gradex.example/problems/recent-authentication-required",
    title: "Recent authentication required",
    status: 403,
    code: "RECENT_AUTHENTICATION_REQUIRED",
  });
  assert.equal(isRecentAuthRequired(error), true);
});

test("diagnostic reasons and provenance use localized labels with safe fallbacks", () => {
  const reasons = { ACTIVE: "Access is active", UNKNOWN: "Review the account facts" };
  const provenance = { MANUAL_INVITATION: "Invitation", UNKNOWN: "Other source" };
  assert.equal(accessReasonMessage("ACTIVE", reasons), "Access is active");
  assert.equal(accessReasonMessage("FUTURE_REASON", reasons), "Review the account facts");
  assert.equal(provenanceLabel("MANUAL_INVITATION", provenance), "Invitation");
  assert.equal(provenanceLabel("FUTURE_SOURCE", provenance), "Other source");
});
