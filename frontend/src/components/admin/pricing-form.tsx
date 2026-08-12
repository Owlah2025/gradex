"use client";

import React, { useState } from "react";
import { formatFils } from "@/lib/formatters/currency";
import { setCoursePrice, setSectionPrice, type SectionWire } from "@/lib/api/catalog";
import { currentCSRFToken } from "@/lib/identity/session";
import { submittedSectionLabel } from "./pricing-sections";

export interface PricingFormProps {
  courseID: string;
  locale: "ar" | "en";
  sections: SectionWire[];
  onSuccess: () => Promise<void>;
}

export function PricingForm({ courseID, locale, sections, onSuccess }: PricingFormProps) {
  const isAr = locale === "ar";

  const [targetType, setTargetType] = useState<"COURSE" | "SECTION">("COURSE");
  const [selectedSectionID, setSelectedSectionID] = useState("");
  const [priceFilsInput, setPriceFilsInput] = useState<number | "">(25000);
  const [reasonInput, setReasonInput] = useState("");

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSuccessMsg(null);

    if (priceFilsInput === "" || priceFilsInput < 0 || !Number.isInteger(Number(priceFilsInput))) {
      setFormError(
        isAr
          ? "السعر يجب أن يكون رقماً صحيحاً غير سالب بالفلس (أصغر وحدة نقدية)"
          : "Price must be a non-negative integer in fils (minor currency units)"
      );
      return;
    }
    if (!reasonInput.trim()) {
      setFormError(
        isAr
          ? "سبب التعديل إجباري لتسجيل السجل التاريخي"
          : "Reason for change is required for audit logging"
      );
      return;
    }
    if (targetType === "SECTION" && !selectedSectionID) {
      setFormError(
        isAr ? "اختر قسماً من المراجعة المُرسلة" : "Select a Section from the submitted revision"
      );
      return;
    }

    const csrf = currentCSRFToken();
    if (!csrf) {
      setFormError(
        isAr
          ? "رمز CSRF الجلسة مفقود. يرجى إعادة تسجيل الدخول"
          : "Session CSRF token missing. Please re-authenticate"
      );
      return;
    }

    setIsSubmitting(true);
    try {
      const fils = Number(priceFilsInput);
      const reason = reasonInput.trim();

      if (targetType === "COURSE") {
        await setCoursePrice({
          courseID,
          priceMinorUnits: fils,
          reason,
          locale,
          csrf,
        });
      } else {
        await setSectionPrice({
          courseID,
          sectionID: selectedSectionID,
          priceMinorUnits: fils,
          reason,
          locale,
          csrf,
        });
      }

      setSuccessMsg(
        isAr
          ? `تم تحديث سعر ${targetType === "COURSE" ? "الدورة" : "القسم"} بنجاح`
          : `Successfully updated ${targetType === "COURSE" ? "Course" : "Section"} price`
      );
      setReasonInput("");
      await onSuccess();
    } catch (err: unknown) {
      const msg =
        err instanceof Error
          ? err.message
          : isAr
            ? "حدث خطأ أثناء حفظ السعر"
            : "An error occurred while setting the price";
      setFormError(msg);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="space-y-4">
      {successMsg && (
        <div data-testid="pricing-success" className="p-3 bg-emerald-50 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-800 rounded-lg text-xs font-medium">
          {successMsg}
        </div>
      )}

      <form onSubmit={handleSubmit} className="bg-slate-50 dark:bg-slate-800/50 p-4 rounded-lg border border-slate-200 dark:border-slate-700 space-y-4">
        <h4 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
          {isAr ? "تحديد سعر جديد (مسؤول النظام)" : "Set Price (Admin Only)"}
        </h4>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-semibold mb-1">
              {isAr ? "نطاق التسعير:" : "Pricing Scope:"}
            </label>
            <select
              value={targetType}
              data-testid="pricing-scope-select"
              onChange={(e) => {
                const nextTarget = e.target.value as "COURSE" | "SECTION";
                setTargetType(nextTarget);
                if (nextTarget === "COURSE") setSelectedSectionID("");
              }}
              className="w-full p-2 border rounded text-xs bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-700"
            >
              <option value="COURSE">{isAr ? "الدورة كاملة (Course)" : "Course Level"}</option>
              <option value="SECTION" disabled={sections.length === 0}>{isAr ? "قسم كورس (Section)" : "Section Level"}</option>
            </select>
          </div>

          {targetType === "SECTION" && (
            <div>
              <label className="block text-xs font-semibold mb-1">
                {isAr ? "القسم المُرسل:" : "Submitted Section:"}
              </label>
              <select
                value={selectedSectionID}
                onChange={(e) => setSelectedSectionID(e.target.value)}
                data-testid="pricing-section-select"
                className="w-full p-2 border rounded text-xs bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-700"
              >
                <option value="">{isAr ? "اختر قسماً" : "Select a Section"}</option>
                {sections.map((section) => (
                  <option key={section.id} value={section.id}>{submittedSectionLabel(section, locale)}</option>
                ))}
              </select>
            </div>
          )}

          <div>
            <label className="block text-xs font-semibold mb-1">
              {isAr ? "السعر بالفلس (Integer Fils):" : "Price in Fils (Integer Fils):"}
            </label>
            <input
              type="number"
              min={0}
              step={1}
              value={priceFilsInput}
              data-testid="pricing-amount"
              onChange={(e) => setPriceFilsInput(e.target.value === "" ? "" : Number(e.target.value))}
              className="w-full p-2 border rounded text-xs bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-700 font-mono"
            />
            <span className="text-[10px] text-slate-500 font-mono block mt-0.5">
              {priceFilsInput !== ""
                ? `= ${formatFils(Number(priceFilsInput), locale)} (${isAr ? "١ د.ك = ١٠٠٠ فلس" : "1 KWD = 1000 fils"})`
                : ""}
            </span>
          </div>

          <div className="sm:col-span-2">
            <label className="block text-xs font-semibold mb-1">
              {isAr ? "سبب التغيير (إجباري):" : "Reason for Change (Mandatory):"}
            </label>
            <input
              type="text"
              value={reasonInput}
              data-testid="pricing-reason"
              onChange={(e) => setReasonInput(e.target.value)}
              placeholder={isAr ? "مثال: تحديث تسعير الفصل الدراسي" : "e.g., Semester pricing adjustment"}
              className="w-full p-2 border rounded text-xs bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-700"
            />
          </div>
        </div>

        {formError && <p className="text-xs text-rose-600 font-medium">{formError}</p>}

        <button
          type="submit"
          data-testid="pricing-submit"
          disabled={isSubmitting}
          className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded text-xs font-semibold transition"
        >
          {isSubmitting
            ? isAr
              ? "جاري الحفظ..."
              : "Submitting..."
            : isAr
              ? "حفظ وتوثيق السعر"
              : "Save & Audit Price"}
        </button>
      </form>
    </div>
  );
}
