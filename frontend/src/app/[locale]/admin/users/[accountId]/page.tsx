import { AdminUserDetail } from "@/components/admin/admin-user-detail";

export default async function AdminUserDetailPage({
  params,
}: {
  params: Promise<{ locale: string; accountId: string }>;
}) {
  const { accountId } = await params;
  return (
    <main id="main">
      <AdminUserDetail accountID={accountId} />
    </main>
  );
}
