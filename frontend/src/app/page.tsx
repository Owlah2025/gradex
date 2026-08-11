import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Hero } from "@/components/sections/hero";
import { FeaturedCourses } from "@/components/sections/featured-courses";
import { WhyGradex } from "@/components/sections/why-gradex";
import { LearningExperience } from "@/components/sections/learning-experience";
import { Faq } from "@/components/sections/faq";
import { FinalCta } from "@/components/sections/final-cta";

/**
 * Landing page (SCREENS.md → Screen 1, Public). Pure composition of section
 * components — no markup or business logic lives here.
 */
export default function LandingPage() {
  return (
    <>
      {/* No authState override: the header follows the real session, so a
          signed-in visitor sees dashboard/sign-out instead of sign-in. */}
      <Navbar />
      <main id="main">
        <Hero />
        <FeaturedCourses />
        <WhyGradex />
        <LearningExperience />
        <Faq />
        <FinalCta />
      </main>
      <Footer />
    </>
  );
}
