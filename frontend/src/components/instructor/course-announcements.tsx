"use client";

import Link from "next/link";
import { ArrowLeft, ArrowRight, Send } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { describeApiError } from "@/lib/api/api-error";
import { getCourseAnnouncements, createCourseAnnouncement, type CourseAnnouncement } from "@/lib/api/instructor";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";
import { isCourseNotPublishedError } from "./announcement-state";
import { announcementFieldLength, validateAnnouncementDraft, type AnnouncementField, type AnnouncementValidationErrors } from "./announcement-validation";

function announcementDate(value: string, locale: "ar" | "en"): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.valueOf())) return "—";
  return new Intl.DateTimeFormat(locale === "ar" ? "ar" : "en", { dateStyle: "medium", timeStyle: "short" }).format(parsed);
}

export function CourseAnnouncements({ courseID }: { courseID: string }) {
  const { locale, t } = useLocale();
  const labels = t.instructor.announcements;
  const Back = locale === "ar" ? ArrowRight : ArrowLeft;
  const [items, setItems] = useState<CourseAnnouncement[]>([]);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [moreLoadError, setMoreLoadError] = useState<string | null>(null);
  const [publishError, setPublishError] = useState<string | null>(null);
  const [courseNotPublished, setCourseNotPublished] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<AnnouncementValidationErrors>({});
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [publishing, setPublishing] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const result = await getCourseAnnouncements(courseID, locale, 1);
      setItems(result.items);
      setPage(result.page);
      setHasMore(result.has_more);
    } catch (cause) {
      setLoadError(describeApiError(cause, locale));
    } finally {
      setLoading(false);
    }
  }, [courseID, locale]);

  useEffect(() => { void load(); }, [load]);

  const loadMore = async () => {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    setMoreLoadError(null);
    try {
      const result = await getCourseAnnouncements(courseID, locale, page + 1);
      setItems((current) => [...current, ...result.items]);
      setPage(result.page);
      setHasMore(result.has_more);
    } catch (cause) {
      setMoreLoadError(describeApiError(cause, locale));
    } finally {
      setLoadingMore(false);
    }
  };

  const fieldMessage = (field: AnnouncementField): string | undefined => {
    const code = fieldErrors[field];
    if (!code) return undefined;
    if (field === "title") return code === "REQUIRED" ? labels.titleRequired : labels.titleTooLong;
    return code === "REQUIRED" ? labels.bodyRequired : labels.bodyTooLong;
  };

  async function publish(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setNotice(null);
    setPublishError(null);
    const validation = validateAnnouncementDraft(title, body);
    setFieldErrors(validation);
    if (Object.keys(validation).length > 0) return;
    const csrf = currentCSRFToken();
    if (!csrf) {
      setPublishError(t.instructor.media.csrfMissing);
      return;
    }
    setPublishing(true);
    try {
      const created = await createCourseAnnouncement({ courseID, locale, csrf, title: title.trim(), body: body.trim() });
      setItems((current) => [created, ...current]);
      setTitle("");
      setBody("");
      setFieldErrors({});
      setNotice(labels.published);
    } catch (cause) {
      if (isCourseNotPublishedError(cause)) {
        setCourseNotPublished(true);
        setPublishError(labels.courseNotPublishedBody);
      } else {
        setPublishError(describeApiError(cause, locale));
      }
    } finally {
      setPublishing(false);
    }
  }

  return (
    <WorkspacePage className="space-y-8">
      <WorkspacePageHeader
        breadcrumb={<Button asChild variant="ghost" size="sm" className="-ms-3"><Link href={`/${locale}/instructor`}><Back aria-hidden />{t.instructor.dashboard.title}</Link></Button>}
        title={labels.title}
        description={labels.intro}
      />
      {notice ? <Alert tone="success" title={notice} /> : null}
      {loadError ? <ErrorState title={labels.loadFailed} detail={loadError} retryLabel={labels.retry} onRetry={() => void load()} /> : null}
      {publishError ? <Alert tone="error" title={courseNotPublished ? labels.courseNotPublishedTitle : labels.publishFailed}>{publishError}</Alert> : null}
      <WorkspaceSection title={labels.composerTitle}>
        <Card>
          <CardHeader><CardTitle>{labels.composerTitle}</CardTitle></CardHeader>
          <CardContent>
            <form className="space-y-5" onSubmit={publish}>
              <fieldset disabled={publishing || courseNotPublished} className="space-y-5">
                <Field label={labels.titleLabel} htmlFor="announcement-title" error={fieldMessage("title")}>
                  <Input
                    id="announcement-title"
                    value={title}
                    maxLength={140}
                    placeholder={labels.titlePlaceholder}
                    aria-invalid={Boolean(fieldErrors.title)}
                    aria-describedby={fieldErrors.title ? "announcement-title-error" : undefined}
                    onChange={(event) => {
                      setTitle(event.target.value);
                      setFieldErrors((current) => ({ ...current, title: undefined }));
                    }}
                  />
                  <p className="mt-1 text-end text-xs text-muted-foreground">{labels.titleCount.replace("{count}", String(announcementFieldLength(title)))}</p>
                </Field>
                <Field label={labels.bodyLabel} htmlFor="announcement-body" error={fieldMessage("body")}>
                  <Textarea
                    id="announcement-body"
                    value={body}
                    maxLength={4000}
                    rows={6}
                    placeholder={labels.bodyPlaceholder}
                    aria-invalid={Boolean(fieldErrors.body)}
                    aria-describedby={fieldErrors.body ? "announcement-body-error" : undefined}
                    onChange={(event) => {
                      setBody(event.target.value);
                      setFieldErrors((current) => ({ ...current, body: undefined }));
                    }}
                  />
                  <p className="mt-1 text-end text-xs text-muted-foreground">{labels.bodyCount.replace("{count}", String(announcementFieldLength(body)))}</p>
                </Field>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <p className="text-sm text-muted-foreground">{labels.immutableHint}</p>
                  <Button type="submit">
                    <Send aria-hidden />
                    {publishing ? labels.publishing : labels.publish}
                  </Button>
                </div>
              </fieldset>
            </form>
          </CardContent>
        </Card>
      </WorkspaceSection>
      <WorkspaceSection title={labels.title}>
        {loading ? <LoadingState label={labels.loading} /> : null}
        {!loading && !loadError && items.length === 0 ? <EmptyState density="compact" title={labels.empty} /> : null}
        {!loading && !loadError && items.length > 0 ? (
          <div className="space-y-4">
            {items.map((item) => (
              <article key={item.id} className="rounded-lg border border-border bg-card p-5 shadow-sm">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <h2 className="min-w-0 font-display text-lg font-bold">{item.title}</h2>
                  <time className="text-sm text-muted-foreground" dateTime={item.published_at}>{labels.publishedAt}: {announcementDate(item.published_at, locale)}</time>
                </div>
                <p className="mt-3 whitespace-pre-wrap leading-7 text-foreground">{item.body}</p>
              </article>
            ))}
            {moreLoadError ? <Alert tone="error" title={labels.moreLoadFailed}>{moreLoadError}</Alert> : null}
            {hasMore ? (
              <div className="flex flex-wrap items-center gap-3">
                <Button type="button" variant="outline" disabled={loadingMore} onClick={() => void loadMore()}>
                  {loadingMore ? labels.loadingMore : labels.loadMore}
                </Button>
                <p className="text-sm text-muted-foreground">{labels.moreAvailable}</p>
              </div>
            ) : null}
          </div>
        ) : null}
      </WorkspaceSection>
    </WorkspacePage>
  );
}
