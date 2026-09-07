import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Hero } from "@/components/sections/hero";
import { FeaturedCourses } from "@/components/sections/featured-courses";
import { WhyGradex } from "@/components/sections/why-gradex";
import { LearningExperience } from "@/components/sections/learning-experience";
import { Faq } from "@/components/sections/faq";
import { FinalCta } from "@/components/sections/final-cta";
import { LandingJourneyProvider } from "@/components/landing/landing-journey";
import { LandingStack } from "@/components/landing/landing-stack";

/**
 * Landing page (SCREENS.md → Screen 1, Public). Pure composition of section
 * components — no markup or business logic lives here.
 *
 * The hero and the personalised courses are one stacked story: the hero asks which university the
 * visitor attends and the courses below answer it, with the second covering the first as the reader
 * scrolls. `LandingStack` owns that arrangement; everything below it scrolls normally.
 *
 * `AcademicContextPanel` is deliberately not mounted. The question it asked is now asked in the
 * hero, where the visitor is when they decide whether Gradex is for them, and asking it a second
 * time on the way to the answer would be the same question twice on one page. The component is
 * unchanged and still serves any surface that wants a full-width version of it.
 */
export default function LandingPage() {
  return (
    <>
      {/* No authState override: the header follows the real session, so a
          signed-in visitor sees dashboard/sign-out instead of sign-in. */}
      <Navbar />
      <main id="main">
        <LandingJourneyProvider>
          <LandingStack hero={<Hero />} courses={<FeaturedCourses />} />
        </LandingJourneyProvider>
        <WhyGradex />
        <LearningExperience />
        <Faq />
        <FinalCta />
      </main>
      <Footer />
    </>
  );
}
