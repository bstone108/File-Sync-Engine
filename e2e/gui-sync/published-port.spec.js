const { expect, test } = require('@playwright/test');
const { execFileSync } = require('node:child_process');
const path = require('node:path');

const composeFile = path.resolve(__dirname, 'compose.published.yaml');

test.skip(!process.env.FSE_PUBLISHED_PORT_E2E, 'published-port pairing runs as its own compose project');

function compose(args, capture = false) {
  return execFileSync('docker', ['compose', '-f', composeFile, ...args], {
    encoding: 'utf8',
    stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
  });
}

function containerIP(service) {
  const id = compose(['ps', '-q', service], true).trim().split('\n')[0];
  return execFileSync('docker', ['inspect', '-f', '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}', id], {
    encoding: 'utf8',
  }).trim();
}

function engineLog(service) {
  try {
    return compose(['exec', '-T', service, 'cat', '/config/logs/fse.jsonl'], true);
  } catch (error) {
    return error && error.stdout ? String(error.stdout) : '';
  }
}

function probe(service, host, port) {
  try {
    compose(['exec', '-T', service, 'sh', '-c', `nc -w 2 ${host} ${port} </dev/null`], true);
    return 'open';
  } catch (error) {
    const stderr = error && error.stderr ? String(error.stderr).trim() : '';
    const stdout = error && error.stdout ? String(error.stdout).trim() : '';
    return `closed ${stdout} ${stderr}`.trim();
  }
}

async function clickClearOfSidebar(locator) {
  await locator.evaluate((el) => el.scrollIntoView({ block: 'center', inline: 'center' }));
  await locator.click();
}

async function closeOptions(page) {
  const close = page.getByTestId('node-options-close');
  if (await close.isVisible().catch(() => false)) {
    await close.click();
  }
}

async function showCode(page) {
  await closeOptions(page);
  const menu = page.getByTestId('add-peer-menu');
  if ((await menu.getAttribute('aria-expanded')) !== 'true') {
    await clickClearOfSidebar(menu);
  }
  await clickClearOfSidebar(page.getByTestId('show-pairing-code'));
  const code = page.getByTestId('pairing-code');
  await expect(code).toBeVisible({ timeout: 60000 });
  await expect(page.getByTestId('pairing-message')).toContainText('is ready');
  return (await code.innerText()).match(/[A-Z0-9]{4}(?:-[A-Z0-9]{4}){3}/)?.[0];
}

async function openLinkForm(page) {
  await closeOptions(page);
  const input = page.getByTestId('pairing-code-input');
  if (!(await input.isVisible().catch(() => false))) {
    const menu = page.getByTestId('add-peer-menu');
    if ((await menu.getAttribute('aria-expanded')) !== 'true') {
      await clickClearOfSidebar(menu);
    }
    await page.getByTestId('link-peer-form').click();
  }
  await expect(input).toBeVisible();
}

async function expectPairingRole(page, role) {
  const dialog = page.getByTestId('node-options-dialog');
  if (!(await dialog.isVisible().catch(() => false))) {
    await page.getByTestId('node-options').click();
  }
  await page.getByTestId('nav-peers').click();
  await expect(page.getByTestId('pairing-message')).toHaveAttribute('role', role);
}

test('pairing works when only published ports connect the containers', async ({ browser }) => {
  test.setTimeout(240000);
  const direct = containerIP('published-a');
  expect(direct).toMatch(/^\d+\.\d+\.\d+\.\d+$/);
  expect(probe('published-b', direct, 22421)).not.toBe('open');
  expect(probe('published-b', direct, 22427)).not.toBe('open');
  expect(probe('published-b', 'published-a', 22421)).toBe('open');
  expect(probe('published-a', 'published-b', 22423)).toBe('open');
  // Hairpin to this container's own published name is open. That dial is not
  // public reachability and must not promote the node.
  expect(probe('published-a', 'published-a', 22421)).toBe('open');

  const logs = compose(['logs', 'published-a'], true);
  expect(logs).not.toContain('reachable API URL not configured');
  expect(logs).toContain('reachable API URL https://published-a:22420');
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('"listenPort":22421');
  // A published port is not DHT server mode. The node stays auto and private
  // until AutoNAT v2 or an authenticated peer dials it back.
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('"dhtModeReason":"auto"');
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('"event":"libp2p.dht.private"');
  const reachability = engineLog('published-a').split('\n').filter((line) => line.includes('libp2p.dht.reachable') || line.includes('libp2p.dht.dialback'));
  for (const line of reachability) {
    expect(line).toMatch(/"source":"(autonat-v2|authenticated-peer)"/);
  }
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('libp2p is advertising the published port');
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('/dns4/published-a/tcp/22421');
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU');

  const a = await browser.newPage();
  const b = await browser.newPage();
  await expect.poll(async () => (await a.goto('http://127.0.0.1:8387/').catch(() => null))?.ok() ?? false).toBe(true);
  await expect.poll(async () => (await b.goto('http://127.0.0.1:8388/').catch(() => null))?.ok() ?? false).toBe(true);
  await expect(a.getByTestId('device-node-name')).toHaveText('published-a');
  await expect(b.getByTestId('device-node-name')).toHaveText('published-b');

  let paired = '';
  let code = '';
  for (let attempt = 0; attempt < 2 && !/paired by code/i.test(paired); attempt += 1) {
    code = await showCode(a);
    expect(code).toMatch(/[A-Z0-9]{4}(?:-[A-Z0-9]{4}){3}/);
    await expect.poll(() => {
      const raw = engineLog('published-a');
      // A published port the node dials itself is not public reachability.
      // Two clients that can dial each other serve the pairing record on that
      // connection until a remote peer confirms the port.
      if (raw.includes('pairing code advertised on the DHT') || raw.includes('pairing code is served to a connected peer')) {
        return 'ok';
      }
      const lines = raw.split('\n').filter((line) => line.includes('libp2p.bootstrap.dial') || line.includes('pairing.publish.failed'));
      return lines.slice(-3).join('\n');
    }, { timeout: 50000 }).toBe('ok');
    await openLinkForm(b);
    await b.getByTestId('pairing-code-input').fill(code);
    await clickClearOfSidebar(b.getByTestId('redeem-pairing-code'));
    await expect.poll(async () => {
      const text = (await b.getByTestId('pairing-message').innerText()).trim();
      return /^linking with/i.test(text) ? '' : text;
    }, { timeout: 90000 }).not.toBe('');
    paired = (await b.getByTestId('pairing-message').innerText()).trim();
    if (/failed to open stream|context deadline/i.test(paired)) {
      throw new Error(`published-port dial failed: ${paired}`);
    }
  }
  if (!/paired by code/i.test(paired)) {
    throw new Error(`${paired}\n--- published-a ---\n${engineLog('published-a').slice(-2500)}\n--- published-b ---\n${engineLog('published-b').slice(-2500)}`);
  }
  expect(paired).toMatch(/paired by code/i);
  await expectPairingRole(b, 'status');

  await openLinkForm(b);
  await b.getByTestId('pairing-code-input').fill(code);
  await clickClearOfSidebar(b.getByTestId('redeem-pairing-code'));
  await expect(b.getByTestId('pairing-message')).toContainText(/already used|not found|was not found/i, { timeout: 30000 });
  await expectPairingRole(b, 'alert');
  await expect(b.getByTestId('pairing-message')).not.toContainText(/paired by code/i);
});
