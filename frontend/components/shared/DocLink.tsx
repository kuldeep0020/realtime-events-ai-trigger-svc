import { ExternalLink } from "lucide-react";
import { cn } from "@/lib/utils";

interface DocLinkProps {
  href: string;
  children: React.ReactNode;
  className?: string;
}

/**
 * Small inline documentation link that opens in a new tab.
 * Used in config preview and email viewer.
 */
export function DocLink({ href, children, className }: DocLinkProps) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={cn(
        "inline-flex items-center gap-1 text-xs",
        "text-violet-400 hover:text-violet-300 underline underline-offset-2",
        "transition-colors",
        className
      )}
    >
      {children}
      <ExternalLink className="w-3 h-3 flex-shrink-0" aria-hidden="true" />
    </a>
  );
}
