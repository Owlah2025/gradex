import { CourseBuilder } from "@/components/instructor/course-builder";
import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";

export default function InstructorCoursesPage() {
  return (
    <>
      <Navbar />
      <main id="main">
        <CourseBuilder />
      </main>
      <Footer />
    </>
  );
}
