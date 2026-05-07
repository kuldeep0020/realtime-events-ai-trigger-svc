import { cn } from "@/lib/utils";

interface BrandHeaderProps {
  className?: string;
}

/**
 * Top-of-page branding strip with RudderStack wordmark + product subtitle.
 */
export function BrandHeader({ className }: BrandHeaderProps) {
  return (
    <header
      className={cn(
        "flex items-center gap-3 px-6 py-4 border-b border-slate-800",
        className
      )}
      role="banner"
    >
      {/* Purple accent bar mimicking the RS logo mark */}
      <div
        className="w-7 h-7 rounded-md flex-shrink-0"
        style={{ background: "#7447fc" }}
        aria-hidden="true"
      />
      <div className="flex flex-col leading-none">
        <span className="text-slate-100 font-semibold text-sm tracking-wide">
          RudderStack
        </span>
        <span className="text-slate-400 text-xs">AI Trigger Demo</span>
      </div>
    </header>
  );
}
