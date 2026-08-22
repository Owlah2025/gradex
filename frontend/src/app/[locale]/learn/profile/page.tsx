import { AcademicProfileForm } from "@/components/learning/academic-profile-form";

/** The Student's own profile surface, where the academic profile can be edited later. */
export default function StudentProfilePage() {
  return (
    <main id="main" className="mx-auto min-h-screen max-w-3xl px-5 py-10 sm:px-6">
      <AcademicProfileForm mode="edit" />
    </main>
  );
}
