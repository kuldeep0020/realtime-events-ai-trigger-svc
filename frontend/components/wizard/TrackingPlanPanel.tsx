"use client";

import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import type { TrackingPlanSpec } from "@/types/api";

interface TrackingPlanPanelProps {
  spec: TrackingPlanSpec | null;
}

/**
 * Read-only side panel that displays the tracking plan event schema for the
 * selected persona. Shown alongside the persona picker (step 1) and Q&A (step 2).
 */
export function TrackingPlanPanel({ spec }: TrackingPlanPanelProps) {
  if (!spec) {
    return (
      <aside
        className="flex flex-col items-center justify-center h-full p-6 text-slate-500 text-sm"
        aria-label="Tracking plan preview"
      >
        Select a persona to view its tracking plan.
      </aside>
    );
  }

  return (
    <aside
      className="flex flex-col gap-4 p-4 overflow-y-auto h-full"
      aria-label={`Tracking plan for ${spec.persona}`}
    >
      <div>
        <p className="text-xs font-mono text-slate-500 uppercase tracking-wider mb-1">
          Tracking Plan
        </p>
        <p className="text-slate-300 text-sm">{spec.description}</p>
        <p className="text-xs text-slate-500 mt-1">v{spec.version}</p>
      </div>
      <Separator className="bg-slate-800" />

      <ul className="flex flex-col gap-4" role="list">
        {spec.events.map((event) => (
          <li key={event.name} className="flex flex-col gap-2">
            <div className="flex items-start gap-2">
              <Badge
                variant="outline"
                className="border-violet-700 text-violet-400 font-mono text-xs shrink-0"
              >
                {event.name}
              </Badge>
            </div>
            <p className="text-xs text-slate-400">{event.description}</p>

            {event.properties.length > 0 && (
              <table
                className="w-full text-xs border-collapse"
                aria-label={`Properties for ${event.name}`}
              >
                <thead>
                  <tr className="text-slate-500 text-left">
                    <th className="py-0.5 pr-2 font-normal w-1/3">property</th>
                    <th className="py-0.5 pr-2 font-normal w-1/4">type</th>
                    <th className="py-0.5 font-normal">req</th>
                  </tr>
                </thead>
                <tbody>
                  {event.properties.map((prop) => (
                    <tr
                      key={prop.name}
                      className="border-t border-slate-800/50"
                    >
                      <td className="py-0.5 pr-2 font-mono text-slate-300">
                        {prop.name}
                      </td>
                      <td className="py-0.5 pr-2 text-slate-400">
                        {prop.type}
                      </td>
                      <td className="py-0.5 text-slate-500">
                        {prop.required ? (
                          <span className="text-amber-500" aria-label="required">
                            *
                          </span>
                        ) : (
                          <span aria-label="optional">—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </li>
        ))}
      </ul>
    </aside>
  );
}
