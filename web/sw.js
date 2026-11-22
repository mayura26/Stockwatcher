const CACHE = 'stockwatcher-v7';
const SHELL = [
  '/',
  '/css/app.css',
  '/js/app.js',
  '/js/api.js',
  '/js/format.js',
  '/js/futures-mark.js',
  '/js/board.js',
  '/js/chart-view.js',
  '/js/search.js',
  '/js/alerts.js',
  '/js/settings.js',
  '/manifest.webmanifest',
  '/pwa-192.png',
  '/pwa-512.png',
  '/apple-touch-180.png',
  '/badge-96.png',
];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key)))).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/api/')) return;
  event.respondWith((async () => {
    try {
      const res = await fetch(event.request);
      if (res.ok && url.pathname !== '/sw.js') {
        const cache = await caches.open(CACHE);
        cache.put(event.request, res.clone());
      }
      return res;
    } catch (err) {
      const cached = await caches.match(event.request);
      if (cached) return cached;
      if (event.request.mode === 'navigate') {
        const home = await caches.match('/');
        if (home) return home;
      }
      throw err;
    }
  })());
});

self.addEventListener('push', (event) => {
  let data = { title: 'Stockwatcher', body: '' };
  try {
    if (event.data) data = { ...data, ...event.data.json() };
  } catch (err) {
    data.body = event.data ? event.data.text() : '';
  }
  event.waitUntil(self.registration.showNotification(data.title, {
    body: data.body,
    tag: data.alert_id ? 'alert-' + data.alert_id : 'stockwatcher',
    data,
    icon: '/pwa-192.png',
    badge: '/badge-96.png',
  }));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const id = event.notification.data && event.notification.data.symbol_id;
  const url = id ? '/#/chart/' + id : '/#/';
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of all) {
      await client.focus();
      if ('navigate' in client) {
        try {
          await client.navigate(url);
          return;
        } catch (err) {
          /* fall through to a message */
        }
      }
      client.postMessage({ type: 'navigate', url });
      return;
    }
    await self.clients.openWindow(url);
  })());
});
