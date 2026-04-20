'use strict';

self.addEventListener('install', () => {
    self.skipWaiting();
});

self.addEventListener('activate', (event) => {
    event.waitUntil(self.clients.claim());
});

self.addEventListener('push', (event) => {
    let payload = {};
    try {
        payload = event.data ? event.data.json() : {};
    } catch (_) {
        payload = {body: event.data ? event.data.text() : ''};
    }

    const title = payload.title || 'Call started';
    const options = {
        body: payload.body || 'Someone just joined.',
        icon: '/icons/icon-192.png',
        badge: '/icons/icon-192.png',
        tag: payload.tag || 'galene-call',
        renotify: true,
        data: {url: payload.url || '/'},
    };
    event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const url = (event.notification.data && event.notification.data.url) || '/';
    event.waitUntil((async () => {
        const all = await self.clients.matchAll({type: 'window', includeUncontrolled: true});
        for (const c of all) {
            if (c.url.includes(url) && 'focus' in c) return c.focus();
        }
        if (self.clients.openWindow) return self.clients.openWindow(url);
    })());
});
