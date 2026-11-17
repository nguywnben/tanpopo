import {readFileSync, writeFileSync} from 'node:fs';
import path from 'node:path';
import {parse} from 'yaml';

const specPath = path.resolve('../../openapi/admin-v1.yaml');
const outputPath = path.resolve('src/api/operation-policy.generated.ts');
const spec = parse(readFileSync(specPath, 'utf8'));
const methods = ['get', 'head', 'options', 'post', 'put', 'patch', 'delete'];
const mutations = new Set(['post', 'put', 'patch', 'delete']);

function resolveParameter(parameter) {
  if (!parameter.$ref) return parameter;
  const name = parameter.$ref.split('/').at(-1);
  return spec.components.parameters[name];
}

const policies = {};
for (const [route, pathItem] of Object.entries(spec.paths)) {
  for (const method of methods) {
    const operation = pathItem[method];
    if (!operation || typeof operation !== 'object') continue;
    const parameters = [...(pathItem.parameters ?? []), ...(operation.parameters ?? [])].map(resolveParameter);
    const headerNames = new Set(parameters.filter(Boolean).map(parameter => parameter.name));
    const key = `${method.toUpperCase()} ${route}`;
    const csrf = headerNames.has('X-CSRF-Token');
    const ifMatch = headerNames.has('If-Match');
    const isPublic = Array.isArray(operation.security) && operation.security.length === 0;
    if (mutations.has(method) && !isPublic && !csrf) {
      throw new Error(`${key} uses inherited authentication but does not declare X-CSRF-Token.`);
    }
    policies[key] = {csrf, ifMatch};
  }
}

const contents = `// Generated from ../../openapi/admin-v1.yaml; do not edit.\nexport const operationPolicy = ${JSON.stringify(policies, null, 2)} as const;\n`;
writeFileSync(outputPath, contents);
