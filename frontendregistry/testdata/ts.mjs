// Lets a Node test import a registry module's TypeScript source directly (Node strips the types):
// `./x.js` resolves to `./x.ts` when only that exists, and `react` to a stub, because the tests
// call the modules' plain functions, never a hook. The test is started with the sources directory
// as its argument (TestFrontendModuleLogic); on a Node without type stripping it prints SKIP.
import * as nodeModule from 'node:module';
import { existsSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import path from 'node:path';

/** Whether this Node can strip TypeScript and register resolve hooks (22.18+). */
export const supported = Boolean(process.features?.typescript) && typeof nodeModule.registerHooks === 'function';

const reactStub = 'data:text/javascript,' + encodeURIComponent(
  ['useCallback', 'useEffect', 'useLayoutEffect', 'useMemo', 'useRef', 'useState', 'useSyncExternalStore']
    .map((name) => `export function ${name}() { throw new Error('hook ${name} called in a Node test'); }`).join('\n'),
);

if (supported) {
  nodeModule.registerHooks({
    resolve(specifier, context, next) {
      if (specifier === 'react') return { url: reactStub, shortCircuit: true };
      if (specifier.startsWith('.') && specifier.endsWith('.js') && context.parentURL?.startsWith('file:')) {
        const js = path.resolve(path.dirname(fileURLToPath(context.parentURL)), specifier);
        const ts = js.replace(/\.js$/, '.ts');
        if (!existsSync(js) && existsSync(ts)) return { url: pathToFileURL(ts).href, shortCircuit: true };
      }
      return next(specifier, context);
    },
  });
}

/** The path of a registry source (relative to frontendregistry/sources). */
export function source(name) {
  return path.join(process.argv[2], name);
}

/** Imports a registry source; exits with SKIP when this Node cannot run TypeScript. */
export async function load(name) {
  if (!supported) {
    console.log('SKIP: Node without TypeScript type stripping');
    process.exit(0);
  }
  return import(pathToFileURL(source(name)).href);
}
