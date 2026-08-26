import { RoutedCourseReview } from "@/components/admin/routed-course-review";

export default async function AdminCourseReviewPage({
  params,
}: {
  params: Promise<{ courseId: string }>;
}) {
  const { courseId } = await params;
  return (
    <main id="main">
      <RoutedCourseReview courseID={courseId} />
    </main>
  );
}
