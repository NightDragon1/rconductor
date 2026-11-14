// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Fitness function render (PRJ-005): JSX shows a field that
// api/openapi.yaml marks as foreign data (x-foreign: true) only as the text
// of ForeignText, never as a bare child. Checked on the types: a property
// access counts when its declaration is that field of the generated
// schema. Optional argument: the directory of a project to check.
import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import ts from 'typescript';

const root = resolve(process.argv[2] ?? join(import.meta.dirname, '..'));
const spec = process.argv[3] ?? join(root, '..', 'api', 'openapi.yaml');

// foreignFields reads Schema.property of every x-foreign mark; the file
// keeps schemas at four and properties at eight spaces.
export function foreignFields(yaml: string): Set<string> {
  const out = new Set<string>();
  let schema = '';
  let prop = '';
  for (const line of yaml.split('\n')) {
    const s = /^ {4}([A-Za-z0-9]+):$/.exec(line);
    if (s) schema = s[1];
    const p = /^ {8}([a-z_]+):$/.exec(line);
    if (p) prop = p[1];
    if (line.trim() === 'x-foreign: true') out.add(`${schema}.${prop}`);
  }
  return out;
}

// Only the generated schema of the project counts, by its exact path.
const schemaFile = resolve(root, 'src', 'api', 'schema.d.ts');
function schemaField(d: ts.Declaration): string | undefined {
  if (!ts.isPropertySignature(d) || resolve(d.getSourceFile().fileName) !== schemaFile) return;
  const owner = d.parent?.parent;
  if (!owner || !ts.isPropertySignature(owner)) return;
  return `${owner.name.getText()}.${d.name.getText()}`;
}

process.on('uncaughtException', (e) => {
  console.error('fitness render:', e.message);
  process.exit(2);
});
const foreign = foreignFields(readFileSync(spec, 'utf8'));
// The program is fixed here, not taken from tsconfig.app.json, which lies
// outside the core paths: its include, paths or options must not narrow or
// redirect the check. Every file under src/ is a root; a source file the
// check cannot read (.js, .jsx, ...) fails it with exit 3.
const src = join(root, 'src');
const other = ts.sys
  .readDirectory(src, ['.js', '.jsx', '.mjs', '.cjs', '.mts', '.cts'])
  .map((f) => resolve(f));
if (other.length > 0) {
  console.error(`fitness render: source file the check cannot read: ${other.join(', ')}`);
  process.exit(3);
}
const program = ts.createProgram(ts.sys.readDirectory(src, ['.ts', '.tsx']), {
  jsx: ts.JsxEmit.ReactJSX,
  module: ts.ModuleKind.ESNext,
  moduleResolution: ts.ModuleResolutionKind.Bundler,
  target: ts.ScriptTarget.ES2023,
  baseUrl: root,
  paths: { '@/*': ['./src/*'] },
  types: [],
  skipLibCheck: true,
  noEmit: true,
});
const checker = program.getTypeChecker();
const findings: string[] = [];

function visit(node: ts.Node) {
  if (ts.isJsxExpression(node) && node.expression && !ts.isJsxAttribute(node.parent)) {
    let e = node.expression;
    while (ts.isParenthesizedExpression(e) || ts.isNonNullExpression(e)) e = e.expression;
    if (ts.isPropertyAccessExpression(e)) {
      const field = checker
        .getSymbolAtLocation(e.name)
        ?.declarations?.map(schemaField)
        .find((f) => f && foreign.has(f));
      if (field) {
        const sf = node.getSourceFile();
        const { line } = sf.getLineAndCharacterOfPosition(node.getStart());
        findings.push(`render ${sf.fileName.slice(root.length + 1)}:${line + 1} ${field}`);
      }
    }
  }
  ts.forEachChild(node, visit);
}

for (const sf of program.getSourceFiles()) {
  if (!sf.isDeclarationFile && sf.fileName.startsWith(join(root, 'src'))) visit(sf);
}
for (const f of findings) console.log(`${f}: shown without ForeignText`);
if (findings.length > 0) process.exit(1);
console.log(`render ok (${foreign.size} foreign fields)`);
