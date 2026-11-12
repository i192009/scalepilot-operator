// Optional evidence capture, outside the Go control plane.
// Run with the pinned Playwright image documented in docs/deployment.md.
import { mkdir, writeFile } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? '/tmp/scalepilot-capture/node_modules/playwright/index.mjs');
const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage({ viewport: { width: 1600, height: 1100 }, deviceScaleFactor: 1, locale: 'en-GB' });
const errors = [];
page.on('response', response => { if (response.url().includes('/api/ds/query') && response.status() >= 400) errors.push(`${response.status()} ${response.url()}`); });
page.on('console', async message => { if (message.type() === 'error') {console.log(`browser: ${message.text()}`);for(const arg of message.args()){try{const value=await arg.evaluate(v=>v instanceof Error?{message:v.message,cause:v.cause?.message,stack:v.cause?.stack}:null);if(value) console.log(JSON.stringify(value));}catch{}}} });
page.on('pageerror', error => console.log(`page error: ${error.message}`));
page.on('requestfailed', request => console.log(`request failed: ${request.url()} ${request.failure()?.errorText}`));
const base = process.env.GRAFANA_URL ?? 'http://scalepilot-dev:3000';
await page.goto(`${base}/d/scalepilot/scalepilot-workload-autoscaling?orgId=1&from=now-30m&to=now&var-namespace=demo&var-policy=document-worker-policy`, { waitUntil: 'domcontentloaded', timeout: 90000 });
try {
  await page.getByText('Active policies', { exact: true }).waitFor({ timeout: 90000 });
} catch (error) {
  await mkdir('/workspace/artifacts', { recursive: true });
  await page.screenshot({ path: '/workspace/artifacts/grafana-capture-failure.png' });
  console.log((await page.locator('body').innerText()).slice(0, 3000));
  throw error;
}
await page.waitForTimeout(20000);
if (errors.length) throw new Error(errors.join('\n'));
if (await page.getByText('Error loading: stat', { exact: false }).count() || await page.getByText('Could not load plugin', { exact: false }).count()) throw new Error('Grafana plugin failed to load; screenshot is not valid evidence');
await mkdir('/workspace/docs/images', { recursive: true });
await page.screenshot({ path: '/workspace/docs/images/grafana-overview.png' });
for (let attempt = 0; attempt < 12; attempt++) {
  if (await page.getByText('Replica decisions and actual readiness', { exact: true }).count()) break;
  await page.mouse.wheel(0, 600);
  await page.waitForTimeout(700);
}
await page.getByText('Replica decisions and actual readiness', { exact: true }).scrollIntoViewIfNeeded();
await page.waitForTimeout(5000);
await page.screenshot({ path: '/workspace/docs/images/grafana-decisions.png' });
const metadata = { url: page.url(), capturedAt: new Date().toISOString(), queryFailures: errors };
await writeFile('/workspace/docs/images/capture.json', JSON.stringify(metadata, null, 2));
console.log(JSON.stringify(metadata));
await browser.close();
