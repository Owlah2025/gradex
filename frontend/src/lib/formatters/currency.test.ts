import { test } from "node:test";
import assert from "node:assert/strict";
import { formatFils } from "./currency";

test("formatFils formats integer fils into KWD display strings for English and Arabic locales", () => {
  const cases: Array<{
    fils: number | null | undefined;
    locale: "ar" | "en";
    expected: string;
  }> = [
    { fils: null, locale: "en", expected: "Unpriced" },
    { fils: null, locale: "ar", expected: "غير مخصص" },
    { fils: undefined, locale: "en", expected: "Unpriced" },
    { fils: undefined, locale: "ar", expected: "غير مخصص" },
    { fils: 0, locale: "en", expected: "0.000 KWD" },
    { fils: 0, locale: "ar", expected: "0.000 د.ك" },
    { fils: 1, locale: "en", expected: "0.001 KWD" },
    { fils: 1, locale: "ar", expected: "0.001 د.ك" },
    { fils: 1000, locale: "en", expected: "1.000 KWD" },
    { fils: 1000, locale: "ar", expected: "1.000 د.ك" },
    { fils: 25000, locale: "en", expected: "25.000 KWD" },
    { fils: 25000, locale: "ar", expected: "25.000 د.ك" },
  ];

  for (const tc of cases) {
    const result = formatFils(tc.fils, tc.locale);
    assert.equal(
      result,
      tc.expected,
      `formatFils(${tc.fils}, "${tc.locale}") got "${result}", want "${tc.expected}"`,
    );
  }
});
