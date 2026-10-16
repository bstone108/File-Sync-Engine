const { expect, test } = require('@playwright/test');
const { execFileSync } = require('node:child_process');
const path = require('node:path');

const composeFile = path.resolve(__dirname, 'compose.yaml');

async function pairFrom(page) {
  await page.getByTestId('nav-peers').click();
  await expect(page.getByTestId('pairing-local-url')).toHaveCount(0);
  await expect(page.getByTestId('pairing-reachable-url')).toHaveCount(0);
  await expect(page.getByTestId('pairing-advertise-url')).toContainText('advertises');
  await page.getByTestId('generate-pairing').click();
  const text = page.getByTestId('pairing-export-text');
  await expect(text).toBeVisible();
  const value = await text.inputValue();
  expect(value).toContain('reachableAPIURL');
  expect(value).toContain('https://fse-');
  expect(value).not.toMatch(/172\.(1[7-9]|2\d|3[01])\./);
  expect(value).not.toContain('pairing-local-url');
  const advertised = page.getByTestId('pairing-advertise-override');
  await expect(advertised).toHaveValue(/https:\/\/fse-[ab]:22420/);
  return value;
}

async function importOn(page, packageText, expected) {
  await page.getByTestId('nav-peers').click();
  await expect(page.getByTestId('pairing-local-url')).toHaveCount(0);
  await page.getByTestId('pairing-import-text').fill(packageText);
  await page.getByTestId('parse-pairing').click();
  await page.getByTestId('import-pairing').click();
  if (expected === 'paired') {
    await expect(page.getByText(/identity import accepted|already paired/i)).toBeVisible();
    return;
  }
  await expect(page.getByText(expected, { exact: false })).toBeVisible();
}

async function addSharedFolder(page) {
  await page.getByTestId('nav-daemon-settings').click();
  await page.locator('form[aria-label="Daemon settings folder form"] select[name="folderFormAction"]').selectOption('add');
  await page.getByTestId('folder-id').fill('shared');
  await page.getByTestId('folder-path').fill('/data/shared');
  const submit = page.getByTestId('folder-submit');
  await submit.scrollIntoViewIfNeeded();
  await submit.click();
  const status = page.getByLabel('Folder operation status');
  await expect(status).toContainText('folder add accepted');
  await expect(status).not.toContainText('No folder command has run yet');
}

async function changeLogLevel(page) {
  await page.getByTestId('nav-daemon-settings').click();
  const localEngine = page.getByTestId('instance-option').filter({ hasText: '· local' });
  if (await localEngine.count()) {
    await localEngine.first().click();
  }
  const select = page.locator('form[aria-label="Config patch form"] select[name="configPatchLoggingLevel"]');
  await select.scrollIntoViewIfNeeded();
  await select.selectOption('debug');
  const submit = page.getByRole('button', { name: 'Submit non-secret config patch' });
  await submit.scrollIntoViewIfNeeded();
  await submit.click();
  const status = page.getByLabel('Config operation status');
  const accepted = 'config update accepted; daemon hot reload will adopt the config change';
  await expect(status).toContainText('config patch request completed');
  await expect(status).toContainText(accepted);
  await expect(status).not.toContainText('No config command has run yet');
  await expect(status).not.toContainText('idle:');
  const statusText = await status.innerText();
  const copies = statusText.split(accepted).length - 1;
  if (copies !== 1) {
    throw new Error(`expected the config acceptance once, saw ${copies}: ${statusText}`);
  }
}

async function pageShowsFolderError(page) {
  await page.getByTestId('nav-daemon-settings').click();
  await page.locator('form[aria-label="Daemon settings folder form"] select[name="folderFormAction"]').selectOption('remove');
  await page.getByTestId('folder-id').fill('docs');
  const submit = page.getByTestId('folder-submit');
  await submit.scrollIntoViewIfNeeded();
  await submit.click();
  await page.getByTestId('confirm-destructive').click();
  const status = page.getByLabel('Folder operation status');
  await expect(status).toContainText('not found');
  await expect(status).not.toContainText('No folder command has run yet');
  const trust = page.getByRole('button', { name: 'Fetch API trust status' });
  await trust.scrollIntoViewIfNeeded();
  await trust.click();
  const trustStatus = page.getByLabel('API trust operation status');
  await expect(trustStatus).not.toContainText('404');
  await expect(trustStatus).not.toContainText('has not been checked yet');
  await expect(trustStatus).not.toContainText('idle:');
  const details = page.getByLabel('API certificate trust details');
  await expect(details).toContainText('Certificate fingerprint');
  await expect(details).toContainText('Trusted certificate matches');
  await expect(details).not.toContainText('certificateSha256');
  await expect(details).not.toContainText('trustedCertificateMatches');
  const summary = page.getByTestId('api-trust-summary');
  await expect(summary).toContainText('Certificate fingerprint');
  await expect(summary).not.toContainText('has not been fetched');
}

async function showDefaultPairingCode(page, service) {
  const raw = compose(['exec', '-T', service, 'cat', '/config/config.jsonc'], true);
  const cfg = JSON.parse(raw);
  const peers = cfg.discovery && cfg.discovery.dhtBootstrapPeers;
  expect(peers).toEqual(['/dnsaddr/bootstrap.libp2p.io']);
  expect(cfg.discovery && cfg.discovery.dht).toBe(true);
  expect(raw).not.toMatch(/"dht": false/);
  await page.getByTestId('nav-peers').click();
  const posts = [];
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/pair-code') {
      posts.push(request.url());
    }
  });
  await page.getByTestId('show-pairing-code').click();
  await expect.poll(() => posts.length, { timeout: 15000 }).toBeGreaterThan(0);
  const code = page.getByTestId('pairing-code');
  await expect.poll(async () => {
    if (await code.count() === 1 && await code.isVisible()) {
      return 'shown';
    }
    const message = await page.getByTestId('pairing-message').textContent().catch(() => '');
    return `${await code.count()} code element(s); ${(message || '').trim() || 'pending'}`;
  }, { timeout: 60000 }).toBe('shown');
  await expect(code).toContainText(/[A-Z0-9]{4}(-[A-Z0-9]{4}){3}/);
  await expect(page.getByTestId('pairing-message')).toContainText('is ready');
  await expect(page.getByTestId('pairing-message')).not.toContainText('invalid p2p multiaddr');
  return (await code.innerText()).match(/[A-Z0-9]{4}(?:-[A-Z0-9]{4}){3}/)?.[0];
}

