import { CourseAnnouncements } from "@/components/instructor/course-announcements";

export default async function InstructorCourseAnnouncementsPage({ params }: { params: Promise<{ courseId: string }> }) {
  const { courseId } = await params;
  return (
    <main id="main">
      <CourseAnnouncements courseID={courseId} />
    </main>
  );
}
