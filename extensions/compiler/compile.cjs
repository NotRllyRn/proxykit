'use strict';
// Host-owned compiler driver. No package configuration, plugins or module execution.
const fs = require('node:fs');
const path = require('node:path');
const ts = require('./typescript.js');
const [root, out] = process.argv.slice(2);
const sdk = path.join(out, '.sdk', 'index.d.mts');
const files = [];
function walk(dir) {
  for (const e of fs.readdirSync(dir, {withFileTypes:true})) {
    const p = path.join(dir, e.name);
    if (p === out) continue;
    if (e.isDirectory()) walk(p);
    else if (/\.(mts|ts)$/.test(e.name)) {
      if (/\.d\.(mts|ts)$/.test(e.name)) throw Error('package declaration files are unsupported');
      files.push(p);
    } else if (/^tsconfig.*\.json$/.test(e.name) || /\.(cts|tsx|jsx|js|mjs|cjs)$/.test(e.name)) {
      throw Error('source mode forbids package tsconfig and JavaScript/CommonJS/JSX: '+p);
    } else if (e.name === 'package.json') {
      const pkg = JSON.parse(fs.readFileSync(p, 'utf8'));
      if (pkg.type && pkg.type !== 'module') throw Error('source mode requires ESM');
    }
  }
}
walk(root);
const allowed = new Set([...files, sdk, path.join(out,'.sdk','http.d.mts')]);
const options = {target:ts.ScriptTarget.ES2022, module:ts.ModuleKind.ES2022,
  moduleResolution:ts.ModuleResolutionKind.Bundler, moduleDetection:ts.ModuleDetectionKind.Force,
  strict:true, noEmitOnError:true,
  skipLibCheck:false, types:[], lib:['lib.es2022.d.ts','lib.dom.d.ts'],
  rootDir:root, outDir:out, allowImportingTsExtensions:true, rewriteRelativeImportExtensions:true};
const host = ts.createCompilerHost(options);
const read = host.readFile;
host.readFile = p => allowed.has(path.resolve(p)) || (path.dirname(p) === __dirname && /^lib\..*\.d\.ts$/.test(path.basename(p))) ? read(p) : undefined;
const resolutions = new Map();
function resolve(spec, importer) {
  if (spec === '@proxykit/sdk') return sdk;
  if (!spec.startsWith('./') && !spec.startsWith('../')) throw Error('unsupported import: '+spec);
  if (/[\\?#%\x00]/.test(spec)) throw Error('unsupported import path: '+spec);
  const base = path.resolve(path.dirname(importer), spec);
  if (!base.startsWith(root+path.sep)) throw Error('import escapes source package: '+spec);
  let target = base;
  if (spec.endsWith('.mjs')) target = base.slice(0,-4)+'.mts';
  else if (spec.endsWith('.js')) target = base.slice(0,-3)+'.ts';
  else if (!/\.(mts|ts)$/.test(spec)) throw Error('imports require explicit .mts/.ts/.mjs/.js extension: '+spec);
  if (!files.includes(target)) throw Error('import is not a package source file: '+spec);
  return target;
}
// Inspect syntax before resolution, including type-only edges and dynamic imports.
for (const f of files) {
  const sf = ts.createSourceFile(f, fs.readFileSync(f,'utf8'), options.target, true);
  if (sf.referencedFiles.length || sf.typeReferenceDirectives.length || sf.libReferenceDirectives.length || sf.hasNoDefaultLib) throw Error('triple-slash directives are unsupported');
  function visit(n) {
    if (ts.isImportEqualsDeclaration(n) || ts.isExportAssignment(n) && n.isExportEquals || ts.isModuleDeclaration(n)) throw Error('CommonJS/namespace/ambient modules are unsupported');
    let spec;
    if ((ts.isImportDeclaration(n) || ts.isExportDeclaration(n)) && n.moduleSpecifier) {
      if (n.attributes) throw Error('import attributes are unsupported');
      spec = n.moduleSpecifier;
    } else if (ts.isImportTypeNode(n)) spec = n.argument.literal;
    else if (ts.isCallExpression(n)) {
      if (n.expression.kind === ts.SyntaxKind.ImportKeyword) {
        if (n.arguments.length !== 1) throw Error('dynamic import requires one literal');
        spec = n.arguments[0];
      } else if (ts.isIdentifier(n.expression) && n.expression.text === 'require') throw Error('require is unsupported');
    }
    if (spec) {
      if (!ts.isStringLiteral(spec)) throw Error('nonliteral imports are unsupported');
      resolutions.set(f+'\0'+spec.text, resolve(spec.text,f));
    }
    ts.forEachChild(n,visit);
  }
  visit(sf);
}
host.resolveModuleNames = (names, importer) => names.map(spec => {
  let p;
  if (importer === sdk && spec === './http.mjs') p = path.join(out,'.sdk','http.d.mts');
  else if (importer === path.join(out,'.sdk','http.d.mts') && spec === './index.mjs') p = sdk;
  else p = resolutions.get(importer+'\0'+spec);
  return p ? {resolvedFileName:p, resolvedUsingTsExtension:/\.(mts|ts)$/.test(spec),
    extension:p.endsWith('.d.mts') ? ts.Extension.Dmts : p.endsWith('.mts') ? ts.Extension.Mts : ts.Extension.Ts} : undefined;
});
function runtimePath(p) {
  if (p === sdk) return path.join(out,'.sdk','index.mjs');
  return path.join(out, path.relative(root,p).replace(/\.(mts|ts)$/,'.mjs'));
}
// Rewrite imports to the exact emitted ESM path; assets keep their relative layout.
const rewrite = context => sf => {
  function visit(n) {
    if (ts.isStringLiteral(n)) {
      const target = resolutions.get(sf.fileName+'\0'+n.text);
      // Rewrite module specifiers only, not arbitrary equal-valued strings/assets.
      const parent = n.parent;
      if (target && parent && (ts.isImportDeclaration(parent) || ts.isExportDeclaration(parent) || ts.isCallExpression(parent) && parent.expression.kind === ts.SyntaxKind.ImportKeyword)) {
        let rel = path.relative(path.dirname(runtimePath(sf.fileName)),runtimePath(target)).split(path.sep).join('/');
        if (!rel.startsWith('.')) rel = './'+rel;
        return ts.factory.createStringLiteral(rel);
      }
    }
    return ts.visitEachChild(n,visit,context);
  }
  return ts.visitNode(sf,visit);
};
let bytes = 0, count = 0;
host.writeFile = (name, text) => {
  name = name.replace(/\.(js|mjs)$/,'.mjs');
  if (!name.startsWith(out+path.sep) || ++count > 2048 || (bytes += Buffer.byteLength(text)) > 64*1024*1024) throw Error('compiler output exceeds bound');
  fs.mkdirSync(path.dirname(name),{recursive:true,mode:0o700});
  fs.writeFileSync(name,text,{mode:0o600,flag:'wx'});
};
const program = ts.createProgram(files, options, host);
const diagnostics = ts.getPreEmitDiagnostics(program);
if (diagnostics.length) {
  process.stderr.write(ts.formatDiagnostics(diagnostics.slice(0,20),{getCanonicalFileName:p=>p,getCurrentDirectory:()=>root,getNewLine:()=> '\n'}).slice(0,12000));
  process.exit(1);
}
const result = program.emit(undefined,undefined,undefined,undefined,{before:[rewrite]});
if (result.emitSkipped) throw Error('TypeScript emit skipped');
