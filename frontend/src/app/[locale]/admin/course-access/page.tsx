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
  adjustEntitlementExpiry,
  revokeEntitlement,
} from "@/lib/api/access";
import { getPublicCourses } from "@/lib/api/public-catalog";
import { ProblemError } from "@/lib/api/problem";
import { PublishedCourseSelector } from "@/components/admin/published-course-selector";
import { PurchaseRequestsPanel } from "@/components/admin/purchase-requests";
import {
  buildPublishedCourseOptions,
  findPublishedCourse,
  invitationCourseLabel,
  type PublishedCourseOption,
} from "@/components/admin/published-courses";

function getProblemErrorMessage(e: unknown, fallback: string): string {
  if (e instanceof ProblemError) {
    return e.problem.detail || e.problem.title || fallback;
  }
  if (
    e &&
    typeof e === "object" &&
    "message" in e &&
    typeof e.message === "string"
  ) {
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

  // The one Course context both Admin operations act on. The Admin picks a
  // published Course by title; the identifier below is never typed by hand.
  const [courseOptions, setCourseOptions] = useState<PublishedCourseOption[]>(
    [],
  );
  const [coursesLoading, setCoursesLoading] = useState<boolean>(true);
  const [coursesError, setCoursesError] = useState<string | null>(null);
  const [selectedCourseId, setSelectedCourseId] = useState<string>("");

  // Form states: Expiry Configuration
  const [expiryDate, setExpiryDate] = useState<string>("");
  const [expiryReason, setExpiryReason] = useState<string>("");
  const [expirySubmitting, setExpirySubmitting] = useState<boolean>(false);

  // Form states: Create Invitation
  const [createEmail, setCreateEmail] = useState<string>("");
  const [createNote, setCreateNote] = useState<string>("");
  const [createRef, setCreateRef] = useState<string>("");
  const [createSubmitting, setCreateSubmitting] = useState<boolean>(false);

  // Modal state: Reject Reason
  const [rejectingInvId, setRejectingInvId] = useState<string | null>(null);
  const [rejectReason, setRejectReason] = useState<string>("");
  const [rejectSubmitting, setRejectSubmitting] = useState<boolean>(false);

  // Modal state: Entitlement Detail — the AD07 surface. It is where an
  // existing grant is inspected and, under BR-026, extended, shortened, or
  // revoked. The Admin reaches it from a queue row, never by identifier.
  const [detailModal, setDetailModal] = useState<AdminEntitlementDetail | null>(
    null,
  );
  const [detailLoading, setDetailLoading] = useState<boolean>(false);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [detailNotice, setDetailNotice] = useState<string | null>(null);
  const [detailBusy, setDetailBusy] = useState<boolean>(false);
  const [adjustDate, setAdjustDate] = useState<string>("");
  const [adjustReason, setAdjustReason] = useState<string>("");
  const [adjustSupportRef, setAdjustSupportRef] = useState<string>("");
  const [revokeReason, setRevokeReason] = useState<string>("");
  const [revokeSupportRef, setRevokeSupportRef] = useState<string>("");
  const [revokeConfirming, setRevokeConfirming] = useState<boolean>(false);

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

  /**
   * The published catalogue is the authoritative list of Courses a launch
   * grant can target: published, not emergency-suspended, not retired, live
   * revision. The second read only supplies the other-locale title, so its
   * failure narrows the label rather than the Course list.
   */
  const fetchCourses = useCallback(async () => {
    setCoursesLoading(true);
    setCoursesError(null);
    try {
      const alternateLocale = locale === "ar" ? "en" : "ar";
      const [primary, alternate] = await Promise.all([
        getPublicCourses(locale),
        getPublicCourses(alternateLocale).catch(() => null),
      ]);
      const options = buildPublishedCourseOptions(
        primary.items ?? [],
        alternate?.items ?? [],
      );
      setCourseOptions(options);
      // A Course that left the published catalogue must not stay silently
      // selected under a stale label.
      setSelectedCourseId((current) =>
        current && options.some((option) => option.id === current)
          ? current
          : "",
      );
    } catch (e: unknown) {
      setCourseOptions([]);
      setSelectedCourseId("");
      setCoursesError(
        getProblemErrorMessage(e, "Failed to load published Courses"),
      );
    } finally {
      setCoursesLoading(false);
    }
  }, [locale]);

  useEffect(() => {
    fetchInvitations();
  }, [fetchInvitations]);

  useEffect(() => {
    fetchCourses();
  }, [fetchCourses]);

  const selectedCourse = findPublishedCourse(courseOptions, selectedCourseId);
  const courseLabel = (courseID: string): string =>
    invitationCourseLabel(courseOptions, courseID);

  const handleSetExpiry = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedCourseId || !expiryDate || !expiryReason) return;
    setExpirySubmitting(true);
    setError(null);
    setSuccess(null);
    try {
      await setCourseDefaultAccessExpiry(
        selectedCourseId,
        expiryDate,
        expiryReason,
        locale,
      );
      setSuccess(
        `Default access expiry configured for ${selectedCourse?.title ?? courseLabel(selectedCourseId)}`,
      );
      setExpiryDate("");
      setExpiryReason("");
    } catch (err: unknown) {
      setError(
        getProblemErrorMessage(err, "Failed to set default access expiry"),
      );
    } finally {
      setExpirySubmitting(false);
    }
  };

  const handleCreateInvitation = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedCourseId || !createEmail) return;
    setCreateSubmitting(true);
    setError(null);
    setSuccess(null);
    try {
      const created = await createCourseAccessInvitation(
        selectedCourseId,
        createEmail,
        createNote || undefined,
        createRef || undefined,
        locale,
      );
      setSuccess(
        `Course access invitation created for ${created?.email || createEmail} on ${
          selectedCourse?.title ?? courseLabel(selectedCourseId)
        }`,
      );
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
      // The grant is now reachable from its queue row; the Admin never needs
      // the identifier it was previously shown here.
      setSuccess(
        `Invitation approved! Course access is active until ${
          res?.entitlement?.access_ends_at
            ? new Date(res.entitlement.access_ends_at).toLocaleString()
            : "the configured expiry"
        }. Use "Manage access" on the row to change or revoke it.`,
      );
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
      await rejectCourseAccessInvitation(
        rejectingInvId,
        rejectReason.trim(),
        locale,
      );
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
      setSuccess(
        `New acceptance link generated and queued for invitation ${id}.`,
      );
      fetchInvitations();
    } catch (err: unknown) {
      setError(getProblemErrorMessage(err, "Resend failed"));
    }
  };

  const resetEntitlementForms = () => {
    setDetailError(null);
    setDetailNotice(null);
    setAdjustDate("");
    setAdjustReason("");
    setAdjustSupportRef("");
    setRevokeReason("");
    setRevokeSupportRef("");
    setRevokeConfirming(false);
  };

  const handleViewEntitlement = async (entitlementId: string) => {
    setDetailLoading(true);
    resetEntitlementForms();
    try {
      const detail = await getAdminEntitlementDetail(entitlementId, locale);
      if (detail) {
        setDetailModal(detail);
      }
    } catch (err: unknown) {
      setError(
        getProblemErrorMessage(err, "Failed to load entitlement details"),
      );
    } finally {
      setDetailLoading(false);
    }
  };

  const closeEntitlementDetail = () => {
    setDetailModal(null);
    resetEntitlementForms();
  };

  /**
   * One expiry adjustment. The direction is the server's business: a later
   * date extends access, an earlier one shortens it, and a past date ends it
   * immediately. The Admin supplies the date and the required reason.
   */
  const handleAdjustExpiry = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!detailModal || !adjustDate || !adjustReason.trim() || detailBusy)
      return;
    setDetailBusy(true);
    setDetailError(null);
    setDetailNotice(null);
    try {
      const updated = await adjustEntitlementExpiry(
        detailModal.entitlement.id,
        adjustDate,
        adjustReason.trim(),
        {
          supportReference: adjustSupportRef.trim() || undefined,
          // The revision the Admin is looking at. A grant changed by someone
          // else in the meantime is refused, not silently overwritten.
          expectedRevision: detailModal.entitlement.revision,
        },
        locale,
      );
      if (updated) setDetailModal(updated);
      setDetailNotice("Access expiry updated.");
      setAdjustDate("");
      setAdjustReason("");
      setAdjustSupportRef("");
      fetchInvitations();
    } catch (err: unknown) {
      setDetailError(
        getProblemErrorMessage(err, "Failed to update access expiry"),
      );
    } finally {
      setDetailBusy(false);
    }
  };

  const handleRevokeEntitlement = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!detailModal || !revokeReason.trim() || !revokeConfirming || detailBusy)
      return;
    setDetailBusy(true);
    setDetailError(null);
    setDetailNotice(null);
    try {
      const updated = await revokeEntitlement(
        detailModal.entitlement.id,
        revokeReason.trim(),
        {
          supportReference: revokeSupportRef.trim() || undefined,
          expectedRevision: detailModal.entitlement.revision,
        },
        locale,
      );
      if (updated) setDetailModal(updated);
      setDetailNotice(
        "Course access revoked. Enrollment and progress records are retained.",
      );
      setRevokeReason("");
      setRevokeSupportRef("");
      setRevokeConfirming(false);
      fetchInvitations();
    } catch (err: unknown) {
      setDetailError(getProblemErrorMessage(err, "Failed to revoke access"));
    } finally {
      setDetailBusy(false);
    }
  };

  return (
    <main id="main" className="max-w-7xl mx-auto p-6 space-y-8">
      <div className="border-b pb-4">
        <h1 className="text-3xl font-bold tracking-tight text-gray-900">
          Course Access Management
        </h1>
        <p className="text-sm text-gray-600 mt-1">
          Configure course default access expiry, issue manual course
          invitations, approve pending grants, and manage entitlement records.
        </p>
      </div>

      {error && (
        <div
          className="bg-red-50 border border-red-200 text-red-800 rounded-md p-4 text-sm"
          role="alert"
        >
          <strong>Error:</strong> {error}
        </div>
      )}

      {success && (
        <div
          className="bg-green-50 border border-green-200 text-green-800 rounded-md p-4 text-sm"
          role="status"
        >
          <strong>Success:</strong> {success}
        </div>
      )}

      <PurchaseRequestsPanel />

      <PublishedCourseSelector
        options={courseOptions}
        loading={coursesLoading}
        error={coursesError}
        selectedCourseID={selectedCourseId}
        onSelect={setSelectedCourseId}
        onRetry={fetchCourses}
      />

      <div className="grid grid-cols-1 md:grid-cols-2 gap-8">
        {/* Course Default Access Expiry Config */}
        <section className="bg-white p-6 rounded-lg border shadow-sm">
          <h2 className="text-xl font-semibold mb-4 text-gray-800">
            1. Configure Course Access Expiry
          </h2>
          <p
            className="text-sm text-gray-600 mb-4"
            data-testid="expiry-course-context"
          >
            {selectedCourse
              ? `Applies to ${selectedCourse.title}.`
              : "Select a published Course above to configure its default access expiry."}
          </p>
          <form onSubmit={handleSetExpiry} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700">
                Default Access Expiry Date (YYYY-MM-DD)
              </label>
              <input
                type="date"
                required
                value={expiryDate}
                onChange={(e) => setExpiryDate(e.target.value)}
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">
                Reason / Reference
              </label>
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
              disabled={expirySubmitting || !selectedCourseId}
              className="w-full bg-blue-600 hover:bg-blue-700 text-white font-medium py-2 px-4 rounded-md shadow-sm text-sm disabled:opacity-50"
            >
              {expirySubmitting ? "Saving..." : "Save Default Expiry"}
            </button>
          </form>
        </section>

        {/* Create Invitation */}
        <section className="bg-white p-6 rounded-lg border shadow-sm">
          <h2 className="text-xl font-semibold mb-4 text-gray-800">
            2. Issue Course Access Invitation
          </h2>
          <p
            className="text-sm text-gray-600 mb-4"
            data-testid="invitation-course-context"
          >
            {selectedCourse
              ? `Grants access to ${selectedCourse.title}.`
              : "Select a published Course above to issue an invitation."}
          </p>
          <form onSubmit={handleCreateInvitation} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700">
                Student Email Address
              </label>
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
              <label className="block text-sm font-medium text-gray-700">
                Admin Internal Note (Optional)
              </label>
              <input
                type="text"
                value={createNote}
                onChange={(e) => setCreateNote(e.target.value)}
                placeholder="Approved via scholarship program"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">
                External Reference ID (Optional)
              </label>
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
              disabled={createSubmitting || !selectedCourseId}
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
          <h2 className="text-xl font-semibold text-gray-800">
            3. Invitation Queue & Decision Center
          </h2>
          <button
            onClick={fetchInvitations}
            disabled={loading}
            className="text-sm bg-gray-100 hover:bg-gray-200 text-gray-700 py-1 px-3 rounded-md border"
          >
            {loading ? "Refreshing..." : "Refresh Queue"}
          </button>
        </div>

        {loading ? (
          <p className="text-gray-500 text-sm py-8 text-center">
            Loading invitations queue...
          </p>
        ) : invitations.length === 0 ? (
          <p className="text-gray-500 text-sm py-8 text-center border rounded-md bg-gray-50">
            No invitations found.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm border-collapse">
              <thead>
                <tr className="bg-gray-100 border-b text-gray-700">
                  <th className="p-3">ID / Recipient</th>
                  <th className="p-3">Course</th>
                  <th className="p-3">State</th>
                  <th className="p-3">Timestamps</th>
                  <th className="p-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {invitations.map((inv) => (
                  <tr key={inv.id} className="hover:bg-gray-50">
                    <td className="p-3 text-xs">
                      {/* The invitation is identified to the Admin by the person it was sent to.
                          Its identifier stays internal — it was never something an Admin acted on,
                          and rendering it only invited manual identifier handling. */}
                      <div className="font-semibold text-gray-900">
                        {inv.email}
                      </div>
                    </td>
                    <td
                      className="p-3 text-xs text-gray-700"
                      data-testid={`invitation-course-${inv.id}`}
                    >
                      {courseLabel(inv.course_id)}
                    </td>
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
                        <div className="text-xs text-gray-500 mt-1">
                          Reason: {inv.decision_reason}
                        </div>
                      )}
                    </td>
                    <td className="p-3 text-xs text-gray-500 space-y-1">
                      <div>
                        Created: {new Date(inv.created_at).toLocaleString()}
                      </div>
                      {inv.accepted_at && (
                        <div>
                          Accepted: {new Date(inv.accepted_at).toLocaleString()}
                        </div>
                      )}
                      {inv.decided_at && (
                        <div>
                          Decided: {new Date(inv.decided_at).toLocaleString()}
                        </div>
                      )}
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

                      {inv.entitlement_id && (
                        <button
                          onClick={() =>
                            handleViewEntitlement(inv.entitlement_id as string)
                          }
                          disabled={detailLoading}
                          data-testid={`manage-access-${inv.id}`}
                          className="bg-slate-700 hover:bg-slate-800 text-white px-3 py-1 text-xs font-semibold rounded disabled:opacity-50"
                        >
                          {detailLoading ? "Opening..." : "Manage access"}
                        </button>
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
            <h3 className="text-lg font-bold text-gray-900 mb-2">
              Reject Course Access Invitation
            </h3>
            <p className="text-sm text-gray-600 mb-4">
              Specify the reason for rejecting invitation{" "}
              <code className="bg-gray-100 p-1 rounded text-xs">
                {rejectingInvId}
              </code>
              .
            </p>
            <form onSubmit={handleRejectSubmit} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  Rejection Reason
                </label>
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

      {/* Entitlement Detail Modal (AD07) */}
      {detailModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50 overflow-y-auto">
          <div
            className="bg-white rounded-lg p-6 max-w-xl w-full shadow-xl space-y-4 my-8"
            data-testid="entitlement-detail"
          >
            <div className="flex justify-between items-center border-b pb-2">
              <h3 className="text-lg font-bold text-gray-900">
                Course Access Record
              </h3>
              <button
                onClick={closeEntitlementDetail}
                className="text-gray-500 hover:text-gray-700 font-bold text-lg"
              >
                &times;
              </button>
            </div>
            <div className="space-y-2 text-sm bg-gray-50 p-4 rounded border">
              <div>
                <strong>Course:</strong>{" "}
                {courseLabel(detailModal.entitlement.course_id)}
              </div>
              <div>
                <strong>Status:</strong>{" "}
                <span
                  data-testid="entitlement-state"
                  className={`font-bold ${detailModal.entitlement.state === "ACTIVE" ? "text-green-700" : "text-red-700"}`}
                >
                  {detailModal.entitlement.state}
                </span>
              </div>
              <div data-testid="entitlement-access-ends-at">
                <strong>Access ends:</strong>{" "}
                {new Date(
                  detailModal.entitlement.access_ends_at,
                ).toLocaleString()}
              </div>
              <div className="text-xs text-gray-600">
                Originally granted until{" "}
                {new Date(
                  detailModal.entitlement.original_access_ends_at,
                ).toLocaleString()}
              </div>
              {detailModal.entitlement.revoked_at && (
                <div
                  className="text-xs text-red-700"
                  data-testid="entitlement-revoked-at"
                >
                  Revoked on{" "}
                  {new Date(
                    detailModal.entitlement.revoked_at,
                  ).toLocaleString()}
                </div>
              )}
              <div className="text-xs text-gray-600">
                Grant source: {detailModal.entitlement.grant_source}
              </div>
            </div>

            {detailNotice && (
              <p
                role="status"
                data-testid="entitlement-notice"
                className="rounded border border-green-200 bg-green-50 p-3 text-sm text-green-800"
              >
                {detailNotice}
              </p>
            )}
            {detailError && (
              <p
                role="alert"
                data-testid="entitlement-error"
                className="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-800"
              >
                {detailError}
              </p>
            )}

            {detailModal.entitlement.state === "ACTIVE" ? (
              <div className="space-y-5">
                <form
                  onSubmit={handleAdjustExpiry}
                  className="space-y-3 border rounded-md p-4"
                  data-testid="entitlement-expiry-form"
                >
                  <h4 className="font-semibold text-sm text-gray-800">
                    Change access expiry
                  </h4>
                  <p className="text-xs text-gray-600">
                    A later date extends access; an earlier date shortens it. A
                    date already past ends access immediately and keeps
                    enrollment and progress.
                  </p>
                  <div>
                    <label
                      className="block text-sm font-medium text-gray-700"
                      htmlFor="entitlement-expiry-date"
                    >
                      New access expiry date (YYYY-MM-DD)
                    </label>
                    <input
                      id="entitlement-expiry-date"
                      type="date"
                      required
                      value={adjustDate}
                      onChange={(e) => setAdjustDate(e.target.value)}
                      className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
                    />
                  </div>
                  <div>
                    <label
                      className="block text-sm font-medium text-gray-700"
                      htmlFor="entitlement-expiry-reason"
                    >
                      Reason
                    </label>
                    <input
                      id="entitlement-expiry-reason"
                      type="text"
                      required
                      value={adjustReason}
                      onChange={(e) => setAdjustReason(e.target.value)}
                      placeholder="Semester extended for the whole cohort"
                      className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
                    />
                  </div>
                  <div>
                    <label
                      className="block text-sm font-medium text-gray-700"
                      htmlFor="entitlement-expiry-reference"
                    >
                      Support reference (optional)
                    </label>
                    <input
                      id="entitlement-expiry-reference"
                      type="text"
                      value={adjustSupportRef}
                      onChange={(e) => setAdjustSupportRef(e.target.value)}
                      placeholder="SUPPORT-2026-08"
                      className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
                    />
                  </div>
                  <button
                    type="submit"
                    disabled={detailBusy || !adjustDate || !adjustReason.trim()}
                    data-testid="save-entitlement-expiry"
                    className="w-full bg-blue-600 hover:bg-blue-700 text-white font-medium py-2 px-4 rounded-md text-sm disabled:opacity-50"
                  >
                    {detailBusy ? "Saving..." : "Save new expiry"}
                  </button>
                </form>

                <form
                  onSubmit={handleRevokeEntitlement}
                  className="space-y-3 border border-red-200 rounded-md p-4 bg-red-50/40"
                  data-testid="entitlement-revoke-form"
                >
                  <h4 className="font-semibold text-sm text-red-800">
                    Revoke access
                  </h4>
                  <p className="text-xs text-gray-700">
                    {"Revoking ends this Student's access to the Course immediately. The enrollment record, learning progress and access history are kept."}
                  </p>
                  <div>
                    <label
                      className="block text-sm font-medium text-gray-700"
                      htmlFor="entitlement-revoke-reason"
                    >
                      Reason
                    </label>
                    <input
                      id="entitlement-revoke-reason"
                      type="text"
                      required
                      value={revokeReason}
                      onChange={(e) => setRevokeReason(e.target.value)}
                      placeholder="Access ended after out-of-band refund"
                      className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
                    />
                  </div>
                  <div>
                    <label
                      className="block text-sm font-medium text-gray-700"
                      htmlFor="entitlement-revoke-reference"
                    >
                      Support reference (optional)
                    </label>
                    <input
                      id="entitlement-revoke-reference"
                      type="text"
                      value={revokeSupportRef}
                      onChange={(e) => setRevokeSupportRef(e.target.value)}
                      placeholder="SUPPORT-2026-08"
                      className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
                    />
                  </div>
                  <label className="flex items-start gap-2 text-xs text-gray-800">
                    <input
                      type="checkbox"
                      checked={revokeConfirming}
                      onChange={(e) => setRevokeConfirming(e.target.checked)}
                      data-testid="confirm-revoke-entitlement"
                      className="mt-0.5"
                    />
                    <span>
                      I confirm this Student should lose access to{" "}
                      {courseLabel(detailModal.entitlement.course_id)} now.
                    </span>
                  </label>
                  <button
                    type="submit"
                    disabled={detailBusy || !revokeConfirming || !revokeReason.trim()}
                    data-testid="revoke-entitlement"
                    className="w-full bg-red-600 hover:bg-red-700 text-white font-medium py-2 px-4 rounded-md text-sm disabled:opacity-50"
                  >
                    {detailBusy ? "Revoking..." : "Revoke access"}
                  </button>
                </form>
              </div>
            ) : (
              <p
                className="text-sm text-gray-700 border rounded-md p-4 bg-gray-50"
                data-testid="entitlement-terminal"
              >
                This access grant is revoked. It is kept as history and can no
                longer be extended, shortened, or revoked again.
              </p>
            )}
            <div>
              <h4 className="font-semibold text-sm mb-2 text-gray-800">
                Adjustment History
              </h4>
              {detailModal.adjustments.length === 0 ? (
                <p className="text-xs text-gray-500 italic">
                  No adjustments recorded for this entitlement.
                </p>
              ) : (
                <ul className="text-xs space-y-2">
                  {detailModal.adjustments.map((adj) => (
                    <li key={adj.id} className="border p-2 rounded bg-gray-50">
                      <div>
                        Adjusted At:{" "}
                        {new Date(adj.adjusted_at).toLocaleString()}
                      </div>
                      <div>Reason: {adj.reason}</div>
                      <div>
                        New Expiry:{" "}
                        {new Date(adj.new_access_ends_at).toLocaleString()}
                      </div>
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
