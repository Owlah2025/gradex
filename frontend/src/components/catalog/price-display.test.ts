import assert from "node:assert/strict";
import test from "node:test";
import { pricePresentation } from "./price-presentation";

test("a valid offer becomes the effective price while retaining regular", () => {
  assert.deepEqual(pricePresentation({ minor_units: 49000, regular_minor_units: 65000, offer_minor_units: 49000, currency: "KWD" }), {
    regular: 65000, effective: 49000, hasOffer: true,
  });
});

test("missing or invalid offers cannot create an artificial discount", () => {
  assert.deepEqual(pricePresentation({ minor_units: 70000, regular_minor_units: 70000, currency: "KWD" }), {
    regular: 70000, effective: 70000, hasOffer: false,
  });
  assert.deepEqual(pricePresentation({ minor_units: 70000, regular_minor_units: 70000, offer_minor_units: 70000, currency: "KWD" }), {
    regular: 70000, effective: 70000, hasOffer: false,
  });
});
