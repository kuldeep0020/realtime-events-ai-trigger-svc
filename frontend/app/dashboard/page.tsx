import Link from "next/link";
import { BrandHeader } from "@/components/shared/BrandHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { BarChart3 } from "lucide-react";

/**
 * Dashboard placeholder.
 * WP-H will replace this with the live 3-column SSE dashboard.
 */
export default function DashboardPage() {
  return (
    <div className="flex flex-col min-h-screen bg-slate-950">
      <BrandHeader />
      <main
        className="flex-1 flex items-center justify-center p-8"
        aria-label="Dashboard"
      >
        <Card className="bg-slate-900 border-slate-800 max-w-md w-full">
          <CardContent className="flex flex-col items-center gap-4 py-12 px-8 text-center">
            <div
              className="w-14 h-14 rounded-xl flex items-center justify-center"
              style={{ background: "rgba(116, 71, 252, 0.15)" }}
              aria-hidden="true"
            >
              <BarChart3 className="w-7 h-7 text-violet-400" />
            </div>
            <div className="flex flex-col gap-2">
              <h1 className="text-xl font-semibold text-slate-100">
                Dashboard
              </h1>
              <Badge
                variant="outline"
                className="border-amber-700 text-amber-400 text-xs self-center"
              >
                Built by WP-H — coming next
              </Badge>
              <p className="text-sm text-slate-400 mt-2 leading-relaxed">
                The live 3-column streaming dashboard with SSE event feeds,
                trigger cards, and the mock email viewer will be implemented
                in the next work package.
              </p>
            </div>
            <Link
              href="/onboarding"
              className="text-xs text-violet-400 hover:text-violet-300 underline underline-offset-2 mt-2"
            >
              Back to onboarding wizard
            </Link>
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
