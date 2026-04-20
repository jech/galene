'use strict';

// Registers the service worker, offers a one-tap "enable notifications"
// button (floating, top-right, dismisses itself after grant), and posts
// the resulting push subscription to the backend.

(() => {
    if (!('serviceWorker' in navigator) ||
        !('PushManager' in window) ||
        !('Notification' in window)) {
        return;
    }
    if (window.location.protocol !== 'https:' &&
        window.location.hostname !== 'localhost') {
        return;
    }

    const SUBSCRIBE_URL = '/push/subscribe';
    const VAPID_URL = '/push/vapid-public-key';

    function b64urlToUint8(s) {
        const pad = '='.repeat((4 - s.length % 4) % 4);
        const b64 = (s + pad).replace(/-/g, '+').replace(/_/g, '/');
        const raw = atob(b64);
        const out = new Uint8Array(raw.length);
        for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
        return out;
    }

    async function register() {
        return navigator.serviceWorker.register('/service-worker.js', {scope: '/'});
    }

    async function subscribe(reg) {
        const key = await fetch(VAPID_URL).then(r => r.ok ? r.text() : null);
        if (!key) throw new Error('no VAPID key');
        const sub = await reg.pushManager.subscribe({
            userVisibleOnly: true,
            applicationServerKey: b64urlToUint8(key.trim()),
        });
        await fetch(SUBSCRIBE_URL, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(sub),
        });
        return sub;
    }

    function makeButton() {
        const btn = document.createElement('button');
        btn.textContent = '🔔 Notify me on calls';
        btn.style.cssText = [
            'position:fixed', 'top:12px', 'right:12px', 'z-index:9999',
            'padding:8px 14px', 'border-radius:8px', 'border:none',
            'background:#1f2937', 'color:#fff', 'font:14px sans-serif',
            'cursor:pointer', 'box-shadow:0 2px 6px rgba(0,0,0,0.25)',
        ].join(';');
        btn.onclick = async () => {
            btn.disabled = true;
            btn.textContent = '…';
            try {
                const perm = await Notification.requestPermission();
                if (perm !== 'granted') {
                    btn.remove();
                    return;
                }
                const reg = await register();
                await subscribe(reg);
                btn.remove();
            } catch (e) {
                console.error('push subscribe failed', e);
                btn.textContent = 'Notifications failed';
            }
        };
        document.body.appendChild(btn);
    }

    async function init() {
        const reg = await register();
        const existing = await reg.pushManager.getSubscription();
        if (existing) return;
        if (Notification.permission === 'granted') {
            await subscribe(reg);
            return;
        }
        if (Notification.permission === 'denied') return;
        if (document.body) makeButton();
        else window.addEventListener('DOMContentLoaded', makeButton);
    }

    init().catch(e => console.error('push init failed', e));
})();
