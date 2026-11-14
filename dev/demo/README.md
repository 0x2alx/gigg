# Demo instance and documentation screenshots

`docs/images/*.png` are taken from a demo instance filled with made-up data (no real names or data),
half in the Nightwalker theme and half in Daywalker, so they can be re-taken whenever the UI changes.
The commands assume the dev setup from `dev/README.md` (the `gigg-dev` network, `localhost/gigg:dev`,
the `ui-shot` image and a TLS certificate in `scratch/`).

1. Bootstrap a fresh data volume. `scratch/demo/` must hold `demo.pw` (the demo users' password,
   `demo-password-2026`, one line) and be writable by UID 65532, which writes the seed keys there:

       mkdir -p scratch/demo && echo demo-password-2026 > scratch/demo/demo.pw
       podman unshare chown -R 65532 scratch/demo
       podman run --rm -v gigg-demo:/data -v "$PWD/scratch/demo":/s -v "$PWD/dev/demo":/b:ro \
         localhost/gigg:dev admin batch /b/bootstrap.txt

2. Start it as `gigg-demo` on the `gigg-dev` network, like the dev instance in `dev/README.md` but
   with `GIGG_PUBLIC_URL=https://gigg-demo:8443`, `GIGG_ALLOWED_HOSTS=gigg-demo,gigg` and the
   `gigg-demo` volume.
3. Fill it (Node 24, no dependencies; as UID 65532 so it can read the key files):

       podman run --rm --user 65532 --network gigg-dev -v "$PWD/dev/demo":/d:ro -v "$PWD/scratch/demo":/s:ro \
         docker.io/library/node:24-alpine node /d/seed.mjs http://gigg-demo:8080 /s

4. Write the scenario and run it with the screenshot runner:

       python3 dev/shot/docs-scenario.py
       podman run --rm --network gigg-dev -v "$PWD/dev/shot":/shot:ro -v "$PWD/out":/out localhost/ui-shot \
         node /shot/run.js /shot/scenarios/docs.json /out/docs

5. Copy `out/docs/*.png` to `docs/images/`.

The scenario signs in as `dana`, switches themes through the API between shots, and names each file
after its page and theme (`board-all-night.png`, `task-panel-day.png`, ...), the names the guide uses.
