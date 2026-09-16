import assert from "node:assert/strict";
import test from "node:test";
import {
  BUNDLE_STACK_LIMIT,
  bundleCourseCount,
  bundleDetailHref,
  bundleOverflowLabel,
  bundleStackLayout,
  clampActiveMember,
} from "./bundle-presentation";

test("Bundle navigation and Course counts are localized without changing identity", () => {
  assert.equal(bundleCourseCount(3, "{count} Courses"), "3 Courses");
  assert.equal(bundleCourseCount(3, "{count} كورسات"), "3 كورسات");
  assert.equal(bundleDetailHref("en", "bundle-bundleid"), "/en/catalog/bundles/bundle-bundleid");
  assert.equal(bundleDetailHref("ar", "bundle name"), "/ar/catalog/bundles/bundle%20name");
  assert.equal(bundleOverflowLabel(2, "+{count} more"), "+2 more");
});

test("a single member is one artwork, not a stack of one", () => {
  const layout = bundleStackLayout(1, 0);
  assert.equal(layout.isSingle, true);
  assert.equal(layout.slots.length, 1);
  assert.equal(layout.overflow, 0);
  assert.equal(layout.slots[0].widthPercent, 100);
  assert.equal(layout.slots[0].insetInlineStartPercent, 0);
});

// The regression the whole rewrite exists for: a 2-member Bundle must be a
// deliberate 2-card layout, and either member must be promotable to the front.
test("a two-member stack promotes either member to the front", () => {
  const initial = bundleStackLayout(2, 0);
  assert.equal(initial.slots.length, 2);
  assert.equal(initial.slots[0].isActive, true);
  assert.equal(initial.slots[1].isActive, false);
  assert.ok(initial.slots[0].zIndex > initial.slots[1].zIndex);
  // Both cards are genuinely on screen at distinct offsets, so the background
  // one is reachable by a pointer rather than fully hidden behind the front.
  assert.notEqual(initial.slots[0].insetInlineStartPercent, initial.slots[1].insetInlineStartPercent);
  assert.ok(initial.slots[1].insetInlineStartPercent > 0);

  const promoted = bundleStackLayout(2, 1);
  assert.equal(promoted.slots[1].isActive, true);
  assert.ok(promoted.slots[1].zIndex > promoted.slots[0].zIndex);
  assert.equal(promoted.slots[1].insetInlineStartPercent, 0);
  // Member identity is unchanged by promotion: index 1 is still index 1.
  assert.equal(promoted.slots[1].index, 1);
});

test("the active member owns the highest z-index for every supported count", () => {
  for (const count of [2, 3, 4, 7]) {
    const visible = Math.min(count, BUNDLE_STACK_LIMIT);
    for (let active = 0; active < visible; active += 1) {
      const layout = bundleStackLayout(count, active);
      const front = layout.slots.find((slot) => slot.isActive);
      assert.ok(front, `no active slot for count ${count} active ${active}`);
      assert.equal(front.index, active);
      for (const slot of layout.slots) {
        if (slot.index === active) continue;
        assert.ok(front.zIndex > slot.zIndex, `count ${count}: active is not front-most`);
        // No member is stacked exactly on top of another, so every inactive
        // member keeps a clickable sliver.
        assert.notEqual(slot.insetInlineStartPercent, front.insetInlineStartPercent);
      }
      // Offsets are unique, so no two cards share a slot at any count.
      const offsets = layout.slots.map((slot) => slot.insetInlineStartPercent);
      assert.equal(new Set(offsets).size, offsets.length);
      // The stack fills its row exactly rather than overflowing the card.
      const rightMost = Math.max(...layout.slots.map((slot) => slot.insetInlineStartPercent + slot.widthPercent));
      assert.ok(Math.abs(rightMost - 100) < 0.001, `count ${count}: stack spans ${rightMost}%`);
    }
  }
});

test("more members than the stack shows are reported, never crammed in", () => {
  const layout = bundleStackLayout(7, 2);
  assert.equal(layout.slots.length, BUNDLE_STACK_LIMIT);
  assert.equal(layout.overflow, 3);
  assert.equal(bundleOverflowLabel(layout.overflow, "+{count} more"), "+3 more");
});

test("an out-of-range active member falls back to the front rather than to nothing", () => {
  assert.equal(clampActiveMember(2, 5), 0);
  assert.equal(clampActiveMember(2, -1), 0);
  assert.equal(clampActiveMember(0, 0), 0);
  assert.equal(clampActiveMember(3, 2), 2);
  // A Bundle whose membership shrank must not keep pointing past the end.
  assert.equal(clampActiveMember(2, 2), 0);
  const layout = bundleStackLayout(2, 9);
  assert.equal(layout.slots[0].isActive, true);
});

test("an empty membership produces no stack and no crash", () => {
  const layout = bundleStackLayout(0, 0);
  assert.deepEqual(layout, { slots: [], overflow: 0, isSingle: false });
});
