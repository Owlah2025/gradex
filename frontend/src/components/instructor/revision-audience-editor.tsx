"use client";

import { useEffect, useState } from "react";
import { programName, type AuthoringSubject } from "@/lib/api/authoring-academic";
import type { RevisionAudienceWire } from "@/lib/api/catalog";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";

type AudienceLabels = Dictionary["instructor"]["academic"]["audience"];

type RevisionAudienceEditorProps = {
  subject: AuthoringSubject;
  audience?: RevisionAudienceWire;
  editable: boolean;
  busy: boolean;
  labels: AudienceLabels;
  onCustomize: (programIDs: string[]) => void;
  onReset: () => void;
};

/**
 * Which Programmes this revision is aimed at.
 *
 * The copy deliberately avoids the word "audience". It is the server's field name and a marketing
 * term, and neither is how an instructor describes the students who will find their course; the
 * question they are answering is which programmes this is for. The wire vocabulary — AUTOMATIC and
 * CUSTOMIZED — never reaches the screen.
 *
 * Empty is a valid, submittable state: zero explicit rows means automatic inference, and the
 * submission validator accepts that even when inference resolves to nothing. So an empty list is
 * explained rather than presented as a problem to fix.
 */
export function RevisionAudienceEditor({
  subject,
  audience,
  editable,
  busy,
  labels,
  onCustomize,
  onReset,
}: RevisionAudienceEditorProps) {
  const { locale } = useLocale();
  const customized = audience?.mode === "CUSTOMIZED";
  const effectivePrograms = customized ? audience.programs : subject.programs;
  const [editing, setEditing] = useState(false);
  const [selectedProgramIDs, setSelectedProgramIDs] = useState<string[]>([]);

  useEffect(() => {
    setSelectedProgramIDs((audience?.programs ?? []).map((program) => program.program_id));
  }, [audience]);

  const begin = () => {
    if (!customized) setSelectedProgramIDs(subject.programs.map((program) => program.program_id));
    setEditing(true);
  };

  const toggle = (programID: string, checked: boolean) => {
    setSelectedProgramIDs((current) =>
      checked ? [...current, programID] : current.filter((id) => id !== programID),
    );
  };

  return (
    <div className="space-y-3">
      {effectivePrograms.length > 0 ? (
        <div data-testid="academic-course-audience">
          <p
            className="font-display text-xs font-bold uppercase tracking-wide text-muted-foreground"
            data-testid="academic-course-audience-mode"
          >
            {customized ? labels.customized : labels.automatic}
          </p>
          <ul className="mt-1.5 space-y-0.5 text-sm text-foreground">
            {effectivePrograms.map((program) => (
              <li key={program.program_id}>
                <bdi>{programName(program, locale)}</bdi>
              </li>
            ))}
          </ul>
          <p className="mt-1.5 text-xs text-muted-foreground">
            {customized ? labels.customizedNote : labels.automaticNote}
          </p>
        </div>
      ) : (
        <p className="text-sm text-muted-foreground" data-testid="academic-course-audience-empty">
          {labels.empty}
        </p>
      )}

      {editable && !editing && !customized && subject.programs.length > 0 && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={begin}
          disabled={busy}
          data-testid="academic-course-customize-audience"
        >
          {labels.customize}
        </Button>
      )}

      {editable && customized && !editing && (
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={begin}
            disabled={busy}
            data-testid="academic-course-edit-audience"
          >
            {labels.edit}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onReset}
            disabled={busy}
            data-testid="academic-course-use-automatic-audience"
          >
            {labels.useAutomatic}
          </Button>
        </div>
      )}

      {editable && editing && (
        <fieldset className="space-y-2" data-testid="academic-course-audience-editor">
          {/* A group of checkboxes needs a group name, not just a name per box. */}
          <legend className="font-display text-xs font-bold uppercase tracking-wide text-muted-foreground">
            {labels.legend}
          </legend>
          {subject.programs.map((program) => (
            <label
              key={program.program_id}
              className="flex items-center gap-2 text-sm text-foreground"
            >
              <input
                type="checkbox"
                checked={selectedProgramIDs.includes(program.program_id)}
                onChange={(event) => toggle(program.program_id, event.target.checked)}
                disabled={busy}
                className="size-4 rounded border-input accent-primary"
                data-testid="academic-course-audience-option"
              />
              <bdi>{programName(program, locale)}</bdi>
            </label>
          ))}
          <div className="flex flex-wrap gap-2 pt-1">
            <Button
              type="button"
              size="sm"
              disabled={busy || selectedProgramIDs.length === 0}
              onClick={() => {
                onCustomize(selectedProgramIDs);
                setEditing(false);
              }}
              data-testid="academic-course-save-audience"
            >
              {labels.save}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setEditing(false)}
              data-testid="academic-course-cancel-audience"
            >
              {labels.cancel}
            </Button>
          </div>
        </fieldset>
      )}
    </div>
  );
}
