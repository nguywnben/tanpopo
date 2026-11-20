import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';
import type {Plugin} from 'vite';
const supportedLocales = ['zh-Hans', 'zh-Hant', 'de', 'en', 'es', 'fr', 'id', 'it', 'ja', 'ko', 'pt', 'ru', 'th', 'tr', 'vi'] as const;

const translatedProps = new Set([
  'aria-label', 'label', 'title', 'subtitle', 'description', 'summary', 'placeholder', 'header',
  'ariaLabel', 'emptyTitle', 'emptyDescription', 'confirmLabel', 'cancelLabel', 'tooltip', 'helperText', 'section', 'rule', 'value',
  'matchLabel', 'hint', 'emptySearchText', 'suggestionDescription', 'change', 'activeVal', 'candidateVal',
]);
const translatedAttributes = new Set([...translatedProps].filter(name => name !== 'rule' && name !== 'value'));
const ignoredTags = new Set(['code', 'pre', 'script', 'style', 'textarea']);
const translatedValueWords = new Set(['None', 'No', 'Yes', 'Enabled', 'Disabled', 'Ready', 'Skipped', 'Added', 'Removed', 'Expanded', 'Changed', 'Reduced']);

function isCopy(value: string, allowSingleWord = false): boolean {
  const text = value.trim();
  if (!text || text.length < 2 || text.length > 800 || /\n|\r|https?:\/\//i.test(text) || /^\/(?:admin|v1|mcp)\//i.test(text)) return false;
  if (text === 'context_tokens' || text === 'tools' || text === 'vision') return false;
  if (text.startsWith('Tanpopo console mount element') || text === 'Default Organization' || text === 'Primary connection' || text === '(prefers-color-scheme: dark)') return false;
  if (/^[\d\s.,:%/$#_+()=-]+$/.test(text) || /^([A-Z][A-Z\d_:-]*|[a-z][\w-]*\/[\w./-]+)$/.test(text)) return false;
  if (!allowSingleWord && !/\s/.test(text)) return false;
  return /[A-Za-z]/.test(text);
}

function normalizeJsxText(value: string): string {
  return value.trim().replace(/\s+/g, ' ');
}

function isInsideFunction(node: ts.Node): boolean {
  for (let parent = node.parent; parent; parent = parent.parent) {
    if (ts.isFunctionLike(parent)) return true;
    if (ts.isSourceFile(parent)) return false;
  }
  return false;
}

function isWithin(node: ts.Node, ancestor: ts.Node): boolean {
  for (let current: ts.Node | undefined = node; current; current = current.parent) {
    if (current === ancestor) return true;
  }
  return false;
}

function templateKey(node: ts.TemplateExpression): {key: string; values: string[]} {
  let key = node.head.text;
  const values: string[] = [];
  for (const span of node.templateSpans) {
    values.push(span.expression.getText());
    key += `{${values.length - 1}}${span.literal.text}`;
  }
  return {key, values};
}

function translateNestedCopy(expression: ts.Expression, sf: ts.SourceFile): string {
  const edits: Array<{start: number; end: number; text: string}> = [];
  const visit = (node: ts.Node) => {
    if (ts.isStringLiteral(node) && isCopy(node.text, true)) {
      let skip = false;
      for (let parent = node.parent; parent && parent !== expression; parent = parent.parent) {
        if (ts.isConditionalExpression(parent) && isWithin(node, parent.condition)) { skip = true; break; }
        if (ts.isCallExpression(parent) && ['t', 'translateUi'].includes(parent.expression.getText(sf))) { skip = true; break; }
      }
      if (!skip) edits.push({start: node.getStart(sf), end: node.getEnd(), text: `translateUi(${JSON.stringify(node.text)})`});
    }
    ts.forEachChild(node, visit);
  };
  visit(expression);
  let result = expression.getText(sf);
  for (const edit of edits.sort((a, b) => b.start - a.start)) {
    const start = edit.start - expression.getStart(sf);
    const end = edit.end - expression.getStart(sf);
    result = result.slice(0, start) + edit.text + result.slice(end);
  }
  return result;
}

function editsForFile(source: string, fileName: string, root: string, emit: boolean) {
  const sf = ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const edits: Array<{start: number; end: number; text: string}> = [];
  const keys = new Set<string>();
  const add = (key: string, start: number, end: number, text?: string) => {
    keys.add(key);
    if (emit && text !== undefined) edits.push({start, end, text});
  };
  const isInCodeElement = (node: ts.Node) => {
    for (let parent = node.parent; parent; parent = parent.parent) {
      if (ts.isJsxElement(parent) && ignoredTags.has(parent.openingElement.tagName.getText(sf).toLowerCase())) return true;
      if (ts.isJsxSelfClosingElement(parent) && ignoredTags.has(parent.tagName.getText(sf).toLowerCase())) return true;
    }
    return false;
  };

  const visit = (node: ts.Node) => {
    if (ts.isJsxText(node) && !isInCodeElement(node)) {
      const raw = node.getText(sf);
      const key = normalizeJsxText(raw);
      if (isCopy(key, true) && key !== 'Tanpopo') {
        const leadingSpace = /^[ \t]/.test(raw) ? ' ' : '';
        const trailingSpace = /[ \t]$/.test(raw) ? ' ' : '';
        add(key, node.getStart(sf), node.getEnd(), `${leadingSpace}{translateUi(${JSON.stringify(key)})}${trailingSpace}`);
      }
    } else if (ts.isJsxAttribute(node) && translatedAttributes.has(node.name.getText(sf)) && node.initializer) {
      if (ts.isStringLiteral(node.initializer) && isCopy(node.initializer.text, true)) {
        add(node.initializer.text, node.initializer.getStart(sf), node.initializer.getEnd(), `{translateUi(${JSON.stringify(node.initializer.text)})}`);
      } else if (ts.isJsxExpression(node.initializer) && node.initializer.expression) {
        const expression = node.initializer.expression;
        if (ts.isTemplateExpression(expression)) {
          const {key} = templateKey(expression);
          if (isCopy(key, true)) {
            const translatedValues = expression.templateSpans.map(span => translateNestedCopy(span.expression, sf));
            add(key, expression.getStart(sf), expression.getEnd(), `translateUi(${JSON.stringify(key)}, [${translatedValues.join(', ')}])`);
          }
        }
      }
    } else if (ts.isJsxExpression(node) && node.expression && ts.isStringLiteral(node.expression) && isCopy(node.expression.text, true)) {
      const literal = node.expression;
      add(literal.text, literal.getStart(sf), literal.getEnd(), `translateUi(${JSON.stringify(literal.text)})`);
    } else if (ts.isTemplateExpression(node)) {
      const isAttributeTemplate = ts.isJsxExpression(node.parent) && ts.isJsxAttribute(node.parent.parent) && translatedProps.has(node.parent.parent.name.getText(sf));
      const withinUi = node.parent && ts.isJsxExpression(node.parent) && !isAttributeTemplate;
      if (withinUi) {
        const {key} = templateKey(node);
        if (isCopy(key, true)) {
          const translatedValues = node.templateSpans.map(span => translateNestedCopy(span.expression, sf));
          add(key, node.getStart(sf), node.getEnd(), `translateUi(${JSON.stringify(key)}, [${translatedValues.join(', ')}])`);
        }
      }
    } else if (ts.isPropertyAssignment(node) && isInsideFunction(node) && translatedProps.has(node.name.getText(sf).replace(/^['"]|['"]$/g, '')) && ts.isStringLiteral(node.initializer)) {
      const propName = node.name.getText(sf).replace(/^['"]|['"]$/g, '');
      const valueLikeProp = ['value', 'activeVal', 'candidateVal'].includes(propName);
      const allowSingleWord = propName !== 'rule' && (!valueLikeProp || translatedValueWords.has(node.initializer.text));
      if (isCopy(node.initializer.text, allowSingleWord)) add(node.initializer.text, node.initializer.getStart(sf), node.initializer.getEnd(), `translateUi(${JSON.stringify(node.initializer.text)})`);
    } else if (ts.isReturnStatement(node) && node.expression && ts.isStringLiteral(node.expression) && isCopy(node.expression.text)) {
      add(node.expression.text, node.expression.getStart(sf), node.expression.getEnd(), `translateUi(${JSON.stringify(node.expression.text)})`);
    } else if (ts.isStringLiteral(node) && !ts.isReturnStatement(node.parent) && !ts.isJsxExpression(node.parent) && !ts.isJsxAttribute(node.parent) && !ts.isPropertyAssignment(node.parent)) {
      let skip = false;
      let allowSingleWord = false;
      for (let parent = node.parent; parent && !ts.isSourceFile(parent); parent = parent.parent) {
        if (ts.isConditionalExpression(parent) && isWithin(node, parent.condition)) { skip = true; break; }
        if (emit && ts.isTemplateExpression(parent)) { skip = true; break; }
        if (ts.isCallExpression(parent) && ['t', 'translateUi'].includes(parent.expression.getText(sf))) { skip = true; break; }
        if (ts.isCallExpression(parent) && parent.expression.getText(sf) === 'useState') { skip = true; break; }
        if (ts.isJsxAttribute(parent)) {
          if (parent.name.getText(sf) === 'code' || !translatedAttributes.has(parent.name.getText(sf))) { skip = true; break; }
        }
        if (ts.isJsxExpression(parent)) {
          if (ts.isJsxAttribute(parent.parent)) {
            const attributeName = parent.parent.name.getText(sf);
            if (!translatedAttributes.has(attributeName)) { skip = true; break; }
            allowSingleWord = true;
          } else {
            allowSingleWord = true;
          }
        }
        if (ts.isPropertyAssignment(parent)) {
          const propertyName = parent.name.getText(sf).replace(/^['"]|['"]$/g, '');
          const valueLikeProp = ['value', 'activeVal', 'candidateVal'].includes(propertyName);
          const valueWord = valueLikeProp && translatedValueWords.has(node.text);
          if (!translatedProps.has(propertyName) || propertyName === 'rule' || valueLikeProp && !valueWord) { skip = true; break; }
          allowSingleWord = true;
        }
        if (ts.isJsxElement(parent) && ignoredTags.has(parent.openingElement.tagName.getText(sf).toLowerCase())) { skip = true; break; }
        if (ts.isJsxSelfClosingElement(parent) && ignoredTags.has(parent.tagName.getText(sf).toLowerCase())) { skip = true; break; }
      }
      if (!skip && isCopy(node.text, allowSingleWord)) add(node.text, node.getStart(sf), node.getEnd(), `translateUi(${JSON.stringify(node.text)})`);
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);

  if (!emit || edits.length === 0) return {keys, code: source};
  const rel = path.relative(path.resolve(root, 'src'), path.dirname(fileName)).split(path.sep).join('/');
  const importPath = `${rel ? '../'.repeat(rel.split('/').length) : './'}i18n/translate`;
  let code = source;
  for (const edit of edits.sort((a, b) => b.start - a.start)) code = code.slice(0, edit.start) + edit.text + code.slice(edit.end);
  const alreadyImportsTranslator = /import\s*\{[^}]*\btranslateUi\b[^}]*\}\s*from\s*['"][^'"]*i18n\/translate['"]/.test(source);
  if (!alreadyImportsTranslator) code = `import {translateUi} from ${JSON.stringify(importPath)};\n${code}`;
  return {keys, code};
}

export function extractLocaleKeys(root: string): string[] {
  const sourceRoot = path.resolve(root, 'src');
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const entry of fs.readdirSync(dir, {withFileTypes: true})) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (/\.(tsx|ts)$/.test(full) && !/\.test\.(tsx|ts)$/.test(full) && !full.includes(`${path.sep}i18n${path.sep}`) && (!full.endsWith('.ts') || path.basename(full) === 'routes.ts')) files.push(full);
    }
  };
  walk(sourceRoot);
  const keys = new Set<string>();
  keys.add('Auto');
  keys.add('Language');
  for (const file of files) {
    const source = fs.readFileSync(file, 'utf8');
    for (const key of editsForFile(source, file, root, false).keys) keys.add(key);
    const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
    if (path.basename(file) === 'primitives.tsx') {
      const collectStateLabels = (node: ts.Node) => {
        if (ts.isVariableDeclaration(node) && node.name.getText(sf) === 'operationalStateLabels' && node.initializer && ts.isObjectLiteralExpression(node.initializer)) {
          for (const property of node.initializer.properties) {
            if (ts.isPropertyAssignment(property) && ts.isStringLiteral(property.initializer)) keys.add(property.initializer.text);
          }
        }
        ts.forEachChild(node, collectStateLabels);
      };
      collectStateLabels(sf);
    }
    const collectCalls = (node: ts.Node) => {
      if (ts.isCallExpression(node) && ['t', 'translateUi'].includes(node.expression.getText(sf))) {
        const collectMessageLiterals = (argument: ts.Node, direct = false) => {
          if (ts.isStringLiteral(argument) && isCopy(argument.text, direct)) keys.add(argument.text);
          ts.forEachChild(argument, child => collectMessageLiterals(child, false));
        };
        for (const argument of node.arguments) collectMessageLiterals(argument, true);
      }
      ts.forEachChild(node, collectCalls);
    };
    collectCalls(sf);
  }
  const formatterPath = path.join(sourceRoot, 'i18n/format.ts');
  if (fs.existsSync(formatterPath)) {
    const formatter = fs.readFileSync(formatterPath, 'utf8');
    const sf = ts.createSourceFile(formatterPath, formatter, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
    const collectFormatterMessages = (node: ts.Node) => {
      if (ts.isCallExpression(node) && node.expression.getText(sf) === 'translateUi' && ts.isStringLiteral(node.arguments[0])) keys.add(node.arguments[0].text);
      ts.forEachChild(node, collectFormatterMessages);
    };
    collectFormatterMessages(sf);
  }
  return [...keys].sort();
}

export function i18nPlugin(): Plugin {
  const root = path.resolve(process.cwd());
  return {
    name: 'tanpopo-console-i18n-static-copy',
    enforce: 'pre',
    buildStart() {
      const scanned = new Set(extractLocaleKeys(root));
      const sourceCatalog = JSON.parse(fs.readFileSync(path.join(root, 'src/i18n/source.json'), 'utf8')) as Record<string, string>;
      const source = new Set(Object.keys(sourceCatalog));
      const missing = [...scanned].filter(key => !source.has(key));
      const extra = [...source].filter(key => !scanned.has(key));
      if (missing.length || extra.length) this.error(`i18n source catalog key mismatch; missing: ${missing.slice(0, 10).join(' | ') || '(none)'}; extra: ${extra.slice(0, 10).join(' | ') || '(none)'}`);

      const placeholders = (value: string) => [...value.matchAll(/\{([\w.]+)\}/g)].map(match => match[1]).sort().join('|');
      for (const locale of supportedLocales) {
        if (locale === 'en') continue;
        const file = path.join(root, 'src/i18n/messages', `${locale}.json`);
        let catalog: Record<string, string>;
        try { catalog = JSON.parse(fs.readFileSync(file, 'utf8')) as Record<string, string>; }
        catch { this.error(`missing or invalid i18n catalog: ${file}`); }
        const keys = new Set(Object.keys(catalog!));
        const absent = [...source].filter(key => !keys.has(key));
        const surplus = [...keys].filter(key => !source.has(key));
        const mismatched = [...source].filter(key => keys.has(key) && placeholders(sourceCatalog[key]) !== placeholders(catalog![key]));
        if (absent.length || surplus.length || mismatched.length) this.error(`${locale} catalog integrity failed; missing ${absent.length}, extra ${surplus.length}, placeholder mismatch ${mismatched.length}`);
      }
    },
    transform(source, id) {
      const file = id.split('?')[0];
      if (!/\.tsx$/.test(file) || file.includes(`${path.sep}i18n${path.sep}`) || file.endsWith('.test.tsx')) return null;
      const result = editsForFile(source, file, root, true);
      return result.code === source ? null : {code: result.code, map: null};
    },
  };
}
