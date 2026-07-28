import { ReviewQueue } from "@/components/admin/review-queue";
import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";

export default function AdminCatalogReviewQueuePage() {
  return (
    <>
      <Navbar />
      <main id="main">
        <ReviewQueue />
      </main>
      <Footer />
    </>
  );
}
