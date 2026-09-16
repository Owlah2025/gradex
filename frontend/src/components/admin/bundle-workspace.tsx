"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "next/navigation";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field } from "@/components/ui/field";
import { Alert } from "@/components/ui/alert";
import { StatusBadge } from "@/components/common/status-badge";
import { PriceDisplay } from "@/components/catalog/price-display";
import { bundleCourseCount } from "@/components/catalog/bundle-presentation";
import {
  createAdminBundle,
  deleteAdminBundle,
  getAdminBundleCourses,
  getAdminBundle,
  listAdminBundles,
  setCoursePrice,
  transitionAdminBundle,
  updateAdminBundle,
  type AdminBundleCourseOption,
  type BundleMutation,
  type AdminBundle,
  type AdminBundleMember,
} from "@/lib/api/catalog";
import { ProblemError } from "@/lib/api/problem";
import { formatFils } from "@/lib/formatters/currency";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

type BundleCopy = ReturnType<typeof useLocale>["t"]["adminBundles"];

/**
 * Turns a failed call into something an Admin can act on.
 *
 * The workspace previously collapsed every failure into one "Bundles could not
 * be loaded" line, which is why a missing pricing reason, a revision conflict
 * and a capability refusal were indistinguishable from each other and from a
 * server fault. The API already returns a typed problem for each; this reads it
 * rather than discarding it.
 */
function problemMessage(cause: unknown, copy: BundleCopy): string {
  if (!(cause instanceof ProblemError)) return copy.serverError;
  switch (cause.problem.code) {
    case "BUNDLE_REFERENCED":
      return copy.deleteUnavailable;
    case "BUNDLE_STATE_CONFLICT":
      return copy.revisionConflict;
    case "VALIDATION_FAILED":
      return cause.problem.detail ?? copy.invalidOffer;
    case "NOT_FOUND":
      return copy.revisionConflict;
    default:
      break;
  }
  if (cause.problem.status === 401 || cause.problem.status === 403) return copy.forbidden;
  if (cause.problem.status >= 400 && cause.problem.status < 500) {
    return cause.problem.detail ?? copy.serverError;
  }
  return copy.serverError;
}

const LIFECYCLE_TONE: Record<AdminBundle["lifecycle"], "success" | "neutral" | "accent"> = {
  DRAFT: "neutral",
  PUBLISHED: "success",
  DELISTED: "accent",
  ARCHIVED: "neutral",
};

function lifecycleLabel(lifecycle: AdminBundle["lifecycle"], copy: BundleCopy): string {
  return {
    DRAFT: copy.lifecycleDraft,
    PUBLISHED: copy.lifecyclePublished,
    DELISTED: copy.lifecycleDelisted,
    ARCHIVED: copy.lifecycleArchived,
  }[lifecycle];
}

function lifecycleHelp(lifecycle: AdminBundle["lifecycle"], copy: BundleCopy): string {
  return {
    DRAFT: copy.lifecycleDraftHelp,
    PUBLISHED: copy.lifecyclePublishedHelp,
    DELISTED: copy.lifecycleDelistedHelp,
    ARCHIVED: copy.lifecycleArchivedHelp,
  }[lifecycle];
}

function memberName(member: AdminBundleMember, locale: "ar" | "en"): string {
  return locale === "ar" ? member.title_ar : member.title_en;
}

/**
 * The savings line, or nothing.
 *
 * It renders only when the server supplied a complete member total and that
 * total genuinely exceeds the Bundle price. A Bundle priced at or above its
 * members is not a saving, and claiming one would be a false commercial
 * statement on an Admin screen that Admins price from.
 */
function savingsMinorUnits(bundle: AdminBundle): number | null {
  if (bundle.member_total_minor_units == null || !bundle.price) return null;
  const saving = bundle.member_total_minor_units - bundle.price.effective_minor_units;
  return saving > 0 ? saving : null;
}

