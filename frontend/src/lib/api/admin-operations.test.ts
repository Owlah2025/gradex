import assert from "node:assert/strict";
import test from "node:test";
import {
  auditActionLabel,
  auditModuleLabel,
  buildAdminAccountQuery,
  buildAdminAuditQuery,
} from "./admin-operations";

test("account directory query builder keeps filters explicit and stable", () => {
  assert.equal(
    buildAdminAccountQuery({
      q: " alice@example.com ",
      role: "STUDENT",
      status: "ACTIVE",
      institutionId: "institution-1",
      joinedFrom: "2026-09-01",
      joinedTo: "2026-09-29",
      page: 2,
      limit: 25,
    }),
    "q=alice%40example.com&role=STUDENT&status=ACTIVE&institutionId=institution-1&joinedFrom=2026-09-01&joinedTo=2026-09-29&page=2&limit=25",
  );
});

test("empty filters do not leak empty query parameters", () => {
  assert.equal(buildAdminAccountQuery({}), "");
  assert.equal(buildAdminAuditQuery({ actor: "  " }), "");
});

test("audit query builder supports human actor and server-side target filters", () => {
  assert.equal(
    buildAdminAuditQuery({
      actor: "Admin User",
      targetType: "ACCOUNT",
      targetId: "account-1",
      action: "ADMIN_USER_VIEWED",
      module: "IDENTITY_AND_ACCESS",
      from: "2026-09-01",
      to: "2026-09-29",
      page: 1,
      limit: 20,
    }),
    "actor=Admin+User&targetType=ACCOUNT&targetId=account-1&action=ADMIN_USER_VIEWED&module=IDENTITY_AND_ACCESS&from=2026-09-01&to=2026-09-29&page=1&limit=20",
  );
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
