import { CourseRoster } from "@/components/instructor/course-roster";
import { WorkspacePage } from "@/components/layout/workspace-page";

export default async function InstructorCourseStudentsPage({ params }: { params: Promise<{ courseId: string }> }) {
  const { courseId } = await params;
  return (
    <main id="main">
      <WorkspacePage>
        <CourseRoster courseID={courseId} />
      </WorkspacePage>
    </main>
  );
}
