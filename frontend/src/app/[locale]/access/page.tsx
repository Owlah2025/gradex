"use client";

import { useEffect, useState, useCallback, useRef } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import Link from "next/link";
import { useLocale } from "@/lib/i18n/locale-provider";
import { safeReturnTo } from "@/lib/identity/return-to";
import { captureTokenFromFragment, releaseFragmentToken, scrubTokenFragment } from "@/lib/identity/validation";
import {
  StudentCourseAccessInvitation,
  StudentCourseAccessHistoryItem,
  getStudentCourseAccessInvitation,
  acceptStudentCourseAccessInvitation,
  getStudentCourseAccessHistory,
} from "@/lib/api/access";
import { ProblemError } from "@/lib/api/problem";

export default function StudentCourseAccessPage() {
  const { locale } = useLocale();
  const searchParams = useSearchParams();
  const router = useRouter();

  const invitationId = searchParams.get("invitation_id") || searchParams.get("id");
  const token = useRef<string | null>(null);

  const [activeInvitation, setActiveInvitation] = useState<StudentCourseAccessInvitation | null>(null);
  const [historyItems, setHistoryItems] = useState<StudentCourseAccessHistoryItem[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [submitting, setSubmitting] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  useEffect(() => {
    if (!invitationId) return;
    const captureInvitationToken = () => {
      token.current = captureTokenFromFragment("COURSE_ACCESS_INVITATION");
      scrubTokenFragment();
    };
    captureInvitationToken();
    window.addEventListener("hashchange", captureInvitationToken);
    return () => window.removeEventListener("hashchange", captureInvitationToken);
  }, [invitationId]);

  const fetchStudentData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      if (invitationId) {
        try {
          const inv = await getStudentCourseAccessInvitation(invitationId, locale);
          setActiveInvitation(inv);
        } catch (e: unknown) {
          if (e instanceof ProblemError) {
            if (e.problem.status === 404) {
              setError("Invitation not found or addressed to a different account.");
            } else {
              setError(e.problem.detail || e.problem.title || "Unable to load invitation details.");
            }
          } else if (e && typeof e === "object" && "message" in e && typeof e.message === "string") {
            setError(e.message);
          } else {
            setError("Unable to load invitation details.");
          }
        }
      }

      const hist = await getStudentCourseAccessHistory(locale);
      if (hist && hist.items) {
        setHistoryItems(hist.items);
      }
    } catch (e: unknown) {
      if (e instanceof ProblemError && e.problem.status === 401) {
        const rawTarget = `/${locale}/access?${searchParams.toString()}`;
        const validatedTarget = safeReturnTo(rawTarget) || `/${locale}/access`;
        router.push(`/login?returnTo=${encodeURIComponent(validatedTarget)}`);
        return;
      }
    } finally {
      setLoading(false);
    }
  }, [invitationId, locale, router, searchParams]);

  useEffect(() => {
    fetchStudentData();
  }, [fetchStudentData]);

  const handleAccept = async () => {
    if (!invitationId || !token.current) {
      setError("Acceptance link is missing a valid single-use token.");
      return;
    }

    setSubmitting(true);
    setError(null);
    setSuccess(null);

    try {
      const updated = await acceptStudentCourseAccessInvitation(invitationId, token.current, locale);
      token.current = null;
      releaseFragmentToken("COURSE_ACCESS_INVITATION");
      setActiveInvitation(updated);
      setSuccess("Invitation accepted successfully! Your request is now pending admin approval.");
      fetchStudentData();
    } catch (err: unknown) {
      if (err instanceof ProblemError) {
        if (err.problem.status === 410) {
          token.current = null;
          releaseFragmentToken("COURSE_ACCESS_INVITATION");
          setError("This acceptance link has expired, been consumed, or superseded. Please request a new link.");
        } else if (err.problem.status === 409) {
          setError("This invitation is not in an acceptable state.");
        } else if (err.problem.status === 404) {
          setError("Invitation not found or addressed to another account.");
        } else {
          setError(err.problem.detail || err.problem.title || "Acceptance failed.");
        }
      } else if (err && typeof err === "object" && "message" in err && typeof err.message === "string") {
        setError(err.message);
      } else {
        setError("Acceptance failed.");
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="max-w-4xl mx-auto p-6 space-y-8">
      <div className="border-b pb-4">
        <h1 className="text-3xl font-bold text-gray-900">Student Course Access Portal</h1>
        <p className="text-sm text-gray-600 mt-1">
          Accept course access invitations and view your active course access status.
        </p>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-800 rounded-md p-4 text-sm" role="alert">
          <strong>Notice:</strong> {error}
        </div>
      )}

      {success && (
        <div className="bg-green-50 border border-green-200 text-green-800 rounded-md p-4 text-sm" role="status">
          <strong>Success:</strong> {success}
        </div>
      )}

      {/* Target Invitation Action Card */}
      {invitationId && (
        <section className="bg-white border rounded-lg p-6 shadow-sm space-y-4">
          <h2 className="text-xl font-semibold text-gray-800">Course Access Invitation Action</h2>

          {loading ? (
            <p className="text-sm text-gray-500">Loading invitation status...</p>
          ) : activeInvitation ? (
            <div className="space-y-4">
              <div className="bg-gray-50 p-4 rounded border text-sm space-y-2">
                <div><strong>Course ID:</strong> {activeInvitation.course_id}</div>
                <div>
                  <strong>Current Status:</strong>{" "}
                  <span
                    className={`inline-block px-2 py-0.5 text-xs font-semibold rounded ${
                      activeInvitation.state === "APPROVED"
                        ? "bg-green-100 text-green-800"
                        : activeInvitation.state === "PENDING_ADMIN_APPROVAL"
                        ? "bg-amber-100 text-amber-800"
                        : activeInvitation.state === "PENDING_STUDENT_ACCEPTANCE"
                        ? "bg-blue-100 text-blue-800"
                        : "bg-red-100 text-red-800"
                    }`}
                  >
                    {activeInvitation.state}
                  </span>
                </div>
              </div>

              {activeInvitation.state === "PENDING_STUDENT_ACCEPTANCE" && (
                <div className="bg-blue-50 border-l-4 border-blue-500 p-4 rounded text-sm text-blue-900 space-y-3">
                  <p className="font-medium">
                    You have received a course access invitation.
                  </p>
                  <p className="text-xs text-blue-700">
                    Accepting this invitation submits your request for administrator review. <strong>Acceptance does not grant immediate access to course materials.</strong>
                  </p>
                  <button
                    onClick={handleAccept}
                    disabled={submitting || !token}
                    className="bg-blue-600 hover:bg-blue-700 text-white font-medium py-2 px-6 rounded-md shadow-sm text-sm disabled:opacity-50"
                  >
                    {submitting ? "Accepting Invitation..." : "Accept Invitation & Request Access"}
                  </button>
                </div>
              )}

              {activeInvitation.state === "PENDING_ADMIN_APPROVAL" && (
                <div className="bg-amber-50 border-l-4 border-amber-500 p-4 rounded text-sm text-amber-900">
                  <p className="font-semibold">Invitation Accepted — Pending Administrator Approval</p>
                  <p className="text-xs text-amber-700 mt-1">
                    Your acceptance has been recorded. Course access will become available once an administrator approves your grant.
                  </p>
                </div>
              )}

              {activeInvitation.state === "APPROVED" && (
                <div className="bg-green-50 border-l-4 border-green-500 p-4 rounded text-sm text-green-900 space-y-3">
                  <p className="font-semibold">Course Access Granted!</p>
                  <p className="text-xs text-green-700">
                    Your access has been approved by the administrator. You may now watch lessons and access protected course content.
                  </p>
                  <div>
                    <Link
                      href={`/${locale}/learn/courses/${activeInvitation.course_id}`}
                      className="inline-block bg-green-600 hover:bg-green-700 text-white font-medium py-2 px-6 rounded-md text-sm shadow-sm"
                    >
                      Open & Watch Course
                    </Link>
                  </div>
                </div>
              )}

              {activeInvitation.state === "REJECTED" && (
                <div className="bg-red-50 border-l-4 border-red-500 p-4 rounded text-sm text-red-900">
                  <p className="font-semibold">Invitation Declined</p>
                  {activeInvitation.decision_reason && (
                    <p className="text-xs text-red-700 mt-1">Reason: {activeInvitation.decision_reason}</p>
                  )}
                </div>
              )}
            </div>
          ) : (
            <p className="text-sm text-gray-500">No active invitation selected.</p>
          )}
        </section>
      )}

      {/* Student Course Access History */}
      <section className="bg-white border rounded-lg p-6 shadow-sm">
        <h2 className="text-xl font-semibold text-gray-800 mb-4">My Course Access & History</h2>
        {historyItems.length === 0 ? (
          <p className="text-sm text-gray-500 py-6 text-center border rounded bg-gray-50">
            No course access records found for your account.
          </p>
        ) : (
          <div className="space-y-4">
            {historyItems.map((item) => (
              <div
                key={item.course_id}
                className="border p-4 rounded-lg flex flex-col md:flex-row md:items-center justify-between gap-4 hover:bg-gray-50"
              >
                <div>
                  <div className="font-semibold text-gray-900">Course: {item.course_id}</div>
                  <div className="text-xs text-gray-500 mt-1">
                    {item.has_active_access ? (
                      <span className="text-green-700 font-medium">
                        Active Access Until: {item.access_ends_at ? new Date(item.access_ends_at).toLocaleString() : "Permanent"}
                      </span>
                    ) : (
                      <span className="text-gray-500">No Active Access</span>
                    )}
                  </div>
                </div>

                <div>
                  {item.has_active_access ? (
                    <Link
                      href={`/${locale}/learn/courses/${item.course_id}`}
                      className="bg-emerald-600 hover:bg-emerald-700 text-white font-medium px-4 py-2 rounded text-xs inline-block"
                    >
                      Watch Course
                    </Link>
                  ) : item.invitation ? (
                    <span className="text-xs text-gray-600 bg-gray-100 px-2 py-1 rounded">
                      Status: {item.invitation.state}
                    </span>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
