import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const composeFile = resolve(dirname(fileURLToPath(import.meta.url)), 'compose.yaml');

async function pairFrom(page: Page, reachableURL: string) {
  await page.getByTestId('nav-peers').click();
  await page.getByTestId('pairing-reachable-url').fill(reachableURL);
  await page.getByTestId('generate-pairing').click();
  const text = page.getByTestId('pairing-export-text');
  await expect(text).toBeVisible();
  const value = await text.inputValue();
  expect(value).toContain('reachableAPIURL');
  return value;
}

async function importOn(page: Page, localURL: string, packageText: string) {
  await page.getByTestId('nav-peers').click();
  await page.getByTestId('pairing-local-url').fill(localURL);
  await page.getByTestId('pairing-import-text').fill(packageText);
  await page.getByTestId('parse-pairing').click();
  await page.getByTestId('import-pairing').click();
  await expect(page.getByText('identity import accepted', { exact: false })).toBeVisible();
}

async function addSharedFolder(page: Page) {
  await page.getByTestId('nav-daemon-settings').click();
  await page.getByTestId('folder-id').fill('shared');
  await page.getByTestId('folder-path').fill('/data/shared');
  await page.getByTestId('folder-submit').click();
  await expect(page.getByText('folder add accepted', { exact: false })).toBeVisible();
}

function compose(args: string[], capture = false) {
  return execFileSync('docker', ['compose', '-f', composeFile, ...args], {
    encoding: 'utf8',
    stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
  });
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

  const exported = await pairFrom(a, 'https://fse-a:22420');
  await importOn(b, 'https://fse-b:22420', exported);
  await addSharedFolder(a);
  await addSharedFolder(b);

  compose(['exec', '-T', 'fse-a', 'sh', '-c', 'mkdir -p /data/shared && chmod 777 /data /data/shared']);
  compose(['exec', '-T', 'fse-b', 'sh', '-c', 'mkdir -p /data/shared && chmod 777 /data /data/shared']);
  compose(['exec', '-T', 'fse-a', 'sh', '-c', 'echo hello-from-a > /data/shared/note.txt && chmod 666 /data/shared/note.txt']);

  await expect.poll(() => compose(['exec', '-T', 'fse-b', 'cat', '/data/shared/note.txt'], true).trim(), { timeout: 40000 }).toBe('hello-from-a');

  compose(['exec', '-T', 'fse-b', 'sh', '-c', 'echo hello-from-b > /data/shared/note.txt']);
  await expect.poll(() => compose(['exec', '-T', 'fse-a', 'cat', '/data/shared/note.txt'], true).trim(), { timeout: 40000 }).toBe('hello-from-b');

  compose(['exec', '-T', 'fse-a', 'rm', '-f', '/data/shared/note.txt']);
  await expect.poll(() => {
    try {
      compose(['exec', '-T', 'fse-b', 'sh', '-c', 'test ! -f /data/shared/note.txt'], true);
      return 'gone';
    } catch {
      return 'present';
    }
  }, { timeout: 40000 }).toBe('gone');
});
