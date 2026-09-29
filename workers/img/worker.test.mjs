// Tests for worker.mjs. Node's built-in runner, no packages:  node --test workers/img/worker.test.mjs
// The bucket and the edge cache are stand-ins; object names are obviously fake.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import worker, { objectKey } from './worker.mjs';

const JPEG = 'image/jpeg';
const YEAR = 'public, max-age=31536000, immutable';

function setup(objects = {}, { failBucket = false } = {}) {
  const store = new Map();
  globalThis.caches = {
    default: {
      async match(req) { const r = store.get(req.url); return r ? r.clone() : undefined; },
      async put(req, resp) { store.set(req.url, resp); },
    },
  };
  const calls = [];
  const env = {
    BUCKET: {
      async get(key) {
        calls.push(key);
        if (failBucket) throw new Error('simulated storage failure');
        const o = objects[key];
        if (!o) return null;
        return {
          body: o.body,
          httpEtag: o.etag,
          writeHttpMetadata(h) {
            if (o.type) h.set('content-type', o.type);
            if (o.cache) h.set('cache-control', o.cache);
          },
        };
      },
    },
  };
  const pending = [];
  const ctx = { waitUntil(p) { pending.push(p); } };
  const call = async (path, init = {}) => {
    const r = await worker.fetch(new Request('https://img.aoc-codex.app' + path, init), env, ctx);
    await Promise.all(pending.splice(0));
    return r;
  };
  return { call, calls, store };
}

const ALPHA = { 'armory/test_alpha.jpg': { body: 'ALPHA-BYTES', etag: '"e-alpha"', type: JPEG, cache: YEAR } };

test('a first GET reads the bucket and serves the object with its own metadata', async () => {
  const { call, calls } = setup(ALPHA);
  const r = await call('/armory/test_alpha.jpg');
  assert.equal(r.status, 200);
  assert.equal(await r.text(), 'ALPHA-BYTES');
  assert.equal(r.headers.get('content-type'), JPEG);
  assert.equal(r.headers.get('cache-control'), YEAR);
  assert.equal(r.headers.get('etag'), '"e-alpha"');
  assert.equal(r.headers.get('x-aoc-cache'), 'miss');
  assert.deepEqual(calls, ['armory/test_alpha.jpg']);
});

test('a cache-buster does not reach the cache key: the second GET is a hit and skips the bucket', async () => {
  const { call, calls, store } = setup(ALPHA);
  await call('/armory/test_alpha.jpg?cb=1');
  const r = await call('/armory/test_alpha.jpg?cb=2');
  assert.equal(r.headers.get('x-aoc-cache'), 'hit');
  assert.equal(await r.text(), 'ALPHA-BYTES');
  assert.equal(calls.length, 1, 'the bucket is read once');
  assert.deepEqual([...store.keys()], ['https://img.aoc-codex.app/armory/test_alpha.jpg']);
});

test('HEAD returns the headers and no body', async () => {
  const { call } = setup(ALPHA);
  const r = await call('/armory/test_alpha.jpg', { method: 'HEAD' });
  assert.equal(r.status, 200);
  assert.equal(r.headers.get('content-type'), JPEG);
  assert.equal(await r.text(), '');
});

test('If-None-Match with the current ETag is a 304 with no body', async () => {
  const { call } = setup(ALPHA);
  const r = await call('/armory/test_alpha.jpg', { headers: { 'if-none-match': '"e-alpha"' } });
  assert.equal(r.status, 304);
  assert.equal(await r.text(), '');
  const stale = await call('/armory/test_alpha.jpg', { headers: { 'if-none-match': '"e-old"' } });
  assert.equal(stale.status, 200);
});

test('only GET and HEAD are allowed', async () => {
  const { call, calls } = setup(ALPHA);
  for (const method of ['POST', 'PUT', 'DELETE']) {
    const r = await call('/armory/test_alpha.jpg', { method });
    assert.equal(r.status, 405);
    assert.equal(r.headers.get('allow'), 'GET, HEAD');
  }
  assert.equal(calls.length, 0);
});

test('keys outside armory/, traversal and malformed escapes are 404 without touching the bucket', async () => {
  const { call, calls } = setup(ALPHA);
  for (const p of ['/', '/test_alpha.jpg', '/other/test_alpha.jpg', '/armory/../secret',
                   '/armory/%2e%2e/secret', '/armory/%E0%A4%A',
                   '/armory/' + 'x'.repeat(600)]) {
    const r = await call(p);
    assert.equal(r.status, 404, p);
  }
  assert.equal(calls.length, 0);
});

test('names with brackets and parentheses decode to their real keys', async () => {
  assert.equal(objectKey('/armory/%5Btest_gamma%5D.jpg'), 'armory/[test_gamma].jpg');
  assert.equal(objectKey('/armory/test_delta_%28nm%29.jpg'), 'armory/test_delta_(nm).jpg');
  assert.equal(objectKey('/armory/test_delta_(nm).jpg'), 'armory/test_delta_(nm).jpg');
});

test('a missing object is 404 and is not put in the edge cache', async () => {
  const { call, store } = setup(ALPHA);
  const r = await call('/armory/test_missing.jpg');
  assert.equal(r.status, 404);
  assert.equal(store.size, 0);
});

test('a bucket failure is 503 no-store, and is never cached as the image', async () => {
  const { call, store } = setup(ALPHA, { failBucket: true });
  const r = await call('/armory/test_alpha.jpg');
  assert.equal(r.status, 503);
  assert.equal(r.headers.get('cache-control'), 'no-store');
  assert.equal(store.size, 0);
});

test('an object uploaded without Cache-Control still gets the one-year immutable header', async () => {
  const { call } = setup({ 'armory/test_beta.jpg': { body: 'B', etag: '"e-b"', type: JPEG } });
  const r = await call('/armory/test_beta.jpg');
  assert.equal(r.headers.get('cache-control'), YEAR);
});
