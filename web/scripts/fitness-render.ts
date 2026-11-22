// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Fitness function render (PRJ-005): JSX shows a field that
// api/openapi.yaml marks as foreign data (x-foreign: true) only as the text
// of ForeignText, never as a bare child. Checked on the types: a property
// access counts when its declaration is that field of the generated
// schema, read directly, by element access, through a call, a condition,
// a template literal or a name bound to it; a plain element's attribute
// counts as shown. Optional argument: the directory of a project to check.
import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import ts from 'typescript';

const root = resolve(process.argv[2] ?? join(import.meta.dirname, '..'));
const spec = process.argv[3] ?? join(root, '..', 'api', 'openapi.yaml');

// foreignFields reads Schema.property of every x-foreign mark; the file
// keeps schemas at four, properties at eight and the mark at ten spaces.
// A mark elsewhere is not read, and the count below fails the check.
export function foreignFields(yaml: string): Set<string> {
  const out = new Set<string>();
  let schema = '';
  let prop = '';
  for (const line of yaml.split('\n')) {
    const s = /^ {4}([A-Za-z0-9]+):$/.exec(line);
    if (s) schema = s[1];
    const p = /^ {8}([A-Za-z0-9_]+):$/.exec(line);
    if (p) prop = p[1];
    if (line === '          x-foreign: true') out.add(`${schema}.${prop}`);
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
const yaml = readFileSync(spec, 'utf8');
const foreign = foreignFields(yaml);
// Every mark must be read: a mark at another indent would be lost.
const marks = yaml.split('\n').filter((l) => l.trim() === 'x-foreign: true').length;
if (marks === 0 || marks !== foreign.size) {
  console.error(`fitness render: ${marks} x-foreign marks, ${foreign.size} fields read`);
  process.exit(2);
}
// The program is fixed here, not taken from tsconfig.app.json, which lies
// outside the core paths: its include, paths or options must not narrow or
// redirect the check. Every file under src/ is a root; a source file the
// check cannot read (.js, .jsx, ...) fails it with exit 3.
const src = resolve(root, 'src');
const other = ts.sys
  .readDirectory(src, ['.js', '.jsx', '.mjs', '.cjs', '.mts', '.cts'])
  .map((f) => resolve(f));
if (other.length > 0) {
  console.error(`fitness render: source file the check cannot read: ${other.join(', ')}`);
  process.exit(3);
}
const program = ts.createProgram(ts.sys.readDirectory(src, ['.ts', '.tsx']), {
  jsx: ts.JsxEmit.ReactJSX,
  // A .js or .jsx import enters the program, so the check below sees it.
  allowJs: true,
  checkJs: false,
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

// fieldOf names the foreign schema field an expression reads, if any: a
// property or element access, or a name bound to one (a destructured
// property or a const with such an initializer).
function fieldOf(e: ts.Node, seen = new Set<ts.Node>()): string | undefined {
  if (seen.has(e)) return;
  seen.add(e);
  let sym: ts.Symbol | undefined;
  if (ts.isPropertyAccessExpression(e)) sym = checker.getSymbolAtLocation(e.name);
  else if (ts.isElementAccessExpression(e))
    sym = checker.getSymbolAtLocation(e.argumentExpression) ?? checker.getSymbolAtLocation(e);
  else if (ts.isIdentifier(e)) sym = checker.getSymbolAtLocation(e);
  for (const d of sym?.declarations ?? []) {
    const f = schemaField(d);
    if (f && foreign.has(f)) return f;
    if (ts.isBindingElement(d)) {
      const name = (d.propertyName ?? d.name).getText();
      const prop = checker.getTypeAtLocation(d.parent).getProperty(name);
      const g = prop?.declarations?.map(schemaField).find((x) => x && foreign.has(x));
      if (g) return g;
    }
    // The parameter of a callback on a foreign list: players.map((p) => p).
    const fn = ts.isParameter(d) ? d.parent : undefined;
    const call =
      fn && (ts.isArrowFunction(fn) || ts.isFunctionExpression(fn)) ? fn.parent : undefined;
    if (
      call &&
      ts.isCallExpression(call) &&
      ts.isPropertyAccessExpression(call.expression) &&
      text(e as ts.Expression)
    ) {
      const g = within(call.expression.expression, seen);
      if (g) return g;
    }
    // A field of an object built here: { name: agent.name }.
    if (ts.isPropertyAssignment(d) && d.getSourceFile() === e.getSourceFile()) {
      const g = within(d.initializer, seen);
      if (g) return g;
    }
    if (ts.isShorthandPropertyAssignment(d)) {
      const g = fieldOf(d.name, seen);
      if (g) return g;
    }
    if (ts.isVariableDeclaration(d) && d.initializer && d.getSourceFile() === e.getSourceFile()) {
      const g = within(d.initializer, seen);
      if (g) return g;
    }
  }
}

// within finds a foreign field anywhere in an expression: in calls,
// conditions, template literals and so on, but not inside nested JSX,
// which is checked on its own.
function within(e: ts.Node, seen = new Set<ts.Node>()): string | undefined {
  if (ts.isJsxElement(e) || ts.isJsxSelfClosingElement(e) || ts.isJsxFragment(e)) return;
  // A callback's value is shown only through map and flatMap; the
  // predicate of filter, find or sort is not.
  if (ts.isArrowFunction(e) || ts.isFunctionExpression(e)) {
    const call = e.parent;
    const method =
      ts.isCallExpression(call) && ts.isPropertyAccessExpression(call.expression)
        ? call.expression.name.text
        : '';
    if (method !== 'map' && method !== 'flatMap') return;
    return within(e.body, seen);
  }
  if (ts.isReturnStatement(e)) return e.expression && within(e.expression, seen);
  if (ts.isBlock(e)) {
    let r: string | undefined;
    for (const st of e.statements) if (ts.isReturnStatement(st)) r ??= within(st, seen);
    return r;
  }
  // plainText of src/lib/foreign.ts makes foreign text safe to show.
  if (ts.isCallExpression(e)) {
    let sym = checker.getSymbolAtLocation(e.expression);
    if (sym && sym.flags & ts.SymbolFlags.Alias) sym = checker.getAliasedSymbol(sym);
    const decl = sym?.declarations?.[0];
    const file = decl && resolve(decl.getSourceFile().fileName);
    if (
      file === resolve(src, 'lib', 'foreign.ts') &&
      sym?.name === 'plainText' &&
      decl !== undefined &&
      ts.isFunctionDeclaration(decl)
    )
      return;
  }
  const f = fieldOf(e, seen);
  if (f) return f;
  // The left side of && and the condition of ?: decide, they are not shown.
  if (ts.isBinaryExpression(e) && e.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken)
    return within(e.right, seen);
  if (ts.isConditionalExpression(e)) return within(e.whenTrue, seen) ?? within(e.whenFalse, seen);
  let found: string | undefined;
  ts.forEachChild(e, (c) => {
    found ??= within(c, seen);
  });
  return found;
}

// text tells whether an expression can reach the page as text: a string
// or a number, or a list of them (JSX elements are checked on their own).
function text(e: ts.Expression): boolean {
  const like =
    ts.TypeFlags.StringLike | ts.TypeFlags.NumberLike | ts.TypeFlags.Any | ts.TypeFlags.Unknown;
  const check = (t: ts.Type): boolean => {
    if (t.isUnion()) return t.types.some(check);
    if (t.flags & like) return true;
    const el = checker.isArrayType(t)
      ? checker.getTypeArguments(t as ts.TypeReference)[0]
      : undefined;
    return el !== undefined && check(el);
  };
  return check(checker.getTypeAtLocation(e));
}

// shown is an expression whose value reaches the page as text: a JSX
// child, or an attribute of a plain element (title, aria-label, ...).
// Attributes of components pass the value on; ForeignText takes it.
function shown(node: ts.JsxExpression): boolean {
  if (!ts.isJsxAttribute(node.parent)) return true;
  const name = node.parent.name.getText();
  if (name === 'key' || name === 'className' || /^on[A-Z]/.test(name)) return false;
  const tag = node.parent.parent.parent.tagName.getText();
  return /^[a-z]/.test(tag);
}

function visit(node: ts.Node) {
  if (ts.isJsxExpression(node) && node.expression && shown(node)) {
    const field = text(node.expression) ? within(node.expression) : undefined;
    if (field) {
      const sf = node.getSourceFile();
      const { line } = sf.getLineAndCharacterOfPosition(node.getStart());
      findings.push(`render ${sf.fileName.slice(root.length + 1)}:${line + 1} ${field}`);
    }
  }
  ts.forEachChild(node, visit);
}

// A source file the program pulls in from outside src/ (an import of
// ../lib/x.tsx, say) would go unchecked: it fails the check with exit 3.
// Installed packages are the node_modules of the project or of a directory
// above it (module resolution walks up); a declaration file elsewhere could
// stand for a .js or .jsx the bundler takes, so it fails the check too.
const installed: string[] = [];
for (let d = root; ; d = dirname(d)) {
  installed.push(join(d, 'node_modules') + '/');
  if (dirname(d) === d) break;
}
const outside = program
  .getSourceFiles()
  .map((sf) => resolve(sf.fileName))
  .filter((f) => !f.startsWith(src + '/') && !installed.some((n) => f.startsWith(n)));
if (outside.length > 0) {
  console.error(`fitness render: source file outside src/: ${outside.join(', ')}`);
  process.exit(3);
}
for (const sf of program.getSourceFiles()) {
  if (!sf.isDeclarationFile && resolve(sf.fileName).startsWith(src + '/')) visit(sf);
}
for (const f of findings) console.log(`${f}: shown without ForeignText`);
if (findings.length > 0) process.exit(1);
console.log(`render ok (${foreign.size} foreign fields)`);