type CourseChoice = AdminBundleCourseOption & { titleAr: string; titleEn: string };
type Draft = {
  id?: string;
  revision?: number;
  titleAr: string;
  titleEn: string;
  descriptionAr: string;
  descriptionEn: string;
  courseIDs: string[];
  regular: string;
  offer: string;
  reason: string;
};

const emptyDraft = (): Draft => ({ titleAr: "", titleEn: "", descriptionAr: "", descriptionEn: "", courseIDs: [], regular: "", offer: "", reason: "" });

function mergeCourseChoices(current: CourseChoice[], incoming: AdminBundleCourseOption[]): CourseChoice[] {
  const byID = new Map(current.map((course) => [course.id, course]));
  for (const course of incoming) {
    byID.set(course.id, { ...course, titleAr: course.title_ar, titleEn: course.title_en });
  }
  return [...byID.values()];
}

function buildBundleMutation(draft: Draft): BundleMutation | null {
  const regularText = draft.regular.trim();
  const offerText = draft.offer.trim();
  if (regularText === "" && offerText !== "") return null;

  const regular = regularText === "" ? null : Number(regularText);
  const offer = offerText === "" ? null : Number(offerText);
  if (
    regular !== null &&
    (!Number.isSafeInteger(regular) ||
      regular < 0 ||
      (offer !== null &&
        (!Number.isSafeInteger(offer) || offer <= 0 || offer >= regular)))
  ) return null;

  const body: BundleMutation = {
    title_ar: draft.titleAr,
    title_en: draft.titleEn,
    description_ar: draft.descriptionAr,
    description_en: draft.descriptionEn,
    course_ids: draft.courseIDs,
    expected_revision: draft.revision,
  };
  if (regular !== null) {
    body.regular_price_minor_units = regular;
    body.offer_price_minor_units = offer;
    body.price_reason = draft.reason;
  }
  return body;
}

