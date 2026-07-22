import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Hero } from "@/components/sections/hero";
import { FeaturedCourses } from "@/components/sections/featured-courses";
import { WhyGradex } from "@/components/sections/why-gradex";
import { LearningExperience } from "@/components/sections/learning-experience";
import { InstructorSpotlight } from "@/components/sections/instructor-spotlight";
import { Testimonials } from "@/components/sections/testimonials";
import { Faq } from "@/components/sections/faq";
import { FinalCta } from "@/components/sections/final-cta";

/**
 * Landing page (SCREENS.md → Screen 1, Public). Pure composition of section
 * components — no markup or business logic lives here.
 */
export default function LandingPage() {
  return (
    <>
      <Navbar authState="guest" />
      <main id="main">
        <Hero />
        <FeaturedCourses />
        <WhyGradex />
        <LearningExperience />
        <InstructorSpotlight />
        <Testimonials />
        <Faq />
        <FinalCta />
      </main>
      <Footer />
    </>
  );
}
