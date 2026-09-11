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
  getAdminBundleCourses,
  getAdminBundle,
  listAdminBundles,
  setCoursePrice,
  transitionAdminBundle,
  updateAdminBundle,
  type AdminBundleCourseOption,
  type BundleMutation,
  type AdminBundle,
} from "@/lib/api/catalog";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

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

  const loadBundles = useCallback(async () => {
    setError(null);
    try {
      const bundleResult = await listAdminBundles(locale);
      setBundles(bundleResult?.items ?? []);
    } catch {
      setError(copy.failed);
    }
  }, [copy.failed, locale]);

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
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(copy.failed);
      return;
    }
    setBusy(true);
    try {
      const saved = draft.id
        ? await updateAdminBundle(draft.id, body, locale, csrf)
        : await createAdminBundle(body, locale, csrf);
      if (!saved) throw new Error("Bundle response was empty");
      setDraft({ ...draft, id: saved.id, revision: saved.revision });
      setNotice(copy.saved);
      await loadBundles();
    } catch {
      setError(copy.failed);
    } finally {
      setBusy(false);
    }
  }

  async function editBundle(id: string) {
    setBusy(true); setError(null);
    try {
      const bundle = await getAdminBundle(id, locale);
      if (!bundle) throw new Error("Bundle response was empty");
      setDraft({ id: bundle.id, revision: bundle.revision, titleAr: bundle.title_ar, titleEn: bundle.title_en,
        descriptionAr: bundle.description_ar, descriptionEn: bundle.description_en,
        courseIDs: bundle.members.map((member) => member.course_id),
        regular: bundle.price ? String(bundle.price.regular_minor_units) : "",
        offer: bundle.price?.offer_minor_units == null ? "" : String(bundle.price.offer_minor_units), reason: "" });
      document.querySelector('[data-testid="bundle-editor"]')?.scrollIntoView({ behavior: "smooth" });
    } catch { setError(copy.failed); }
    finally { setBusy(false); }
  }

  async function changeLifecycle(bundle: AdminBundle, action: "publish" | "delist" | "archive") {
    const csrf = currentCSRFToken(); if (!csrf) return;
    setBusy(true); setError(null);
    try {
      await transitionAdminBundle(bundle.id, action, bundle.revision, locale, csrf);
      await loadBundles();
    } catch {
      setError(copy.failed);
    } finally {
      setBusy(false);
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
        {bundles === null ? <p aria-live="polite">{copy.loading}</p> : null}
        {bundles?.length === 0 ? <p className="text-muted-foreground">{copy.empty}</p> : null}
        <ul className="space-y-3">
          {bundles?.map((bundle) => <li key={bundle.id} className="rounded-lg border border-border p-4">
            <div className="flex flex-wrap items-start justify-between gap-4"><div>
              <StatusBadge tone={bundle.lifecycle === "PUBLISHED" && bundle.eligible ? "success" : "neutral"} label={bundle.lifecycle} />
              <h3 className="mt-2 font-display text-lg font-bold"><bdi>{locale === "ar" ? bundle.title_ar : bundle.title_en}</bdi></h3>
              <p className="text-sm text-muted-foreground">{bundleCourseCount(bundle.course_count, t.bundles.courseCount)}</p>
              {bundle.price ? <PriceDisplay price={{ minor_units: bundle.price.effective_minor_units, regular_minor_units: bundle.price.regular_minor_units, offer_minor_units: bundle.price.offer_minor_units, currency: "KWD" }} locale={locale} className="mt-2" compact /> : null}
            </div><div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" onClick={() => void editBundle(bundle.id)}>{copy.edit}</Button>
              {bundle.lifecycle === "DRAFT" || bundle.lifecycle === "DELISTED" ? <Button type="button" disabled={busy || !bundle.eligible || !bundle.price} onClick={() => void changeLifecycle(bundle, "publish")}>{copy.publish}</Button> : null}
              {bundle.lifecycle === "PUBLISHED" ? <Button type="button" variant="outline" disabled={busy} onClick={() => void changeLifecycle(bundle, "delist")}>{copy.delist}</Button> : null}
              {bundle.lifecycle !== "ARCHIVED" ? <Button type="button" variant="outline" disabled={busy} onClick={() => void changeLifecycle(bundle, "archive")}>{copy.archive}</Button> : null}
            </div></div>
          </li>)}
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
