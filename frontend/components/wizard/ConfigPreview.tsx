"use client";

import { useState, useTransition } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Loader2, ArrowLeft, CheckCircle2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { activateConfig } from "@/lib/api-client";
import type { GenerateConfigResponse, Persona } from "@/types/api";
import { useRouter } from "next/navigation";

interface ConfigPreviewProps {
  data: GenerateConfigResponse;
  persona: Persona;
  configYaml?: string;
  onBack: () => void;
}

// Safe syntax highlighter returns React nodes — no innerHTML
type Token =
  | { kind: "comment"; text: string }
  | { kind: "key"; text: string }
  | { kind: "str"; text: string }
  | { kind: "num"; text: string }
  | { kind: "kw"; text: string }
  | { kind: "plain"; text: string };

function tokenizeLine(line: string): Token[] {
  if (/^\s*#/.test(line)) {
    return [{ kind: "comment", text: line }];
  }
  const tokens: Token[] = [];
  const pattern =
    /([a-zA-Z_][\w-]*)(?=\s*:)|"([^"]*)"|(?<![.\w])(\d+(?:\.\d+)?)(?![.\w])|\b(true|false|null)\b/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = pattern.exec(line)) !== null) {
    const start = m.index;
    if (start > last) tokens.push({ kind: "plain", text: line.slice(last, start) });
    if (m[1] !== undefined) {
      tokens.push({ kind: "key", text: m[1] });
      last = start + m[1].length;
    } else if (m[2] !== undefined) {
      tokens.push({ kind: "str", text: `"${m[2]}"` });
      last = start + m[0].length;
    } else if (m[3] !== undefined) {
      tokens.push({ kind: "num", text: m[3] });
      last = start + m[3].length;
    } else if (m[4] !== undefined) {
      tokens.push({ kind: "kw", text: m[4] });
      last = start + m[4].length;
    }
  }
  if (last < line.length) tokens.push({ kind: "plain", text: line.slice(last) });
  return tokens;
}

const TOKEN_CLASS: Record<Token["kind"], string> = {
  comment: "text-slate-500 italic",
  key: "text-blue-400",
  str: "text-green-400",
  num: "text-orange-400",
  kw: "text-purple-400",
  plain: "text-slate-300",
};

function HighlightedYAML({ yaml }: { yaml: string }) {
  return (
    <pre
      className="text-xs p-4 font-mono leading-relaxed whitespace-pre overflow-auto"
      aria-label="Generated configuration YAML"
    >
      {yaml.split("\n").map((line, li) => (
        <div key={li}>
          {tokenizeLine(line).map((tok, ti) => (
            <span key={ti} className={TOKEN_CLASS[tok.kind]}>{tok.text}</span>
          ))}
        </div>
      ))}
    </pre>
  );
}

export function ConfigPreview({ data, persona, configYaml, onBack }: ConfigPreviewProps) {
  const router = useRouter();
  const [activating, setActivating] = useState(false);
  const [activateError, setActivateError] = useState<string | null>(null);
  const [, startTransition] = useTransition();

  const yaml = configYaml ?? data.config_yaml;
  // Count rules robustly across YAML formatter variations:
  //   - seed YAML uses 0-space indent for `rules:` items (`- fire:` at col 0)
  //   - gopkg.in/yaml.v3 (re-marshalled) uses 4-space indent (`    - fire:`)
  //   - both put a different key first per rule item (`name:` or `fire:`)
  //
  // Strategy: find the `rules:` line, then look at the FIRST `- ` list item
  // after it to learn the rule-item indent. Subsequent list items at that
  // exact indent are rules; deeper indents are nested (e.g., predicates
  // inside `when.all`); a top-level key at column 0 ends the block.
  function countRules(yaml: string): number {
    const lines = yaml.split("\n");
    let inRules = false;
    let ruleIndent: number | null = null;
    let count = 0;
    for (const line of lines) {
      if (/^rules:\s*$/.test(line)) {
        inRules = true;
        ruleIndent = null;
        continue;
      }
      if (!inRules) continue;
      // End block: a fresh top-level key at column 0.
      if (/^[A-Za-z_][\w-]*:/.test(line) && line[0] !== " ") break;
      // Skip blank lines.
      const m = line.match(/^(\s*)-\s/);
      if (!m) continue;
      const indent = m[1].length;
      if (ruleIndent === null) {
        ruleIndent = indent;
        count = 1;
      } else if (indent === ruleIndent) {
        count++;
      }
      // Items at deeper indent are nested predicates / suburbs lists; ignore.
    }
    return count;
  }
  const ruleCount = countRules(yaml);

  const handleActivate = () => {
    if (activating) return;
    setActivating(true);
    setActivateError(null);
    startTransition(async () => {
      try {
        await activateConfig({ persona, config_yaml: yaml });
        router.push("/dashboard");
      } catch (err) {
        setActivateError(err instanceof Error ? err.message : "Activation failed");
        setActivating(false);
      }
    });
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h2 className="text-xl font-semibold text-slate-100 mb-1">Your generated config</h2>
        <p className="text-sm text-slate-400">{data.description}</p>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <Badge variant="outline" className="border-violet-700 text-violet-400 font-mono text-xs">
          {ruleCount} {ruleCount === 1 ? "rule" : "rules"}
        </Badge>
        <Badge variant="outline" className="border-slate-700 text-slate-400 font-mono text-xs">
          source: {data.source}
        </Badge>
        <Badge variant="outline" className="border-emerald-700 text-emerald-400 text-xs">
          <CheckCircle2 className="w-3 h-3 mr-1" aria-hidden="true" />
          Ready to activate
        </Badge>
      </div>

      <div className="rounded-lg border border-slate-700 bg-slate-900 overflow-auto max-h-72">
        <HighlightedYAML yaml={yaml} />
      </div>

      {activateError && (
        <div role="alert" className="rounded-md border border-red-800 bg-red-950/50 px-4 py-3 text-sm text-red-300">
          {activateError}
        </div>
      )}

      <div className="flex items-center gap-3">
        <Button
          onClick={handleActivate}
          disabled={activating}
          className="bg-violet-600 hover:bg-violet-700 text-white focus-visible:ring-violet-500"
          aria-busy={activating}
        >
          {activating && <Loader2 className="w-4 h-4 mr-2 animate-spin" aria-hidden="true" />}
          {activating ? "Activating..." : "Activate & continue"}
        </Button>
        <Button
          variant="outline"
          onClick={onBack}
          disabled={activating}
          className={cn("border-slate-700 text-slate-300 hover:bg-slate-800 focus-visible:ring-violet-500")}
        >
          <ArrowLeft className="w-4 h-4 mr-2" aria-hidden="true" />
          Edit answers
        </Button>
      </div>
    </div>
  );
}
