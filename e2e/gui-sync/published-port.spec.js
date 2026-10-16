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

async function showCode(page) {
  await page.getByTestId('nav-peers').click();
  await page.getByTestId('show-pairing-code').click();
  const code = page.getByTestId('pairing-code');
  await expect(code).toBeVisible({ timeout: 60000 });
  await expect(page.getByTestId('pairing-message')).toContainText('is ready');
  return (await code.innerText()).match(/[A-Z0-9]{4}(?:-[A-Z0-9]{4}){3}/)?.[0];
}

test('pairing works when only published ports connect the containers', async ({ browser }) => {
  test.setTimeout(240000);
  const direct = containerIP('published-a');
  expect(direct).toMatch(/^\d+\.\d+\.\d+\.\d+$/);
  expect(probe('published-b', direct, 22421)).not.toBe('open');
  expect(probe('published-b', direct, 22427)).not.toBe('open');
  expect(probe('published-b', 'published-a', 22421)).toBe('open');
  expect(probe('published-a', 'published-b', 22423)).toBe('open');

  const logs = compose(['logs', 'published-a'], true);
  expect(logs).not.toContain('reachable API URL not configured');
  expect(logs).toContain('reachable API URL https://published-a:22420');
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('"listenPort":22421');
  await expect.poll(() => engineLog('published-a'), { timeout: 20000 }).toContain('12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU');

  const a = await browser.newPage();
  const b = await browser.newPage();
  await expect.poll(async () => (await a.goto('http://127.0.0.1:8387/').catch(() => null))?.ok() ?? false).toBe(true);
  await expect.poll(async () => (await b.goto('http://127.0.0.1:8388/').catch(() => null))?.ok() ?? false).toBe(true);
  await expect(a.getByTestId('device-node-name')).toHaveText('published-a');
  await expect(b.getByTestId('device-node-name')).toHaveText('published-b');

  let paired = '';
  for (let attempt = 0; attempt < 2 && !/paired by code/i.test(paired); attempt += 1) {
    const code = await showCode(a);
    expect(code).toMatch(/[A-Z0-9]{4}(?:-[A-Z0-9]{4}){3}/);
    await expect.poll(() => {
      const raw = engineLog('published-a');
      if (raw.includes('pairing code advertised on the DHT')) {
        return 'ok';
      }
      const failed = raw.split('\n').filter((line) => line.includes('pairing.publish.failed')).pop();
      return failed || '';
    }, { timeout: 50000 }).toBe('ok');
    await b.getByTestId('nav-peers').click();
    await b.getByTestId('pairing-code-input').fill(code);
    await b.getByTestId('redeem-pairing-code').click();
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
  await expect(b.getByTestId('pairing-message')).toHaveAttribute('role', 'status');

  await b.getByTestId('redeem-pairing-code').click();
  await expect(b.getByTestId('pairing-message')).toContainText(/already used|not found|was not found/i, { timeout: 30000 });
  await expect(b.getByTestId('pairing-message')).toHaveAttribute('role', 'alert');
  await expect(b.getByTestId('pairing-message')).not.toContainText(/paired by code/i);
});
