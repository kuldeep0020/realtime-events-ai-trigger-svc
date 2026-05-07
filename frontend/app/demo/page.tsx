/**
 * /demo — full-page demo controller.
 * Shows the fire buttons, reset, and replay-last-trigger in a prominent layout.
 */

import Link from "next/link";
import { BrandHeader } from "@/components/shared/BrandHeader";
import { Controller } from "@/components/demo/Controller";

export default function DemoPage() {
  return (
    <div className="flex flex-col min-h-screen bg-slate-950">
      <BrandHeader />
      <main className="flex-1 flex flex-col items-center justify-center px-6 py-12 gap-8">
        <div className="text-center space-y-2">
          <h1 className="text-2xl font-semibold text-slate-100">Demo Controller</h1>
          <p className="text-sm text-slate-400 max-w-sm">
            Fire the event scripts and watch triggers fire in the live dashboard.
          </p>
          <Link
            href="/dashboard"
            className="text-xs text-violet-400 hover:text-violet-300 underline underline-offset-2"
          >
            Open dashboard →
          </Link>
        </div>

        <Controller />
      </main>
    </div>
  );
}
