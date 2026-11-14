// gigg registers no service worker. This file only exists to remove one that another app left on
// this address (for example a board that ran here before gigg): a browser checks a registered
// worker's script for updates, gets this one, which clears that app's caches, unregisters itself
// and reloads its open pages, so they talk to gigg directly again.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) await caches.delete(key);
      await self.registration.unregister();
      for (const client of await self.clients.matchAll({ type: "window" })) client.navigate(client.url);
    })(),
  );
});