async function redeemPairingCode(page, code) {
  await page.getByTestId('nav-peers').click();
  await page.getByTestId('pairing-code-input').fill(code);
  const posts = [];
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/pair-code/redeem') {
      posts.push(request.url());
    }
  });
  await page.getByTestId('redeem-pairing-code').click();
  await expect.poll(() => posts.length, { timeout: 20000 }).toBeGreaterThan(0);
  await expect(page.getByTestId('pairing-message')).toContainText(/paired by code/i);
  await page.getByTestId('redeem-pairing-code').click();
  await expect.poll(() => posts.length, { timeout: 20000 }).toBeGreaterThan(1);
  await expect(page.getByTestId('pairing-message')).toContainText(/already used|not found|was not found/i, { timeout: 30000 });
  await expect(page.getByTestId('pairing-message')).not.toContainText(/paired by code/i);
}

function compose(args, capture = false) {
  return execFileSync('docker', ['compose', '-f', composeFile, ...args], {
    encoding: 'utf8',
    stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
  });
}

function readShared(service) {
  try {
    return compose(['exec', '-T', service, 'cat', '/data/shared/note.txt'], true).trim();
  } catch (error) {
    const stderr = error && error.stderr ? String(error.stderr).trim() : '';
    return stderr ? `missing: ${stderr}` : 'missing';
  }
}

test('two containers pair in the GUI and sync both ways', async ({ browser }) => {
  const a = await browser.newPage();
  const b = await browser.newPage();
  await expect.poll(async () => {
    const response = await a.goto('http://127.0.0.1:8385/').catch(() => null);
    return response?.ok() ?? false;
  }).toBe(true);
  await expect.poll(async () => {
    const response = await b.goto('http://127.0.0.1:8386/').catch(() => null);
    return response?.ok() ?? false;
  }).toBe(true);
  await expect(a.getByTestId('device-node-name')).toHaveText('fse-a');
  await expect(b.getByTestId('device-node-name')).toHaveText('fse-b');
  await expect(a.locator('aside strong.connection-state').first()).toHaveText('Online');
  await expect(b.locator('aside strong.connection-state').first()).toHaveText('Online');
  const pairingCode = await showDefaultPairingCode(a, 'fse-a');
  expect(pairingCode).toMatch(/[A-Z0-9]{4}(?:-[A-Z0-9]{4}){3}/);
  await redeemPairingCode(b, pairingCode);

  compose(['exec', '-T', '-u', '0', 'fse-a', 'sh', '-c', 'mkdir -p /data/shared && chown -R 99:100 /data && chmod 777 /data /data/shared']);
  compose(['exec', '-T', '-u', '0', 'fse-b', 'sh', '-c', 'mkdir -p /data/shared && chown -R 99:100 /data && chmod 777 /data /data/shared']);

  const exported = await pairFrom(a);
  await importOn(b, exported, 'paired');
  const reverse = await pairFrom(b);
  await importOn(a, reverse, 'already paired');
  await expect(a.getByText('peer-pair key pending')).toHaveCount(0);
  await expect(a.getByText('prepared at level 0')).toHaveCount(0);
  await changeLogLevel(a);
  await addSharedFolder(a);
  await addSharedFolder(b);
  await pageShowsFolderError(a);
  compose(['exec', '-T', '-u', '99:100', 'fse-a', 'sh', '-c', 'echo hello-from-a > /data/shared/note.txt']);

  await expect.poll(() => readShared('fse-b'), { timeout: 45000 }).toBe('hello-from-a');

  compose(['exec', '-T', '-u', '99:100', 'fse-b', 'sh', '-c', 'echo hello-from-b > /data/shared/note.txt']);
  await expect.poll(() => readShared('fse-a'), { timeout: 45000 }).toBe('hello-from-b');

  compose(['exec', '-T', '-u', '99:100', 'fse-a', 'rm', '-f', '/data/shared/note.txt']);
  await expect.poll(() => {
    try {
      compose(['exec', '-T', 'fse-b', 'sh', '-c', 'test ! -f /data/shared/note.txt'], true);
      return 'gone';
    } catch {
      return 'present';
    }
  }, { timeout: 45000 }).toBe('gone');

  await expect(a.locator('aside strong.connection-state').first()).toHaveText('Online');
  await expect(b.locator('aside strong.connection-state').first()).toHaveText('Online');
  await expect(a.getByTestId('local-engine-connection')).toHaveText('Online');
  await expect(b.getByTestId('local-engine-connection')).toHaveText('Online');
  await expect(a.getByTestId('instance-switcher')).toBeVisible();
  await a.getByTestId('nav-overview').click();
  await a.setViewportSize({ width: 1280, height: 800 });
  await a.screenshot({ path: 'screenshots/desktop-overview.png', fullPage: true });
  await a.setViewportSize({ width: 390, height: 844 });
  await a.screenshot({ path: 'screenshots/mobile-overview.png', fullPage: true });
});
