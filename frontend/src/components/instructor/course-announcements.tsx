"use client";

import Link from "next/link";
import { ArrowLeft, ArrowRight, Send } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { describeApiError } from "@/lib/api/api-error";
import { createCourseAnnouncement, getCourseAnnouncements, type CourseAnnouncement } from "@/lib/api/instructor";
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
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [publishing, setPublishing] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setItems(await getCourseAnnouncements(courseID, locale));
    } catch (cause) {
      setError(describeApiError(cause, locale));
    } finally {
      setLoading(false);
    }
  }, [courseID, locale]);

  useEffect(() => { void load(); }, [load]);

  async function publish(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setNotice(null);
    const cleanTitle = title.trim();
    const cleanBody = body.trim();
    if (!cleanTitle || Array.from(cleanTitle).length > 140 || !cleanBody || Array.from(cleanBody).length > 4000) {
      setError(locale === "ar" ? "تحقق من طول العنوان والرسالة قبل النشر." : "Check the title and message length before publishing.");
      return;
    }
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(locale === "ar" ? "انتهت جلستك. أعد تحميل الصفحة وسجّل الدخول مرة أخرى." : "Your session expired. Reload and sign in again.");
      return;
    }
    setPublishing(true);
    setError(null);
    try {
      const created = await createCourseAnnouncement({ courseID, locale, csrf, title: cleanTitle, body: cleanBody });
      setItems((current) => [created, ...current]);
      setTitle("");
      setBody("");
      setNotice(labels.published);
    } catch (cause) {
      setError(describeApiError(cause, locale));
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
      {error ? <ErrorState title={labels.loadFailed} detail={error} retryLabel={labels.retry} onRetry={() => void load()} /> : null}
      <WorkspaceSection title={labels.composerTitle}>
        <Card>
          <CardHeader><CardTitle>{labels.composerTitle}</CardTitle></CardHeader>
          <CardContent>
            <form className="space-y-5" onSubmit={publish}>
              <Field label={labels.titleLabel} htmlFor="announcement-title">
                <Input id="announcement-title" value={title} maxLength={140} placeholder={labels.titlePlaceholder} onChange={(event) => setTitle(event.target.value)} />
                <p className="mt-1 text-end text-xs text-muted-foreground">{labels.titleCount.replace("{count}", String(Array.from(title).length))}</p>
              </Field>
              <Field label={labels.bodyLabel} htmlFor="announcement-body">
                <Textarea id="announcement-body" value={body} maxLength={4000} rows={6} placeholder={labels.bodyPlaceholder} onChange={(event) => setBody(event.target.value)} />
                <p className="mt-1 text-end text-xs text-muted-foreground">{labels.bodyCount.replace("{count}", String(Array.from(body).length))}</p>
              </Field>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-sm text-muted-foreground">{labels.immutableHint}</p>
                <Button type="submit" disabled={publishing}>
                  <Send aria-hidden />
                  {publishing ? labels.publishing : labels.publish}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      </WorkspaceSection>
      <WorkspaceSection title={labels.title}>
        {loading ? <LoadingState label={labels.loading} /> : null}
        {!loading && !error && items.length === 0 ? <EmptyState density="compact" title={labels.empty} /> : null}
        {!loading && !error && items.length > 0 ? (
          <div className="space-y-4">
            {items.map((item) => (
              <article key={item.id} className="rounded-lg border border-border bg-card p-5 shadow-sm">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <h2 className="font-display text-lg font-bold">{item.title}</h2>
                  <time className="text-sm text-muted-foreground" dateTime={item.published_at}>{labels.publishedAt}: {announcementDate(item.published_at, locale)}</time>
                </div>
                <p className="mt-3 whitespace-pre-wrap leading-7 text-foreground">{item.body}</p>
              </article>
            ))}
          </div>
        ) : null}
      </WorkspaceSection>
    </WorkspacePage>
  );
}
