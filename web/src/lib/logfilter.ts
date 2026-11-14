// SPDX-License-Identifier: AGPL-3.0-or-later

// Log filter in two parts (UI-019): one on the bracket part at the start of
// a line ("[10:00:00] [Server thread/INFO]"), one on the text after it,
// combined with and, or, xor. Matching stays a plain, case-insensitive
// substring test like the single filter; no expression from the user is
// ever compiled, so no input can make matching slow beyond linear.

export type LogOp = 'and' | 'or' | 'xor';
export const LOG_OPS: readonly LogOp[] = ['and', 'or', 'xor'];

// A filter part longer than this is refused with a hint.
export const MAX_FILTER_LENGTH = 200;
// The bracket part is only looked for in this many leading characters, so
// a huge line costs no more than a short one to split.
export const MAX_BRACKET_PART = 1024;

export interface SplitLine {
  bracket: string; // leading bracketed groups, "" without any
  text: string; // the rest of the line
}

// splitLine takes the leading run of bracketed groups, separated by spaces
// or tabs, nested brackets counted. An unclosed group ends the run; a line
// without a closed group at its start has no bracket part.
export function splitLine(line: string): SplitLine {
  const limit = Math.min(line.length, MAX_BRACKET_PART);
  let end = 0; // end of the last closed group
  let i = 0;
  while (i < limit) {
    let j = i;
    while (j < limit && (line[j] === ' ' || line[j] === '\t')) j++;
    if (line[j] !== '[') break;
    let depth = 0;
    let k = j;
    for (; k < limit; k++) {
      if (line[k] === '[') depth++;
      else if (line[k] === ']' && --depth === 0) break;
    }
    if (k >= limit) break; // unclosed
    end = k + 1;
    i = end;
  }
  if (end === 0) return { bracket: '', text: line };
  // A colon right after the brackets belongs to neither part.
  const rest = line.slice(end).replace(/^[ \t]*:?[ \t]*/, '');
  return { bracket: line.slice(0, end).trimStart(), text: rest };
}

export type FilterProblem = 'tooLong' | 'control';

export interface LogFilter {
  bracket: string;
  text: string;
  op: LogOp;
}

// Control characters, as in log lines from foreign servers.
// eslint-disable-next-line no-control-regex
const CONTROL = /[\u0000-\u001f\u007f-\u009f]/;

// problemOf checks one filter part as typed.
export function problemOf(part: string): FilterProblem | undefined {
  if (part.length > MAX_FILTER_LENGTH) return 'tooLong';
  if (CONTROL.test(part)) return 'control';
  return undefined;
}

export type CompiledFilter =
  | { ok: true; test: ((line: string) => boolean) | undefined }
  | { ok: false; problem: FilterProblem };

// compileFilter checks both parts; without a problem it returns the test
// for a line, or undefined when both parts are empty. An empty part sets
// no condition, so the other part alone decides whatever the operator.
export function compileFilter(f: LogFilter): CompiledFilter {
  const problem = problemOf(f.bracket) ?? problemOf(f.text);
  if (problem) return { ok: false, problem };
  const b = f.bracket.trim().toLowerCase();
  const x = f.text.trim().toLowerCase();
  if (!b && !x) return { ok: true, test: undefined };
  const op = LOG_OPS.includes(f.op) ? f.op : 'and';
  return {
    ok: true,
    test: (line) => {
      const s = splitLine(line);
      const inB = s.bracket.toLowerCase().includes(b);
      const inX = s.text.toLowerCase().includes(x);
      if (!b) return inX;
      if (!x) return inB;
      return op === 'and' ? inB && inX : op === 'or' ? inB || inX : inB !== inX;
    },
  };
}