export function BundleWorkspace() {
  const { locale, t } = useLocale();
  const copy = t.adminBundles;
  const searchParams = useSearchParams();
  const [bundles, setBundles] = useState<AdminBundle[] | null>(null);
  const [courses, setCourses] = useState<CourseChoice[]>([]);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [search, setSearch] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // Which Bundle a mutation is currently in flight for, so the pending state is
  // shown on the row being changed rather than greying out the whole page.
  const [pendingBundleID, setPendingBundleID] = useState<string | null>(null);
  const [deleteCandidate, setDeleteCandidate] = useState<AdminBundle | null>(null);
  const [coursePriceID, setCoursePriceID] = useState(searchParams.get("course_price") ?? "");
  const [courseRegular, setCourseRegular] = useState("");
  const [courseOffer, setCourseOffer] = useState("");
  const [courseReason, setCourseReason] = useState("");
  const [coursePage, setCoursePage] = useState(1);
  const [coursePageSize, setCoursePageSize] = useState(20);
  const [courseTotal, setCourseTotal] = useState(0);
  const [courseLoading, setCourseLoading] = useState(true);
  const [courseError, setCourseError] = useState<string | null>(null);
  const courseLoadSequence = useRef(0);
  const bundleLoadSequence = useRef(0);

  /**
   * Refetches the authoritative list.
   *
   * Sequenced, because every mutation triggers one: a slow refresh issued
   * before a fast one must not land after it and repaint the list with the
   * pre-mutation state. Whatever the newest request returns is what the screen
   * shows.
   */
  const loadBundles = useCallback(async () => {
    const requestSequence = ++bundleLoadSequence.current;
    try {
      const bundleResult = await listAdminBundles(locale);
      if (requestSequence !== bundleLoadSequence.current) return;
      setBundles(bundleResult?.items ?? []);
    } catch (cause: unknown) {
      if (requestSequence !== bundleLoadSequence.current) return;
      setBundles((current) => current ?? []);
      setError(problemMessage(cause, copy));
    }
  }, [copy, locale]);

  const loadCourses = useCallback(async () => {
    const requestSequence = ++courseLoadSequence.current;
    setCourseLoading(true);
    setCourseError(null);
    try {
      const result = await getAdminBundleCourses(locale, { page: coursePage, search });
      if (requestSequence !== courseLoadSequence.current) return;
      if (!result) throw new Error("Eligible Course response was empty");
      setCoursePageSize(result.page_size);
      setCourseTotal(result.total);
      setCourses((current) => mergeCourseChoices(current, result.items));
    } catch {
      if (requestSequence === courseLoadSequence.current) {
        setCourseError(copy.coursesFailed);
      }
    } finally {
      if (requestSequence === courseLoadSequence.current) {
        setCourseLoading(false);
      }
    }
  }, [coursePage, copy.coursesFailed, locale, search]);

  useEffect(() => { void loadBundles(); }, [loadBundles]);
  useEffect(() => { void loadCourses(); }, [loadCourses]);

  useEffect(() => {
    if (!coursePriceID || courseRegular !== "") return;
    const course = courses.find((item) => item.id === coursePriceID);
    if (!course?.price) return;
    setCourseRegular(String(course.price.regular_minor_units ?? course.price.effective_minor_units));
    setCourseOffer(course.price.offer_minor_units == null ? "" : String(course.price.offer_minor_units));
  }, [coursePriceID, courseRegular, courses]);

  const visibleCourses = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase(locale);
    return courses.filter((course) => needle === "" || `${course.titleAr} ${course.titleEn}`.toLocaleLowerCase(locale).includes(needle));
  }, [courses, locale, search]);

  function toggleCourse(id: string) {
    setDraft((current) => ({ ...current, courseIDs: current.courseIDs.includes(id) ? current.courseIDs.filter((value) => value !== id) : [...current.courseIDs, id] }));
  }

  function moveCourse(index: number, delta: -1 | 1) {
    setDraft((current) => {
      const next = [...current.courseIDs];
      const destination = index + delta;
      if (destination < 0 || destination >= next.length) return current;
      [next[index], next[destination]] = [next[destination], next[index]];
      return { ...current, courseIDs: next };
    });
  }

  async function saveBundle(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setNotice(null);
    const body = buildBundleMutation(draft);
    if (!body) {
      setError(copy.invalidOffer);
      return;
    }
    // The server requires a reason for every price change and answers a missing
    // one with a generic validation problem. Saying so here names the field the
    // Admin has to fill instead of making them guess which input was wrong.
    if (body.regular_price_minor_units !== undefined && draft.reason.trim() === "") {
      setError(copy.reasonRequired);
      return;
    }
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(copy.forbidden);
      return;
    }
    setBusy(true);
    try {
      const saved = draft.id
        ? await updateAdminBundle(draft.id, body, locale, csrf)
        : await createAdminBundle(body, locale, csrf);
      if (!saved) throw new Error("Bundle response was empty");
      // The editor is repopulated from the server's reply, not from what was
      // typed: the revision, the trimmed titles and the stored price are the
      // authoritative values, and the next save has to carry them.
      applyAuthoritativeBundle(saved);
      setNotice(copy.saved);
      await loadBundles();
    } catch (cause: unknown) {
      setError(problemMessage(cause, copy));
      // A conflict means the screen is behind the server; reload rather than
      // leaving a stale revision in the editor for the Admin to retry with.
      if (cause instanceof ProblemError && (cause.problem.status === 409 || cause.problem.status === 404)) {
        await loadBundles();
      }
    } finally {
      setBusy(false);
    }
  }

  /** Replaces the editor draft with exactly what the server returned. */
  function applyAuthoritativeBundle(bundle: AdminBundle) {
    setDraft({
      id: bundle.id,
      revision: bundle.revision,
      titleAr: bundle.title_ar,
      titleEn: bundle.title_en,
      descriptionAr: bundle.description_ar,
      descriptionEn: bundle.description_en,
      courseIDs: bundle.members.map((member) => member.course_id),
      regular: bundle.price ? String(bundle.price.regular_minor_units) : "",
      offer: bundle.price?.offer_minor_units == null ? "" : String(bundle.price.offer_minor_units),
      reason: "",
    });
  }

  async function editBundle(id: string) {
    setBusy(true); setPendingBundleID(id); setError(null);
    try {
      const bundle = await getAdminBundle(id, locale);
      if (!bundle) throw new Error("Bundle response was empty");
      applyAuthoritativeBundle(bundle);
      setCourses((current) => mergeCourseChoices(current, bundle.members.map((member) => ({
        id: member.course_id,
        title_ar: member.title_ar,
        title_en: member.title_en,
        instructor_display_name: member.instructor_display_name,
      }))));
      document.querySelector('[data-testid="bundle-editor"]')?.scrollIntoView({ behavior: "smooth" });
    } catch (cause: unknown) { setError(problemMessage(cause, copy)); }
    finally { setBusy(false); setPendingBundleID(null); }
  }

  async function changeLifecycle(bundle: AdminBundle, action: "publish" | "delist" | "archive") {
    const csrf = currentCSRFToken();
    if (!csrf) { setError(copy.forbidden); return; }
    setBusy(true); setPendingBundleID(bundle.id); setError(null); setNotice(null);
    try {
      const updated = await transitionAdminBundle(bundle.id, action, bundle.revision, locale, csrf);
      // Even on success the list is refetched rather than patched in place: the
      // lifecycle move can change eligibility and public visibility of rows the
      // response does not describe.
      if (updated && draft.id === updated.id) applyAuthoritativeBundle(updated);
      setNotice(copy.saved);
    } catch (cause: unknown) {
      setError(problemMessage(cause, copy));
    } finally {
      await loadBundles();
      setBusy(false); setPendingBundleID(null);
    }
  }

  /**
   * Hard-deletes one Bundle after an explicit, titled confirmation.
   *
   * The button is offered only for a Bundle the server reported as deletable,
   * but the refusal path is still handled: `deletable` was computed when the
   * list was fetched, and a purchase request can arrive in between. The server
   * is the authority either way.
   */
  async function confirmDelete(bundle: AdminBundle) {
    const csrf = currentCSRFToken();
    if (!csrf) { setError(copy.forbidden); return; }
    setBusy(true); setPendingBundleID(bundle.id); setError(null); setNotice(null);
    try {
      await deleteAdminBundle(bundle.id, bundle.revision, locale, csrf);
      setDeleteCandidate(null);
      if (draft.id === bundle.id) setDraft(emptyDraft());
      setNotice(copy.deleted);
    } catch (cause: unknown) {
      setError(problemMessage(cause, copy));
      setDeleteCandidate(null);
    } finally {
      await loadBundles();
      setBusy(false); setPendingBundleID(null);
    }
  }

  async function saveCoursePrice(clearOffer = false) {
    const csrf = currentCSRFToken();
    const regular = Number(courseRegular); const offer = courseOffer === "" ? null : Number(courseOffer);
    if (!csrf || !coursePriceID || !Number.isSafeInteger(regular) || regular < 0 || (!clearOffer && offer !== null && (!Number.isSafeInteger(offer) || offer <= 0 || offer >= regular))) { setError(copy.invalidOffer); return; }
    setBusy(true); setError(null);
    try {
      await setCoursePrice({ courseID: coursePriceID, priceMinorUnits: regular,
        offerPriceMinorUnits: clearOffer ? undefined : offer, clearOffer,
        reason: courseReason, locale, csrf });
      setNotice(copy.saved);
      await loadBundles();
      if (clearOffer) setCourseOffer("");
    } catch { setError(copy.failed); }
    finally { setBusy(false); }
  }

  return (
    <WorkspacePage>
      <WorkspacePageHeader title={copy.title} description={copy.intro} actions={<Button type="button" onClick={() => setDraft(emptyDraft())}>{copy.create}</Button>} />
      {error ? <div className="mt-5"><Alert tone="error" title={error} /></div> : null}
      {notice ? <div className="mt-5"><Alert tone="success" title={notice} /></div> : null}

      <WorkspaceSection testID="bundle-editor" title={draft.id ? copy.edit : copy.newTitle} className="mt-8">
        <form onSubmit={saveBundle} className="space-y-6">
          <p className="text-sm text-muted-foreground">{copy.draftHint}</p>
          <div className="grid gap-4 md:grid-cols-2">
            <Field htmlFor="bundle-title-ar" label={copy.titleAr}><Input id="bundle-title-ar" required dir="rtl" value={draft.titleAr} onChange={(event) => setDraft({ ...draft, titleAr: event.target.value })} /></Field>
            <Field htmlFor="bundle-title-en" label={copy.titleEn}><Input id="bundle-title-en" required dir="ltr" value={draft.titleEn} onChange={(event) => setDraft({ ...draft, titleEn: event.target.value })} /></Field>
            <Field htmlFor="bundle-description-ar" label={copy.descriptionAr}><Textarea id="bundle-description-ar" dir="rtl" value={draft.descriptionAr} onChange={(event) => setDraft({ ...draft, descriptionAr: event.target.value })} /></Field>
            <Field htmlFor="bundle-description-en" label={copy.descriptionEn}><Textarea id="bundle-description-en" dir="ltr" value={draft.descriptionEn} onChange={(event) => setDraft({ ...draft, descriptionEn: event.target.value })} /></Field>
            <Field htmlFor="bundle-regular" label={copy.regularPrice}><Input id="bundle-regular" type="number" min="0" step="1" value={draft.regular} onChange={(event) => setDraft({ ...draft, regular: event.target.value })} /></Field>
            <Field htmlFor="bundle-offer" label={copy.offerPrice}><Input id="bundle-offer" type="number" min="1" step="1" value={draft.offer} onChange={(event) => setDraft({ ...draft, offer: event.target.value })} /></Field>
          </div>
          <Field htmlFor="bundle-price-reason" label={copy.reason}><Input id="bundle-price-reason" value={draft.reason} onChange={(event) => setDraft({ ...draft, reason: event.target.value })} /></Field>
          <div>
            <label htmlFor="bundle-course-search" className="text-sm font-semibold">{copy.search}</label>
            <Input id="bundle-course-search" type="search" className="mt-2 max-w-xl" value={search} onChange={(event) => { setSearch(event.target.value); setCoursePage(1); }} />
            <fieldset className="mt-4 grid max-h-64 gap-2 overflow-y-auto rounded-lg border border-border p-3 sm:grid-cols-2">
              <legend className="px-1 text-sm font-semibold">{copy.courses}</legend>
              {courseLoading ? (
                <p className="text-sm text-muted-foreground" data-testid="bundle-course-loading">
                  {copy.coursesLoading}
                </p>
              ) : null}
              {courseError ? (
                <div className="sm:col-span-2">
                  <Alert tone="error" title={courseError}>
                    <Button type="button" variant="outline" size="sm" onClick={() => void loadCourses()}>
                      {copy.retryCourses}
                    </Button>
                  </Alert>
                </div>
              ) : null}
              {!courseLoading && !courseError && visibleCourses.length === 0 ? (
                <p className="text-sm text-muted-foreground">{copy.noCourses}</p>
              ) : null}
              {!courseLoading && !courseError ? visibleCourses.map((course) => (
                <label key={course.id} className="flex min-h-11 cursor-pointer items-center gap-3 rounded-md p-2 hover:bg-muted focus-within:outline focus-within:outline-2 focus-within:outline-primary">
                  <input type="checkbox" checked={draft.courseIDs.includes(course.id)} onChange={() => toggleCourse(course.id)} />
                  <span className="min-w-0"><bdi className="block truncate font-medium">{locale === "ar" ? course.titleAr : course.titleEn}</bdi><bdi className="block truncate text-xs text-muted-foreground">{locale === "ar" ? course.titleEn : course.titleAr}</bdi></span>
                </label>
              )) : null}
            </fieldset>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <Button type="button" variant="outline" size="sm" disabled={courseLoading || coursePage <= 1} onClick={() => setCoursePage((current) => current - 1)} data-testid="bundle-course-page-previous">
                {copy.previousCourses}
              </Button>
              <span className="text-sm text-muted-foreground" aria-live="polite" data-testid="bundle-course-page-number">{copy.coursePage.replace("{page}", String(coursePage))}</span>
              <Button type="button" variant="outline" size="sm" disabled={courseLoading || coursePage * coursePageSize >= courseTotal} onClick={() => setCoursePage((current) => current + 1)} data-testid="bundle-course-page-next">
                {copy.nextCourses}
              </Button>
            </div>
          </div>
          <div>
            <h3 className="text-sm font-semibold">{copy.selected} ({draft.courseIDs.length})</h3>
            <ol className="mt-3 space-y-2">
              {draft.courseIDs.map((id, index) => {
                const course = courses.find((item) => item.id === id);
                return <li key={id} className="flex min-h-11 items-center gap-2 rounded-md bg-muted px-3 py-2">
                  <span className="me-auto min-w-0 truncate"><bdi>{course ? (locale === "ar" ? course.titleAr : course.titleEn) : id}</bdi></span>
                  <Button type="button" size="sm" variant="ghost" aria-label={copy.moveUp} disabled={index === 0} onClick={() => moveCourse(index, -1)}><ArrowUp aria-hidden className="size-4" /></Button>
                  <Button type="button" size="sm" variant="ghost" aria-label={copy.moveDown} disabled={index === draft.courseIDs.length - 1} onClick={() => moveCourse(index, 1)}><ArrowDown aria-hidden className="size-4" /></Button>
                  <Button type="button" size="sm" variant="ghost" aria-label={copy.remove} onClick={() => toggleCourse(id)}><X aria-hidden className="size-4" /></Button>
                </li>;
              })}
            </ol>
          </div>
          <Button type="submit" disabled={busy}>{draft.id ? copy.update : copy.save}</Button>
        </form>
      </WorkspaceSection>

      <WorkspaceSection title={copy.title} className="mt-8">
        {bundles === null ? <p aria-live="polite" data-testid="bundle-list-loading">{copy.loading}</p> : null}
        {bundles?.length === 0 ? <p className="text-muted-foreground" data-testid="bundle-list-empty">{copy.empty}</p> : null}
        <ul className="space-y-3" data-testid="bundle-list">
          {bundles?.map((bundle) => {
            const saving = savingsMinorUnits(bundle);
            const rowBusy = busy && pendingBundleID === bundle.id;
            return (
              <li
                key={bundle.id}
                className="rounded-lg border border-border p-4"
                data-testid="bundle-row"
                data-bundle-id={bundle.id}
                data-lifecycle={bundle.lifecycle}
                data-deletable={bundle.deletable ? "true" : "false"}
                aria-busy={rowBusy}
              >
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0">
                    {/* The state, the word that explains it, and the reason the
                        Bundle is not publishable — never colour alone, and never
                        a state the reader has to infer from which buttons exist. */}
                    <StatusBadge
                      tone={LIFECYCLE_TONE[bundle.lifecycle]}
                      label={lifecycleLabel(bundle.lifecycle, copy)}
                      detail={lifecycleHelp(bundle.lifecycle, copy)}
                      labelTestID="bundle-lifecycle"
                    />
                    <h3 className="mt-2 font-display text-lg font-bold"><bdi>{locale === "ar" ? bundle.title_ar : bundle.title_en}</bdi></h3>
                    <p className="text-sm text-muted-foreground" data-testid="bundle-course-count">
                      {bundleCourseCount(bundle.course_count, t.bundles.courseCount)}
                    </p>

                    {bundle.members.length > 0 ? (
                      <ul className="mt-2 space-y-1 text-sm text-muted-foreground" data-testid="bundle-members">
                        {bundle.members.map((member) => (
                          <li key={member.course_id} className="truncate">
                            <bdi>{memberName(member, locale)}</bdi>
                            {member.effective_minor_units != null ? (
                              <span className="ms-2 tabular-nums"><bdi>{formatFils(member.effective_minor_units, locale)}</bdi></span>
                            ) : null}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="mt-2 text-sm text-muted-foreground" data-testid="bundle-no-members">{copy.noMembers}</p>
                    )}

                    {bundle.price ? (
                      <PriceDisplay
                        price={{ minor_units: bundle.price.effective_minor_units, regular_minor_units: bundle.price.regular_minor_units, offer_minor_units: bundle.price.offer_minor_units, currency: "KWD" }}
                        locale={locale}
                        className="mt-2"
                        compact
                      />
                    ) : null}
                    {bundle.member_total_minor_units != null ? (
                      <p className="mt-1 text-sm text-muted-foreground tabular-nums" data-testid="bundle-member-total">
                        {copy.memberTotal}: <bdi>{formatFils(bundle.member_total_minor_units, locale)}</bdi>
                      </p>
                    ) : null}
                    {saving !== null ? (
                      <p className="mt-1">
                        {/* The success token is only ever painted on the light
                            ground it was contrast-proved against. */}
                        <span className="inline-block rounded-md bg-gx-success-soft px-2 py-0.5 text-sm font-semibold text-gx-success-strong" data-testid="bundle-savings">
                          {copy.savings.replace("{amount}", formatFils(saving, locale))}
                        </span>
                      </p>
                    ) : null}
                    {!bundle.eligible && bundle.lifecycle !== "ARCHIVED" ? (
                      <p className="mt-2 text-sm text-muted-foreground" data-testid="bundle-not-eligible">{copy.notEligible}</p>
                    ) : null}
                    <p className="mt-2 text-xs text-muted-foreground">
                      <span data-testid="bundle-created">{copy.createdAt.replace("{date}", new Date(bundle.created_at).toLocaleDateString(locale))}</span>
                      {" · "}
                      <span data-testid="bundle-updated">{copy.updatedAt.replace("{date}", new Date(bundle.updated_at).toLocaleDateString(locale))}</span>
                    </p>
                    {rowBusy ? <p className="mt-2 text-sm text-muted-foreground" aria-live="polite" data-testid="bundle-row-pending">{copy.pending}</p> : null}
                  </div>

                  <div className="flex flex-wrap gap-2">
                    <Button type="button" variant="outline" disabled={busy} onClick={() => void editBundle(bundle.id)}>{copy.edit}</Button>
                    {/* Actions are derived from the authoritative lifecycle the
                        server reported, not from local optimism. DRAFT and
                        DELISTED are the two states the domain allows to publish
                        from; ARCHIVED is terminal. */}
                    {bundle.lifecycle === "DRAFT" || bundle.lifecycle === "DELISTED" ? (
                      <Button type="button" data-testid="bundle-publish" disabled={busy || !bundle.eligible} onClick={() => void changeLifecycle(bundle, "publish")}>{copy.publish}</Button>
                    ) : null}
                    {bundle.lifecycle === "PUBLISHED" ? (
                      <Button type="button" variant="outline" data-testid="bundle-delist" disabled={busy} onClick={() => void changeLifecycle(bundle, "delist")}>{copy.delist}</Button>
                    ) : null}
                    {bundle.lifecycle !== "ARCHIVED" ? (
                      <Button type="button" variant="outline" data-testid="bundle-archive" disabled={busy} onClick={() => void changeLifecycle(bundle, "archive")}>{copy.archive}</Button>
                    ) : null}
                    {/* Hard delete is offered only where the server says it is
                        available. Where it is not, the supported lifecycle
                        action stays and the reason is stated — no button that
                        exists only to fail. */}
                    {bundle.deletable ? (
                      <Button type="button" variant="outline" data-testid="bundle-delete" disabled={busy} onClick={() => { setError(null); setNotice(null); setDeleteCandidate(bundle); }}>{copy.delete}</Button>
                    ) : (
                      <p className="max-w-xs text-xs text-muted-foreground" data-testid="bundle-delete-unavailable">{copy.deleteUnavailable}</p>
                    )}
                  </div>
                </div>

                {deleteCandidate?.id === bundle.id ? (
                  <div
                    role="alertdialog"
                    aria-modal="false"
                    aria-labelledby={`bundle-delete-title-${bundle.id}`}
                    aria-describedby={`bundle-delete-body-${bundle.id}`}
                    className="mt-4 rounded-lg border border-destructive/40 bg-destructive/5 p-4"
                    data-testid="bundle-delete-dialog"
                  >
                    {/* The Bundle's own title is in the confirmation, so the
                        Admin confirms the Bundle they meant rather than the row
                        they happened to click. */}
                    <h4 id={`bundle-delete-title-${bundle.id}`} className="font-semibold">
                      {copy.deleteConfirmTitle.replace("{title}", locale === "ar" ? bundle.title_ar : bundle.title_en)}
                    </h4>
                    <p id={`bundle-delete-body-${bundle.id}`} className="mt-1 text-sm text-muted-foreground">{copy.deleteConfirmBody}</p>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Button type="button" data-testid="bundle-delete-confirm" disabled={busy} onClick={() => void confirmDelete(bundle)}>{copy.deleteConfirm}</Button>
                      <Button type="button" variant="outline" data-testid="bundle-delete-cancel" disabled={busy} onClick={() => setDeleteCandidate(null)}>{copy.deleteCancel}</Button>
                    </div>
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      </WorkspaceSection>

      <WorkspaceSection title={copy.pricingTitle} description={copy.pricingIntro} className="mt-8">
        <form onSubmit={(event) => { event.preventDefault(); void saveCoursePrice(); }} className="grid gap-4 md:grid-cols-2">
          <Field htmlFor="course-price-course" label={copy.selectCourse}><select id="course-price-course" className="min-h-11 rounded-md border border-input bg-background px-3" required value={coursePriceID} onChange={(event) => {
            const id = event.target.value; setCoursePriceID(id); const course = courses.find((item) => item.id === id);
            setCourseRegular(course?.price ? String(course.price.regular_minor_units ?? course.price.effective_minor_units) : ""); setCourseOffer(course?.price?.offer_minor_units == null ? "" : String(course.price.offer_minor_units));
          }}><option value="">—</option>{courses.map((course) => <option key={course.id} value={course.id}>{locale === "ar" ? course.titleAr : course.titleEn}</option>)}</select></Field>
          <Field htmlFor="course-regular" label={copy.regularPrice}><Input id="course-regular" type="number" min="0" step="1" required value={courseRegular} onChange={(event) => setCourseRegular(event.target.value)} /></Field>
          <Field htmlFor="course-offer" label={copy.offerPrice}><Input id="course-offer" type="number" min="1" step="1" value={courseOffer} onChange={(event) => setCourseOffer(event.target.value)} /></Field>
          <Field htmlFor="course-price-reason" label={copy.reason}><Input id="course-price-reason" required value={courseReason} onChange={(event) => setCourseReason(event.target.value)} /></Field>
          <div className="flex flex-wrap gap-2 md:col-span-2"><Button type="submit" disabled={busy}>{copy.savePricing}</Button><Button type="button" variant="outline" disabled={busy || courseOffer === ""} onClick={() => void saveCoursePrice(true)}>{copy.clearOffer}</Button></div>
        </form>
      </WorkspaceSection>
    </WorkspacePage>
  );
}
