// img.aoc-codex.app — the armory tooltip images, served from R2 through this Worker's binding.
//
// Why a Worker and not R2's own custom domain (AOC-041): on 2026-09-25, pinned to the same
// Cloudflare edge (SIN) with the same objects, the R2 custom domain delivered 119 of 160 images
// whole while a Worker reading the same bucket through a binding delivered 160 of 160. The
// custom-domain path stalls; the binding does not. The mechanism inside Cloudflare is unknown.
//
// What it does, and nothing else:
//   * GET and HEAD of keys under armory/ only. Everything else is 404 or 405.
//   * One edge-cache entry per object: the query string never reaches the cache key, so a
//     cache-buster cannot multiply entries or force a bucket read on every request.
//   * Cache-Control comes from the object's own metadata (the uploader sets one year, immutable).
//   * ETag + If-None-Match -> 304.
//   * A bucket error is 503 with no-store, so an outage is never cached as if it were the image.

const PREFIX = 'armory/';
const FALLBACK_CACHE = 'public, max-age=31536000, immutable';
const MAX_KEY = 512;

function plain(status, text, extra = {}) {
  return new Response(text, {
    status,
    headers: { 'content-type': 'text/plain; charset=utf-8', 'cache-control': 'public, max-age=60', ...extra },
  });
}

export function objectKey(pathname) {
  let key;
  try {
    key = decodeURIComponent(pathname.slice(1));
  } catch {
    return null;
  }
  // The prefix is what bounds access. There is no '..' check on purpose: URL parsing has already
  // resolved dot segments before this runs, and R2 keys are flat strings with no directories to
  // climb out of, so such a check could never fire (AOC-041 build, measured by mutation).
  if (!key.startsWith(PREFIX) || key.length > MAX_KEY) return null;
  return key;
}

export default {
  async fetch(request, env, ctx) {
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      return plain(405, 'method not allowed', { allow: 'GET, HEAD' });
    }
    const url = new URL(request.url);
    const key = objectKey(url.pathname);
    if (key === null) return plain(404, 'not found');

    const cacheKey = new Request(url.origin + url.pathname, { method: 'GET' });
    const cache = caches.default;
    let resp = await cache.match(cacheKey);
    let state = 'hit';
    if (!resp) {
      state = 'miss';
      let obj;
      try {
        obj = await env.BUCKET.get(key);
      } catch {
        return new Response('storage unavailable', {
          status: 503,
          headers: { 'content-type': 'text/plain; charset=utf-8', 'cache-control': 'no-store', 'retry-after': '5' },
        });
      }
      if (obj === null) return plain(404, 'not found');
      const headers = new Headers();
      obj.writeHttpMetadata(headers);
      if (!headers.has('cache-control')) headers.set('cache-control', FALLBACK_CACHE);
      headers.set('etag', obj.httpEtag);
      resp = new Response(obj.body, { headers });
      ctx.waitUntil(cache.put(cacheKey, resp.clone()));
    }

    const headers = new Headers(resp.headers);
    headers.set('x-aoc-cache', state);
    const inm = request.headers.get('if-none-match');
    if (inm && inm === headers.get('etag')) return new Response(null, { status: 304, headers });
    if (request.method === 'HEAD') return new Response(null, { status: 200, headers });
    return new Response(resp.body, { status: 200, headers });
  },
};
