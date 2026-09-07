import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * The Hero → Personalization → Courses stack.
 *
 * Three sections, one continuous story: each pins at the header edge while the next scrolls up and
 * covers it. The mechanics are entirely in CSS (`.stack*` in `globals.css`) — sticky siblings,
 * ascending `z-index`, opaque surfaces — so there is no scroll listener anywhere on this page and
 * the effect cannot cost a frame.
 *
 * It stops after the third layer on purpose. Everything below Courses is ordinary scrolling,
 * because the stack is here to carry the conversion journey and not to turn the page into a
 * slideshow.
 */
export function SectionStack({ children }: { children: React.ReactNode }) {
  return <div className="stack">{children}</div>;
}

const LAYERS = {
  1: { z: "z-[1]", covers: "", recedes: "stack-recedes-under-2" },
  2: { z: "z-[2]", covers: "stack-covers-2", recedes: "stack-recedes-under-3" },
  3: { z: "z-[3]", covers: "stack-covers-3", recedes: "" },
} as const;

/**
 * One layer of the stack.
 *
 * `id` lives here rather than on the section inside, because this is the box the reader is actually
 * scrolled to — by the header anchors, by the hero's call to action, and by the journey's own
 * hand-off — and it is the box carrying the `scroll-margin-top` that keeps the landing under the
 * header instead of behind it.
 *
 * `tail` marks the last layer: it does not pin, so it carries the whole stack away and hands the
 * page back to normal scrolling.
 *
 * `min-h` is not decoration. A layer shorter than the viewport would pin with the layer beneath it
 * still visible below its own bottom edge, which reads as a rendering fault rather than as depth.
 */
export const StackLayer = React.forwardRef<
  HTMLDivElement,
  {
    layer: 1 | 2 | 3;
    id?: string;
    tail?: boolean;
    className?: string;
    children: React.ReactNode;
  }
>(({ layer, id, tail = false, className, children }, ref) => {
  const config = LAYERS[layer];
  return (
    <div
      ref={ref}
      id={id}
      className={cn(
        // Only where the sections actually pin. Below `lg` they scroll one after another and a
        // forced viewport height would be padding around content that already fits.
        "lg:min-h-[calc(100svh-4rem)]",
        tail ? "stack-tail relative" : "stack-layer",
        config.z,
        config.covers,
        config.recedes,
        className,
      )}
    >
      {children}
    </div>
  );
});
StackLayer.displayName = "StackLayer";
