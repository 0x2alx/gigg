// Scenario runner for UI checks (puppeteer-core + Chromium in the ui-shot container).
//   node run.js <scenario.json> <out-dir>
// A scenario: {"base": "https://gigg-dev:8443", "viewport": [1440, 900], "steps": [...]}.
// Steps: {"goto": "/path"}, {"click": "css"}, {"type": ["css", "text"]}, {"press": "Enter"},
//        {"wait": "css"} or {"wait": 500}, {"viewport": [w, h]}, {"hover": "css"},
//        {"shot": "name"} (full page; "full": false = the viewport; "el": "css" = one element, "margin": px),
//        {"dom": "name"} (HTML + accessibility tree), {"eval": "js expression", "expect": value}.
// Always written: log.json with console messages, page errors, failed requests, CSP violations and
// the result of every step. Exit 1 if a step fails or an "expect" doesn't match.
const fs = require("fs");
const path = require("path");
const puppeteer = require("puppeteer-core");

(async () => {
  const [scenarioPath, outDir] = process.argv.slice(2);
  const sc = JSON.parse(fs.readFileSync(scenarioPath, "utf8"));
  fs.mkdirSync(outDir, { recursive: true });
  const log = { scenario: path.basename(scenarioPath), console: [], pageErrors: [], failedRequests: [], csp: [], steps: [] };
  const browser = await puppeteer.launch({
    executablePath: process.env.PUPPETEER_EXECUTABLE_PATH || "/usr/bin/chromium-browser",
    acceptInsecureCerts: true,
    args: ["--no-sandbox", "--disable-gpu", "--ignore-certificate-errors"],
  });
  let failed = false;
  try {
    const page = await browser.newPage();
    const [w, h] = sc.viewport || [1440, 900];
    await page.setViewport({ width: w, height: h });
    page.on("console", (m) => {
      log.console.push({ type: m.type(), text: m.text() });
      if (/Content Security Policy/i.test(m.text())) log.csp.push(m.text());
    });
    page.on("pageerror", (e) => log.pageErrors.push(String(e)));
    page.on("requestfailed", (r) => log.failedRequests.push({ url: r.url(), error: r.failure()?.errorText }));
    page.on("response", (r) => { if (r.status() >= 400) log.failedRequests.push({ url: r.url(), status: r.status() }); });
    for (const step of sc.steps) {
      const rec = { step };
      try {
        // networkidle2 tolerates the SSE stream of one or two workspaces; with more, a scenario sets
        // "waitUntil": "domcontentloaded" and waits for its own selectors
        if (step.goto !== undefined) await page.goto(sc.base + step.goto, { waitUntil: sc.waitUntil || "networkidle2" });
        else if (step.click) await page.click(step.click);
        else if (step.type) await page.type(step.type[0], step.type[1]);
        else if (step.press) {
          // a key, or a chord like "Control+Enter" (modifiers held while the last key is pressed)
          const ks = step.press.split("+");
          for (const k of ks.slice(0, -1)) await page.keyboard.down(k);
          await page.keyboard.press(ks[ks.length - 1]);
          for (const k of ks.slice(0, -1).reverse()) await page.keyboard.up(k);
        }
        else if (typeof step.wait === "number") await new Promise((r) => setTimeout(r, step.wait));
        else if (step.wait) await page.waitForSelector(step.wait, { timeout: 10000 });
        else if (step.viewport) await page.setViewport({ width: step.viewport[0], height: step.viewport[1] });
        else if (step.hover) await page.hover(step.hover);
        else if (step.shot && step.el) {
          // one element (with a margin), e.g. a dialog or a section
          const box = await (await page.waitForSelector(step.el, { timeout: 10000 })).boundingBox();
          const m = step.margin ?? 12;
          await page.screenshot({ path: path.join(outDir, step.shot + ".png"), clip: { x: Math.max(0, box.x - m), y: Math.max(0, box.y - m), width: box.width + 2 * m, height: box.height + 2 * m },
            // as it is on screen: capturing beyond the viewport resizes the page, and a scrolled panel jumps
            captureBeyondViewport: false });
        } else if (step.shot) await page.screenshot({ path: path.join(outDir, step.shot + ".png"), fullPage: step.full !== false });
        else if (step.dom) {
          fs.writeFileSync(path.join(outDir, step.dom + ".html"), await page.content());
          fs.writeFileSync(path.join(outDir, step.dom + ".a11y.json"), JSON.stringify(await page.accessibility.snapshot(), null, 1));
        } else if (step.eval) {
          rec.value = await page.evaluate(step.eval);
          if ("expect" in step && JSON.stringify(rec.value) !== JSON.stringify(step.expect)) {
            rec.error = `expected ${JSON.stringify(step.expect)}`;
          }
        }
      } catch (e) {
        rec.error = String(e);
      }
      if (rec.error) failed = true;
      log.steps.push(rec);
    }
  } finally {
    await browser.close();
    if (log.pageErrors.length || log.csp.length) failed = true;
    log.ok = !failed;
    fs.writeFileSync(path.join(outDir, "log.json"), JSON.stringify(log, null, 1));
    console.log(JSON.stringify({ ok: log.ok, steps: log.steps.map((s) => s.error || "ok"), csp: log.csp, pageErrors: log.pageErrors, failedRequests: log.failedRequests }));
  }
  process.exit(failed ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(1); });
