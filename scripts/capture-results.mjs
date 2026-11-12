// Browser captures of recorded results, styled to match the dark Grafana dashboard.
import { readFile, readdir, mkdir, writeFile, copyFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? '/tmp/scalepilot-capture/node_modules/playwright/index.mjs');
const dir = '/workspace/benchmarks/reports';
const output = '/workspace/screenshots';
await mkdir(output, { recursive: true });
const files = (await readdir(dir)).filter(name => /^(hpa|pilot)-.*\.json$/.test(name)).sort();
const reports = await Promise.all(files.map(async name => ({ name, ...JSON.parse(await readFile(`${dir}/${name}`, 'utf8')) })));
if (!reports.length || reports.some(r => r.Completed + r.Failed !== r.Jobs)) throw new Error('Missing or incomplete job audits');
const completed = reports.reduce((n, r) => n + r.Completed, 0);
const failed = reports.reduce((n, r) => n + r.Failed, 0);
const pairs = new Map();
for (const r of reports) {
  const key = r.name.replace(/^(hpa|pilot)-/, '').replace(/\.json$/, '');
  if (!pairs.has(key)) pairs.set(key, {});
  pairs.get(key)[r.Strategy] = r;
}
if ([...pairs.values()].some(p => !p.hpa || !p.pilot)) throw new Error('Missing comparison arm');
const escape = value => String(value).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const plain = value => value.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/\*\*/g, '').replace(/`/g, '');
const f = value => Number(value).toFixed(2);
const style = `*{box-sizing:border-box}body{margin:0;background:#111217;color:#e5e7eb;font:16px Arial,sans-serif}main{padding:46px 52px}.brand{color:#ff9830;font-size:15px;font-weight:700;letter-spacing:3px}.eyebrow{color:#9ba7b5;margin:20px 0 12px;font-size:14px;letter-spacing:1.5px}h1{font-size:38px;font-weight:600;margin:0 0 14px}p{color:#b5bdc9;line-height:1.6;margin:10px 0 24px}.cards{display:grid;grid-template-columns:repeat(3,1fr);gap:18px;margin:28px 0}.card{border:1px solid #34373e;background:#181b1f;border-radius:6px;padding:24px}.label{color:#b5bdc9;font-size:15px}.value{color:#73bf69;font-size:46px;line-height:1.3;margin-top:8px}.panel{border:1px solid #34373e;background:#181b1f;border-radius:6px;overflow:hidden;margin-top:24px}.panel h2{font-size:20px;font-weight:500;margin:0;padding:22px 24px;border-bottom:1px solid #34373e}table{border-collapse:collapse;width:100%;font-size:15px}th{text-align:left;font-weight:500;color:#a9bad0;background:#202329}th,td{padding:15px 22px;border-bottom:1px solid #2d3037;line-height:1.5}tr:last-child td{border-bottom:0}td:nth-child(n+2){font-variant-numeric:tabular-nums}.hpa{color:#ffb357}.pilot{color:#73bf69}.note{margin:24px 0 0;padding:20px 24px;border-left:3px solid #ff9830;background:#181b1f;font-size:15px;line-height:1.6;color:#b5bdc9}.source{margin-top:24px;color:#91a0b2;font-size:13px;line-height:1.7}.status{color:#73bf69}`;
const shell = (title, body) => `<!doctype html><html lang="en"><meta charset="utf-8"><title>${escape(title)}</title><style>${style}</style><main><div class="brand">SCALEPILOT</div><div class="eyebrow">RECORDED LOCAL RESULTS · 12 NOVEMBER 2026</div><h1>${escape(title)}</h1>${body}</main></html>`;
const tableRows = [...pairs.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([key, p]) => `<tr><td>${escape(key.replaceAll('-', ' '))}</td><td>${p.hpa.Jobs}</td><td><span class="hpa">${f(p.hpa.DurationSeconds)}</span> / <span class="pilot">${f(p.pilot.DurationSeconds)}</span></td><td><span class="hpa">${f(p.hpa.P95WaitSeconds)}</span> / <span class="pilot">${f(p.pilot.P95WaitSeconds)}</span></td><td><span class="hpa">${f(p.hpa.PodSeconds)}</span> / <span class="pilot">${f(p.pilot.PodSeconds)}</span></td></tr>`).join('');
const benchmark = shell('CPU HPA / queue-aware scaling', `<p>Equivalent worker images, seeded job durations, one initial replica and six maximum.<br>Separate queues; sequential arms with CPU HPA first and a 30-second warm-up.</p><div class="cards"><div class="card"><div class="label">Completed jobs</div><div class="value">${completed.toLocaleString('en-GB')}</div></div><div class="card"><div class="label">Audited comparison arms</div><div class="value">${reports.length}</div></div><div class="card"><div class="label">Failed jobs</div><div class="value">${failed}</div></div></div><div class="panel"><h2>Measured pairs · <span class="hpa">CPU HPA</span> / <span class="pilot">ScalePilot</span></h2><table><thead><tr><th>Profile / repetition</th><th>Jobs per arm</th><th>Drain seconds</th><th>p95 queue wait seconds</th><th>Observed Pod-seconds</th></tr></thead><tbody>${tableRows}</tbody></table></div><div class="note">Local Docker Desktop / ARM64 · 8 CPUs · 7.55 GiB · two-node Kind Kubernetes 1.37.0.<br>Fixed arm ordering; short runs; no statistical significance or universal efficiency claim. Failure-run sampling gaps make resource integrals approximate. Controller restart topologies differ. Pod-seconds exclude a long post-drain downscale tail.</div><div class="source">Source: benchmarks/reports/hpa-*.json and pilot-*.json, excluding exploratory/.<br>Methodology and full resource-sampling limitations: docs/hpa-comparison.md.</div>`);
const verification = await readFile(`${dir}/verification.md`, 'utf8');
const checks = verification.split(/\r?\n/).filter(line => line.startsWith('| ') && !line.startsWith('| Check')).map(line => line.split('|').slice(1, -1).map(v => plain(v.trim())));
const verificationPage = shell('Executed verification', `<p>Recorded checks from real Go builds, API servers, Redis containers and Kubernetes Pods.<br>Windows / ARM64 host with Docker Desktop Linux / ARM64; verification evidence stays in the repository.</p><div class="panel"><h2>Local checks and observed outcomes</h2><table><thead><tr><th style="width:38%">Check</th><th>Recorded result</th></tr></thead><tbody>${checks.map(([name, result]) => `<tr><td>${escape(name)}</td><td class="status">${escape(result)}</td></tr>`).join('')}</tbody></table></div><div class="note">These are local verification results, not a production certification. Hosted CI and release publication are separate from the checks shown. Alternate CNI enforcement, prolonged active-workload soak tests and OTLP backend delivery remain unverified.</div><div class="source">Source: benchmarks/reports/verification.md and its linked raw logs / JSON.<br>Actual browser-captured report; values are read from recorded evidence, not synthesized.</div>`);
const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage({ viewport: { width: 1512, height: 1000 }, deviceScaleFactor: 1, locale: 'en-GB' });
const captured = [];
for (const [name, html] of [['benchmark-results', benchmark], ['verification-results', verificationPage]]) {
  await page.setContent(html, { waitUntil: 'load' });
  await page.screenshot({ path: `${output}/${name}.png`, fullPage: true });
  captured.push({ file: `${name}.png`, width: 1512, height: await page.locator('html').evaluate(el => el.scrollHeight) });
}
await browser.close();
for (const name of ['grafana-overview.png', 'grafana-decisions.png']) await copyFile(`/workspace/docs/images/${name}`, `${output}/${name}`);
const metadata = { capturedAt: new Date().toISOString(), description: 'Browser screenshots of recorded local reports; Grafana images are selected copies of original live captures.', reportSources: reports.map(r => r.name), reportCaptures: captured, grafanaCapture: JSON.parse(await readFile('/workspace/docs/images/capture.json', 'utf8')), files: [] };
for (const name of ['benchmark-results.png', 'verification-results.png', 'grafana-overview.png', 'grafana-decisions.png']) metadata.files.push({ file: name, sha256: createHash('sha256').update(await readFile(`${output}/${name}`)).digest('hex') });
await writeFile(`${output}/capture.json`, JSON.stringify(metadata, null, 2) + '\n');
console.log(JSON.stringify({ capturedAt: metadata.capturedAt, reportCaptures: captured, arms: reports.length, completed, failed }));
