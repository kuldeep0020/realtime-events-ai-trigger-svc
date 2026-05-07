import { redirect } from "next/navigation";

/**
 * Root route: redirect directly to the onboarding wizard.
 */
export default function RootPage() {
  redirect("/onboarding");
}
