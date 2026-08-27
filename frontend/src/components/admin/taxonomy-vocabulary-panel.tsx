"use client";

import { useCallback, useEffect, useState } from "react";
import { getTaxonomyTerms, type TaxonomyTerm } from "@/lib/api/catalog";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ErrorState } from "@/components/common/error-state";
import { WorkspaceSection } from "@/components/layout/workspace-page";
import { TaxonomyTermManagement } from "./taxonomy-term-management";

export function TaxonomyVocabularyPanel() {
  const { locale, t } = useLocale();
  const copy = t.adminTaxonomy;
  const [terms, setTerms] = useState<TaxonomyTerm[]>([]);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setError(null);
    try {
      setTerms(await getTaxonomyTerms(locale));
    } catch (loadError) {
      setError(describeApiError(loadError, locale) || copy.loadFailed);
    }
  }, [copy.loadFailed, locale]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return (
    <WorkspaceSection title={copy.title} description={copy.lead} testID="taxonomy-vocabulary">
      {error ? (
        <ErrorState
          title={copy.loadFailed}
          detail={error}
          retryLabel={copy.retry}
          onRetry={() => void refresh()}
          testID="taxonomy-vocabulary-failed"
        />
      ) : (
        // The area is the vocabulary; the form inside it is what a reader is actually editing. The
        // nesting is what lets a screen-reader user move to the editor rather than to the whole
        // catalogue region.
        <WorkspaceSection title={copy.panelTitle} headingLevel="h3" className="mt-0">
          <TaxonomyTermManagement terms={terms} refresh={refresh} />
        </WorkspaceSection>
      )}
    </WorkspaceSection>
  );
}
