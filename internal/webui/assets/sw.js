// gx-torrent's service worker: the page shell and the shared assets are cached
// so an installed app opens immediately, while every authenticated page and
// API call stays online-only. Nothing personal is stored in the cache.
const CACHE = 'gx-torrent-v1';
const ASSETS = ['/assets/theme.css', '/assets/common.js', '/assets/icons.svg', '/favicon.ico'];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(ASSETS)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key)))).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (request.method !== 'GET') return;
  const url = new URL(request.url);
  if (url.origin !== location.origin) return;
  const shared = url.pathname.startsWith('/assets/') || url.pathname === '/favicon.ico';
  if (!shared) return;
  // Network first: a new release updates the style and script at once when the
  // browser is online; the cached copy is only the offline fallback.
  event.respondWith(
    fetch(request)
      .then((response) => {
        if (response.ok) caches.open(CACHE).then((cache) => cache.put(request, response.clone()));
        return response;
      })
      .catch(() => caches.match(request))
  );
});
