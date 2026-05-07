"use client";

/**
 * EmailViewer — faux-Gmail interface for mock email payloads.
 * Renders body_markdown via react-markdown and doc_links as a clickable list.
 */

import ReactMarkdown from "react-markdown";
import { ExternalLink, Archive, Reply, CheckCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { MockEmailPayload } from "@/types/api";

interface EmailViewerProps {
  email: MockEmailPayload | null;
  open: boolean;
  onClose: () => void;
}

export function EmailViewer({ email, open, onClose }: EmailViewerProps) {
  if (!email) return null;

  const handleCosmetic = (action: string) => {
    console.log(`[EmailViewer] ${action}`, email.id);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        className="bg-slate-900 border-slate-700 max-w-2xl w-full max-h-[90vh] flex flex-col"
        aria-label="Email viewer"
      >
        <DialogHeader className="border-b border-slate-800 pb-3 shrink-0">
          <DialogTitle className="text-slate-100 text-sm font-medium">
            Inbox
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto">
          {/* Gmail-style header row */}
          <div className="px-4 pt-4 pb-3 border-b border-slate-800">
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1 min-w-0">
                <h2 className="text-slate-100 font-semibold text-base leading-snug mb-2">
                  {email.subject}
                </h2>
                <div className="text-xs text-slate-400 space-y-0.5">
                  <div>
                    <span className="text-slate-500">From:</span>{" "}
                    <span className="text-slate-300">
                      RudderStack &lt;onboarding@rudderstack.com&gt;
                    </span>
                  </div>
                  <div>
                    <span className="text-slate-500">To:</span>{" "}
                    <span className="text-slate-300">{email.to_email}</span>
                  </div>
                  <div>
                    <span className="text-slate-500">Date:</span>{" "}
                    <span className="text-slate-300">
                      {new Date(email.created_at).toLocaleString()}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Email body */}
          <div className="px-4 py-4 prose prose-invert prose-sm max-w-none">
            <ReactMarkdown
              components={{
                h1: ({ children }) => (
                  <h1 className="text-slate-100 text-lg font-bold mt-4 mb-2">
                    {children}
                  </h1>
                ),
                h2: ({ children }) => (
                  <h2 className="text-slate-100 text-base font-semibold mt-3 mb-1.5">
                    {children}
                  </h2>
                ),
                h3: ({ children }) => (
                  <h3 className="text-slate-200 text-sm font-semibold mt-3 mb-1">
                    {children}
                  </h3>
                ),
                p: ({ children }) => (
                  <p className="text-slate-300 text-sm leading-relaxed mb-3">
                    {children}
                  </p>
                ),
                ul: ({ children }) => (
                  <ul className="text-slate-300 text-sm list-disc pl-5 mb-3 space-y-1">
                    {children}
                  </ul>
                ),
                ol: ({ children }) => (
                  <ol className="text-slate-300 text-sm list-decimal pl-5 mb-3 space-y-1">
                    {children}
                  </ol>
                ),
                li: ({ children }) => (
                  <li className="text-slate-300 text-sm">{children}</li>
                ),
                strong: ({ children }) => (
                  <strong className="text-slate-100 font-semibold">{children}</strong>
                ),
                em: ({ children }) => (
                  <em className="text-slate-200 italic">{children}</em>
                ),
                code: ({ children }) => (
                  <code className="bg-slate-800 text-violet-300 font-mono text-xs px-1.5 py-0.5 rounded">
                    {children}
                  </code>
                ),
                a: ({ href, children }) => (
                  <a
                    href={href}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-violet-400 hover:text-violet-300 underline underline-offset-2"
                  >
                    {children}
                  </a>
                ),
              }}
            >
              {email.body_markdown}
            </ReactMarkdown>
          </div>

          {/* Doc links */}
          {email.links && email.links.length > 0 && (
            <div className="px-4 pb-4 border-t border-slate-800 pt-3">
              <p className="text-xs text-slate-500 mb-2 font-medium uppercase tracking-wider">
                Related docs
              </p>
              <ul className="space-y-1.5" aria-label="Related documentation links">
                {email.links.map((link) => (
                  <li key={link.url}>
                    <a
                      href={link.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-1.5 text-xs text-violet-400 hover:text-violet-300 group"
                    >
                      <ExternalLink
                        className="w-3 h-3 opacity-60 group-hover:opacity-100 shrink-0"
                        aria-hidden="true"
                      />
                      {link.title}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>

        {/* Gmail-style action bar */}
        <div className="flex items-center gap-2 px-4 py-3 border-t border-slate-800 shrink-0">
          <Button
            variant="outline"
            size="sm"
            className="border-slate-700 text-slate-300 hover:text-slate-100 text-xs h-7"
            onClick={() => handleCosmetic("reply")}
            aria-label="Reply (cosmetic)"
          >
            <Reply className="w-3 h-3 mr-1" aria-hidden="true" />
            Reply
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="border-slate-700 text-slate-300 hover:text-slate-100 text-xs h-7"
            onClick={() => handleCosmetic("mark-read")}
            aria-label="Mark as read (cosmetic)"
          >
            <CheckCheck className="w-3 h-3 mr-1" aria-hidden="true" />
            Mark as read
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="border-slate-700 text-slate-300 hover:text-slate-100 text-xs h-7"
            onClick={() => handleCosmetic("archive")}
            aria-label="Archive (cosmetic)"
          >
            <Archive className="w-3 h-3 mr-1" aria-hidden="true" />
            Archive
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
