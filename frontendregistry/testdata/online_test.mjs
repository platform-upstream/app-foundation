// Module `api`, online.ts: telling a request that never reached a server from an answer.
// Started by TestFrontendModuleLogic.
import assert from 'node:assert/strict';
import { load } from './ts.mjs';

const { isOfflineError, isOnline } = await load('api/online.ts');
const { APIError } = await load('api/api.ts');

const setOnline = (value) => Object.defineProperty(globalThis, 'navigator', { value: value === undefined ? {} : { onLine: value }, configurable: true, writable: true });

setOnline(true);
assert.equal(isOnline(), true);
for (const message of ['Failed to fetch', 'NetworkError when attempting to fetch resource.', 'Load failed', 'network connection was lost']) {
  assert.equal(isOfflineError(new TypeError(message)), true, message);
}
assert.equal(isOfflineError(new APIError(503, 'Service Unavailable')), false, 'a server answer');
assert.equal(isOfflineError(new APIError(401, 'Authentication required.')), false, 'an expired session');
assert.equal(isOfflineError(new TypeError("Cannot read properties of undefined (reading 'x')")), false, 'a bug');
assert.equal(isOfflineError(new Error('Failed to fetch')), false, 'not a TypeError');
assert.equal(isOfflineError('Failed to fetch'), false);
assert.equal(isOfflineError(undefined), false);

setOnline(false);
assert.equal(isOnline(), false);
assert.equal(isOfflineError(new Error('anything')), true, 'offline: any failure');
assert.equal(isOfflineError(undefined), true);
assert.equal(isOfflineError(new APIError(500, 'x')), false, 'an APIError is an answer even then');

setOnline(undefined);
assert.equal(isOnline(), true, 'unknown counts as online');

console.log('online ok');
