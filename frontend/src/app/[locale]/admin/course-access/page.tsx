"use client";

import { useEffect, useState, useCallback } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  CourseAccessInvitation,
  AdminEntitlementDetail,
  createCourseAccessInvitation,
  listAdminCourseAccessInvitations,
  approveCourseAccessInvitation,
  rejectCourseAccessInvitation,
  cancelCourseAccessInvitation,
  resendCourseAccessInvitation,
  setCourseDefaultAccessExpiry,
  getAdminEntitlementDetail,
} from "@/lib/api/access";
import { ProblemError } from "@/lib/api/problem";

function getProblemErrorMessage(e: unknown, fallback: string): string {
  if (e instanceof ProblemError) {
    return e.problem.detail || e.problem.title || fallback;
  }
  if (e && typeof e === "object" && "message" in e && typeof e.message === "string") {
    return e.message;
  }
  return fallback;
}

export default function AdminCourseAccessPage() {
  const { locale } = useLocale();
  const [invitations, setInvitations] = useState<CourseAccessInvitation[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Form states: Expiry Configuration
  const [expiryCourseId, setExpiryCourseId] = useState<string>("");
  const [expiryDate, setExpiryDate] = useState<string>("");
  const [expiryReason, setExpiryReason] = useState<string>("");
  const [expirySubmitting, setExpirySubmitting] = useState<boolean>(false);

  // Form states: Create Invitation
  const [createCourseId, setCreateCourseId] = useState<string>("");
  const [createEmail, setCreateEmail] = useState<string>("");
  const [createNote, setCreateNote] = useState<string>("");
  const [createRef, setCreateRef] = useState<string>("");
  const [createSubmitting, setCreateSubmitting] = useState<boolean>(false);

  // Modal state: Reject Reason
  const [rejectingInvId, setRejectingInvId] = useState<string | null>(null);
  const [rejectReason, setRejectReason] = useState<string>("");
  const [rejectSubmitting, setRejectSubmitting] = useState<boolean>(false);

  // Modal state: Entitlement Detail
  const [detailModal, setDetailModal] = useState<AdminEntitlementDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState<boolean>(false);

  const fetchInvitations = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await listAdminCourseAccessInvitations(1, 100, locale);
      if (res && res.invitations) {
        setInvitations(res.invitations);
      }
    } catch (e: unknown) {
      setError(getProblemErrorMessage(e, "Failed to fetch invitations"));
    } finally {
      setLoading(false);
    }
  }, [locale]);

  useEffect(() => {
    fetchInvitations();
  }, [fetchInvitations]);

  const handleSetExpiry = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!expiryCourseId || !expiryDate || !expiryReason) return;
    setExpirySubmitting(true);
    setError(null);
    setSuccess(null);
    try {
      await setCourseDefaultAccessExpiry(expiryCourseId, expiryDate, expiryReason, locale);
      setSuccess(`Default access expiry configured for course ${expiryCourseId}`);
      setExpiryCourseId("");
      setExpiryDate("");
      setExpiryReason("");
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Failed to set default access expiry"));
    } finally {
      setExpirySubmitting(false);
    }
  };

  const handleCreateInvitation = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!createCourseId || !createEmail) return;
    setCreateSubmitting(true);
    setError(null);
    setSuccess(null);
    try {
      const created = await createCourseAccessInvitation(
        createCourseId,
        createEmail,
        createNote || undefined,
        createRef || undefined,
        locale,
      );
      setSuccess(`Course access invitation created for ${created?.email || createEmail}`);
      setCreateCourseId("");
      setCreateEmail("");
      setCreateNote("");
      setCreateRef("");
      fetchInvitations();
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Failed to create invitation"));
    } finally {
      setCreateSubmitting(false);
    }
  };

  const handleApprove = async (id: string) => {
    setError(null);
    setSuccess(null);
    try {
      const res = await approveCourseAccessInvitation(id, locale);
      setSuccess(`Invitation approved! Entitlement ID: ${res?.entitlement?.id}`);
      fetchInvitations();
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Approval failed"));
    }
  };

  const handleRejectSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!rejectingInvId || !rejectReason.trim()) return;
    setRejectSubmitting(true);
    setError(null);
    setSuccess(null);
    try {
      await rejectCourseAccessInvitation(rejectingInvId, rejectReason.trim(), locale);
      setSuccess(`Invitation ${rejectingInvId} rejected.`);
      setRejectingInvId(null);
      setRejectReason("");
      fetchInvitations();
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Rejection failed"));
    } finally {
      setRejectSubmitting(false);
    }
  };

  const handleCancel = async (id: string) => {
    setError(null);
    setSuccess(null);
    try {
      await cancelCourseAccessInvitation(id, locale);
      setSuccess(`Invitation ${id} cancelled.`);
      fetchInvitations();
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Cancellation failed"));
    }
  };

  const handleResend = async (id: string) => {
    setError(null);
    setSuccess(null);
    try {
      await resendCourseAccessInvitation(id, locale);
      setSuccess(`New acceptance link generated and queued for invitation ${id}.`);
      fetchInvitations();
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Resend failed"));
    }
  };

  const handleViewEntitlement = async (entitlementId: string) => {
    setDetailLoading(true);
    try {
      const detail = await getAdminEntitlementDetail(entitlementId, locale);
      if (detail) {
        setDetailModal(detail);
      }
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Failed to load entitlement details"));
    } finally {
      setDetailLoading(false);
    }
  };

  return (
    <main id="main" className="max-w-7xl mx-auto p-6 space-y-8">
      <div className="border-b pb-4">
        <h1 className="text-3xl font-bold tracking-tight text-gray-900">Course Access Management</h1>
        <p className="text-sm text-gray-600 mt-1">
          Configure course default access expiry, issue manual course invitations, approve pending grants, and manage entitlement records.
        </p>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-800 rounded-md p-4 text-sm" role="alert">
          <strong>Error:</strong> {error}
        </div>
      )}

      {success && (
        <div className="bg-green-50 border border-green-200 text-green-800 rounded-md p-4 text-sm" role="status">
          <strong>Success:</strong> {success}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 gap-8">
        {/* Course Default Access Expiry Config */}
        <section className="bg-white p-6 rounded-lg border shadow-sm">
          <h2 className="text-xl font-semibold mb-4 text-gray-800">1. Configure Course Access Expiry</h2>
          <form onSubmit={handleSetExpiry} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700">Course ID (UUID)</label>
              <input
                type="text"
                required
                value={expiryCourseId}
                onChange={(e) => setExpiryCourseId(e.target.value)}
                placeholder="20000000-0000-0000-0000-000000000001"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Default Access Expiry Date (YYYY-MM-DD)</label>
              <input
                type="date"
                required
                value={expiryDate}
                onChange={(e) => setExpiryDate(e.target.value)}
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Reason / Reference</label>
              <input
                type="text"
                required
                value={expiryReason}
                onChange={(e) => setExpiryReason(e.target.value)}
                placeholder="Standard cohort 30-day access grant"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <button
              type="submit"
              disabled={expirySubmitting}
              className="w-full bg-blue-600 hover:bg-blue-700 text-white font-medium py-2 px-4 rounded-md shadow-sm text-sm disabled:opacity-50"
            >
              {expirySubmitting ? "Saving..." : "Save Default Expiry"}
            </button>
          </form>
        </section>

        {/* Create Invitation */}
        <section className="bg-white p-6 rounded-lg border shadow-sm">
          <h2 className="text-xl font-semibold mb-4 text-gray-800">2. Issue Course Access Invitation</h2>
          <form onSubmit={handleCreateInvitation} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700">Course ID (UUID)</label>
              <input
                type="text"
                required
                value={createCourseId}
                onChange={(e) => setCreateCourseId(e.target.value)}
                placeholder="20000000-0000-0000-0000-000000000001"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Student Email Address</label>
              <input
                type="email"
                required
                value={createEmail}
                onChange={(e) => setCreateEmail(e.target.value)}
                placeholder="student@example.com"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Admin Internal Note (Optional)</label>
              <input
                type="text"
                value={createNote}
                onChange={(e) => setCreateNote(e.target.value)}
                placeholder="Approved via scholarship program"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">External Reference ID (Optional)</label>
              <input
                type="text"
                value={createRef}
                onChange={(e) => setCreateRef(e.target.value)}
                placeholder="SCHOLARSHIP-2026-08"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <button
              type="submit"
              disabled={createSubmitting}
              className="w-full bg-emerald-600 hover:bg-emerald-700 text-white font-medium py-2 px-4 rounded-md shadow-sm text-sm disabled:opacity-50"
            >
              {createSubmitting ? "Creating..." : "Create Invitation"}
            </button>
          </form>
        </section>
      </div>

      {/* Invitations Queue & Decision Center */}
      <section className="bg-white p-6 rounded-lg border shadow-sm">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-xl font-semibold text-gray-800">3. Invitation Queue & Decision Center</h2>
          <button
            onClick={fetchInvitations}
            disabled={loading}
            className="text-sm bg-gray-100 hover:bg-gray-200 text-gray-700 py-1 px-3 rounded-md border"
          >
            {loading ? "Refreshing..." : "Refresh Queue"}
          </button>
        </div>

        {loading ? (
          <p className="text-gray-500 text-sm py-8 text-center">Loading invitations queue...</p>
        ) : invitations.length === 0 ? (
          <p className="text-gray-500 text-sm py-8 text-center border rounded-md bg-gray-50">No invitations found.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm border-collapse">
              <thead>
                <tr className="bg-gray-100 border-b text-gray-700">
                  <th className="p-3">ID / Recipient</th>
                  <th className="p-3">Course ID</th>
                  <th className="p-3">State</th>
                  <th className="p-3">Timestamps</th>
                  <th className="p-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {invitations.map((inv) => (
                  <tr key={inv.id} className="hover:bg-gray-50">
                    <td className="p-3 font-mono text-xs">
                      <div className="font-semibold text-gray-900">{inv.email}</div>
                      <div className="text-gray-400">{inv.id}</div>
                    </td>
                    <td className="p-3 font-mono text-xs text-gray-600">{inv.course_id}</td>
                    <td className="p-3">
                      <span
                        className={`inline-block px-2 py-1 text-xs font-semibold rounded ${
                          inv.state === "APPROVED"
                            ? "bg-green-100 text-green-800"
                            : inv.state === "PENDING_ADMIN_APPROVAL"
                            ? "bg-amber-100 text-amber-800"
                            : inv.state === "PENDING_STUDENT_ACCEPTANCE"
                            ? "bg-blue-100 text-blue-800"
                            : inv.state === "REJECTED"
                            ? "bg-red-100 text-red-800"
                            : "bg-gray-100 text-gray-800"
                        }`}
                      >
                        {inv.state}
                      </span>
                      {inv.decision_reason && (
                        <div className="text-xs text-gray-500 mt-1">Reason: {inv.decision_reason}</div>
                      )}
                    </td>
                    <td className="p-3 text-xs text-gray-500 space-y-1">
                      <div>Created: {new Date(inv.created_at).toLocaleString()}</div>
                      {inv.accepted_at && <div>Accepted: {new Date(inv.accepted_at).toLocaleString()}</div>}
                      {inv.decided_at && <div>Decided: {new Date(inv.decided_at).toLocaleString()}</div>}
                    </td>
                    <td className="p-3 text-right space-x-2">
                      {inv.state === "PENDING_ADMIN_APPROVAL" && (
                        <>
                          <button
                            onClick={() => handleApprove(inv.id)}
                            className="bg-green-600 hover:bg-green-700 text-white px-3 py-1 text-xs font-semibold rounded"
                          >
                            Approve
                          </button>
                          <button
                            onClick={() => {
                              setRejectingInvId(inv.id);
                              setRejectReason("");
                            }}
                            className="bg-red-600 hover:bg-red-700 text-white px-3 py-1 text-xs font-semibold rounded"
                          >
                            Reject
                          </button>
                        </>
                      )}

                      {inv.state === "PENDING_STUDENT_ACCEPTANCE" && (
                        <>
                          <button
                            onClick={() => handleResend(inv.id)}
                            className="bg-blue-600 hover:bg-blue-700 text-white px-2 py-1 text-xs rounded"
                          >
                            Resend Link
                          </button>
                          <button
                            onClick={() => handleCancel(inv.id)}
                            className="bg-gray-600 hover:bg-gray-700 text-white px-2 py-1 text-xs rounded"
                          >
                            Cancel
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Reject Reason Modal */}
      {rejectingInvId && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className="bg-white rounded-lg p-6 max-w-md w-full shadow-xl">
            <h3 className="text-lg font-bold text-gray-900 mb-2">Reject Course Access Invitation</h3>
            <p className="text-sm text-gray-600 mb-4">
              Specify the reason for rejecting invitation <code className="bg-gray-100 p-1 rounded text-xs">{rejectingInvId}</code>.
            </p>
            <form onSubmit={handleRejectSubmit} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700">Rejection Reason</label>
                <textarea
                  required
                  rows={3}
                  value={rejectReason}
                  onChange={(e) => setRejectReason(e.target.value)}
                  placeholder="Ineligible academic status or duplicate request."
                  className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
                />
              </div>
              <div className="flex justify-end space-x-3">
                <button
                  type="button"
                  onClick={() => setRejectingInvId(null)}
                  className="bg-gray-200 hover:bg-gray-300 text-gray-800 px-4 py-2 text-sm rounded-md"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={rejectSubmitting}
                  className="bg-red-600 hover:bg-red-700 text-white px-4 py-2 text-sm font-medium rounded-md disabled:opacity-50"
                >
                  {rejectSubmitting ? "Rejecting..." : "Confirm Rejection"}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Entitlement Detail Modal */}
      {detailModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className="bg-white rounded-lg p-6 max-w-xl w-full shadow-xl space-y-4">
            <div className="flex justify-between items-center border-b pb-2">
              <h3 className="text-lg font-bold text-gray-900">Entitlement Record Detail</h3>
              <button
                onClick={() => setDetailModal(null)}
                className="text-gray-500 hover:text-gray-700 font-bold text-lg"
              >
                &times;
              </button>
            </div>
            <div className="space-y-2 text-sm font-mono bg-gray-50 p-4 rounded border">
              <div><strong>ID:</strong> {detailModal.entitlement.id}</div>
              <div><strong>Student ID:</strong> {detailModal.entitlement.student_account_id}</div>
              <div><strong>Course ID:</strong> {detailModal.entitlement.course_id}</div>
              <div><strong>State:</strong> <span className="font-bold text-green-700">{detailModal.entitlement.state}</span></div>
              <div><strong>Grant Source:</strong> {detailModal.entitlement.grant_source}</div>
              <div><strong>Source Inv ID:</strong> {detailModal.entitlement.source_invitation_id || "N/A"}</div>
              <div><strong>Access Ends At:</strong> {new Date(detailModal.entitlement.access_ends_at).toLocaleString()}</div>
            </div>
            <div>
              <h4 className="font-semibold text-sm mb-2 text-gray-800">Adjustment History</h4>
              {detailModal.adjustments.length === 0 ? (
                <p className="text-xs text-gray-500 italic">No adjustments recorded for this entitlement.</p>
              ) : (
                <ul className="text-xs space-y-2">
                  {detailModal.adjustments.map((adj) => (
                    <li key={adj.id} className="border p-2 rounded bg-gray-50">
                      <div>Adjusted At: {new Date(adj.adjusted_at).toLocaleString()}</div>
                      <div>Reason: {adj.reason}</div>
                      <div>New Expiry: {new Date(adj.new_access_ends_at).toLocaleString()}</div>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        </div>
      )}
    </main>
  );
}
