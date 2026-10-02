import { test, expect, type Page } from '@playwright/test';
import { fileURLToPath } from 'node:url';

const stub = fileURLToPath(new URL('./bridge-stub.js', import.meta.url));

interface Stub {
  calls: string[];
  connectionError: boolean;
  advance(percent: number): void;
  finishCurrent(failure?: string): void;
  finishAll(): void;
  announce(text: string, isError?: boolean): void;
  drop(paths: string[]): void;
}

// Opens the interface with the stand-in backend in the given scenario.
async function desktop(page: Page, scenario = 'empty'): Promise<void> {
  await page.route('**/thumb/*', route => route.fulfill({ status: 404, body: '' }));
  await page.addInitScript(name => { (window as unknown as { __scenario: string }).__scenario = name; }, scenario);
  await page.addInitScript({ path: stub });
  await page.goto('/');
  await expect(page.locator('#window')).toBeVisible();
}

function calls(page: Page): Promise<string[]> {
  return page.evaluate(() => (window as unknown as StubWindow).__stub.calls.slice());
}

type StubWindow = { __stub: Stub };
const advance = (page: Page, percent: number) => page.evaluate(value => (window as unknown as StubWindow).__stub.advance(value), percent);
const finishCurrent = (page: Page) => page.evaluate(() => (window as unknown as StubWindow).__stub.finishCurrent());
const finishAll = (page: Page) => page.evaluate(() => (window as unknown as StubWindow).__stub.finishAll());
const announce = (page: Page, message: string) => page.evaluate(value => (window as unknown as StubWindow).__stub.announce(value, true), message);
const drop = (page: Page, paths: string[]) => page.evaluate(value => (window as unknown as StubWindow).__stub.drop(value), paths);
const connection = (page: Page, broken: boolean) => page.evaluate(value => { (window as unknown as StubWindow).__stub.connectionError = value; }, broken);

const rows = (page: Page) => page.locator('#rows .row');
const row = (page: Page, name: string) => page.locator('#rows .row', { has: page.locator('.row-name', { hasText: name }) });
const primary = (page: Page) => page.locator('#primary');

test('the empty window says what the app does and what it reads and writes', async ({ page }) => {
  await desktop(page);
  await expect(page.getByRole('heading', { name: 'Drop files to convert' })).toBeVisible();
  await expect(page.getByText('nothing is uploaded')).toBeVisible();
  const table = page.getByRole('table', { name: 'Formats Convert Me reads and writes' });
  await expect(table.locator('tbody tr')).toHaveCount(3);
  await expect(table.locator('tbody tr').nth(1)).toContainText('WebM');
  await expect(table.locator('tbody tr').nth(1)).toContainText('or audio only');
  await expect(page.locator('#bar')).toBeHidden();
  await expect(page.locator('#list')).toBeHidden();

  await page.getByRole('button', { name: 'Choose files' }).click();
  await expect(rows(page)).toHaveCount(4);
  await expect(page.locator('#empty')).toBeHidden();
  await expect(page.locator('#bar')).toBeVisible();
  expect(await calls(page)).toContain('choose');
});

test('nothing can be chosen until the engine has been checked', async ({ page }) => {
  await desktop(page, 'starting');
  await expect(page.getByRole('button', { name: 'Choose files' })).toBeDisabled();
});

test('a missing engine is explained and blocks the app', async ({ page }) => {
  await desktop(page, 'setup-error');
  await expect(page.getByRole('alert')).toContainText('The conversion engine is missing or damaged');
  await expect(page.getByRole('button', { name: 'Choose files' })).toBeDisabled();
});

test('dismissing the file dialog leaves the window as it was', async ({ page }) => {
  await desktop(page, 'cancel-choose');
  await page.getByRole('button', { name: 'Choose files' }).click();
  await expect(page.locator('#empty')).toBeVisible();
  await expect(page.locator('#toast')).toBeHidden();
});

test('a ready list shows each file, its route and one clear next step', async ({ page }) => {
  await desktop(page, 'ready');
  await expect(page.locator('#summary-title')).toHaveText('4 files');
  await expect(page.locator('#summary-detail')).toHaveText('2 images · 1 video · 1 audio file');
  await expect(rows(page)).toHaveCount(4);
  const video = row(page, 'interview-final.mov');
  await expect(video.locator('.row-meta')).toHaveText('182 MB · 12:04 · 1080p');
  await expect(video.locator('.tag-source')).toHaveText('MOV');
  await expect(video.locator('.tag-target')).toHaveText('MP4');
  await expect(primary(page)).toHaveText('Convert 4 files');
  await expect(primary(page)).toBeEnabled();
  await expect(page.locator('#open-folder')).toBeHidden();
  await expect(page.getByLabel('Images to')).toHaveValue('jpg');
  await expect(page.getByLabel('Video to')).toHaveValue('mp4');
  await expect(page.getByLabel('Audio to')).toHaveValue('mp3');
  await expect(page.getByLabel('Video to').locator('optgroup[label="Audio only"] option')).toHaveCount(4);
});

test('choosing a format a file already has takes it out of the work', async ({ page }) => {
  await desktop(page, 'ready');
  await page.getByLabel('Images to').selectOption('png');
  expect(await calls(page)).toContain('target:image:png');
  const photo = row(page, 'beach-sunset.png');
  await expect(photo.locator('.row-note-text')).toHaveText('Already PNG, so there is nothing to convert.');
  await expect(photo.locator('.tag-target')).toBeHidden();
  await expect(row(page, 'product-shot.webp').locator('.tag-target')).toHaveText('PNG');
  await expect(primary(page)).toHaveText('Convert 3 files');
  await expect(page.locator('#summary-detail')).toContainText('1 will be skipped');
});

test('a list where nothing needs converting explains itself', async ({ page }) => {
  await desktop(page, 'nothing');
  await expect(page.locator('#summary-title')).toHaveText('Nothing to convert');
  await expect(page.locator('#summary-detail')).toHaveText('Pick another format below to convert these files');
  await expect(primary(page)).toBeDisabled();
  await page.getByLabel('Images to').selectOption('webp');
  await expect(primary(page)).toHaveText('Convert 2 files');
  await expect(primary(page)).toBeEnabled();
});

test('converting shows progress, can be watched to the end, and ends with Open folder', async ({ page }) => {
  await desktop(page, 'ready');
  await primary(page).click();
  expect(await calls(page)).toContain('start:');
  await expect(page.locator('#summary-title')).toHaveText('Converting 1 of 4');
  await expect(page.locator('#summary-detail')).toHaveText('beach-sunset.png');
  await expect(primary(page)).toHaveText('Cancel');
  await expect(page.getByRole('button', { name: 'Add files' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Clear' })).toBeDisabled();
  await expect(page.getByLabel('Images to')).toBeDisabled();
  await expect(page.getByLabel('Save to')).toBeDisabled();
  await expect(page.locator('#overall')).toBeVisible();
  // Files cannot be removed in the middle of a run.
  await expect(row(page, 'voice-memo.m4a').locator('.row-action')).toBeHidden();

  await advance(page, 42);
  const first = row(page, 'beach-sunset.png');
  await expect(first.locator('.rail-percent')).toHaveText('42%');
  await expect(first.locator('progress.track')).toHaveJSProperty('value', 42);
  await expect(page.locator('#overall')).toHaveJSProperty('value', 10.5);

  await finishCurrent(page);
  await expect(first.locator('.row-note-text')).toContainText('Saved as beach-sunset.jpg');
  await expect(page.locator('#summary-title')).toHaveText('Converting 2 of 4');

  await finishAll(page);
  await expect(page.locator('#summary-title')).toHaveText('4 files converted');
  await expect(page.locator('#summary-detail')).toHaveText('Saved next to the originals');
  await expect(primary(page)).toHaveText('Open folder');
  await expect(page.locator('#overall')).toBeHidden();
  await primary(page).click();
  await first.getByRole('button', { name: 'Show beach-sunset.jpg in its folder' }).click();
  const made = await calls(page);
  expect(made).toContain('open-folder');
  expect(made.some(call => call.startsWith('reveal:'))).toBe(true);
});

test('cancel stops the run and offers to try again', async ({ page }) => {
  await desktop(page, 'ready');
  await primary(page).click();
  await expect(primary(page)).toHaveText('Cancel');
  await primary(page).click();
  expect(await calls(page)).toContain('cancel');
  await expect(page.locator('#summary-title')).toHaveText('0 of 4 files converted');
  await expect(row(page, 'beach-sunset.png').locator('.row-note-text')).toHaveText('Cancelled. The original was not changed.');
  await expect(primary(page)).toHaveText('Try 4 again');
  await expect(page.getByRole('button', { name: 'Clear' })).toBeEnabled();
});

test('existing files are never replaced without a clear choice', async ({ page }) => {
  await desktop(page, 'conflict');
  await primary(page).click();
  const dialog = page.getByRole('dialog', { name: '2 files already exist' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('beach-sunset.jpg');
  await expect(dialog).toContainText('Your originals are never replaced');
  await expect(dialog.getByRole('button', { name: 'Keep both' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  await expect(primary(page)).toHaveText('Convert 4 files');
  expect((await calls(page)).filter(call => call.startsWith('start:'))).toEqual(['start:']);

  await primary(page).click();
  await dialog.getByRole('button', { name: 'Keep both' }).click();
  await expect(primary(page)).toHaveText('Cancel');
  expect(await calls(page)).toContain('start:keep');
});

test('replace is only sent when the person asks for it', async ({ page }) => {
  await desktop(page, 'conflict');
  await primary(page).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Replace' }).click();
  await expect(primary(page)).toHaveText('Cancel');
  expect(await calls(page)).toContain('start:replace');
});

test('problems are explained on the row, with details on request', async ({ page }) => {
  await desktop(page, 'problems');
  await expect(page.locator('#summary-title')).toHaveText('3 of 5 files converted');
  await expect(page.locator('#summary-detail')).toHaveText('2 failed · Saved next to the originals');

  const broken = row(page, 'product-shot.webp');
  await expect(broken.locator('.row-note-text')).toHaveText('This file looks damaged or incomplete.');
  await expect(broken.locator('.row-details')).toBeHidden();
  await broken.getByRole('button', { name: 'Show details' }).click();
  await expect(broken.locator('.row-details')).toContainText('image data not found');
  await broken.getByRole('button', { name: 'Hide details' }).click();
  await expect(broken.locator('.row-details')).toBeHidden();

  const unsupported = row(page, 'drone-footage.mp4');
  await expect(unsupported.locator('.row-note-text')).toHaveText('This file uses AV1, which Convert Me cannot read yet.');
  await expect(unsupported.locator('.tag-target')).toBeHidden();
  await expect(row(page, 'interview-final.mov').locator('.row-warning')).toContainText('HDR');
  await expect(row(page, 'logo.jpg').locator('.row-note-text')).toContainText('Already JPG');

  await expect(primary(page)).toHaveText('Try 2 again');
  await expect(page.getByRole('button', { name: 'Open folder' })).toBeVisible();
  // A long list scrolls inside its frame. The window itself never scrolls.
  expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true);
  await row(page, 'logo.jpg').scrollIntoViewIfNeeded();
  await expect(row(page, 'logo.jpg')).toBeInViewport();
});

test('a file can be removed, and the list can be cleared', async ({ page }) => {
  await desktop(page, 'ready');
  await page.getByRole('button', { name: 'Remove voice-memo.m4a from the list' }).click();
  await expect(rows(page)).toHaveCount(3);
  await expect(page.getByLabel('Audio to')).toHaveCount(0);
  await expect(primary(page)).toHaveText('Convert 3 files');
  await page.getByRole('button', { name: 'Clear' }).click();
  await expect(page.locator('#empty')).toBeVisible();
  const made = await calls(page);
  expect(made.some(call => call.startsWith('remove:'))).toBe(true);
  expect(made).toContain('clear');
});

test('formats that need a missing Windows encoder are shown but cannot be picked', async ({ page }) => {
  await desktop(page, 'no-h264');
  const picker = page.getByLabel('Video to');
  await expect(picker).toHaveValue('gif');
  const mp4 = picker.locator('option[value="mp4"]');
  await expect(mp4).toHaveJSProperty('disabled', true);
  await expect(mp4).toHaveText('MP4 (not available)');
  await expect(mp4).toHaveAttribute('title', /H\.264 encoder/);
  await expect(picker.locator('option[value="mp3"]')).toHaveJSProperty('disabled', false);
});

test('the destination can be a chosen folder or next to each original', async ({ page }) => {
  await desktop(page, 'ready');
  const destination = page.getByLabel('Save to');
  await expect(destination).toHaveValue('source');
  await destination.selectOption('choose');
  await expect(destination).toHaveValue('folder');
  await expect(destination.locator('option[value="folder"]')).toHaveText('Exports (D:\\Media\\Exports)');
  await expect(destination).toHaveAttribute('title', 'D:\\Media\\Exports');
  await destination.selectOption('source');
  await expect(destination).toHaveValue('source');
  // The folder stays on offer after switching away from it.
  await destination.selectOption('folder');
  await expect(destination).toHaveValue('folder');
  expect(await calls(page)).toEqual(expect.arrayContaining(['choose-destination', 'use-source', 'use-folder']));
});

test('dismissing the folder dialog keeps the previous destination', async ({ page }) => {
  await desktop(page, 'cancel-choose');
  await drop(page, ['C:\\Media\\photo.png']);
  await expect(rows(page)).toHaveCount(1);
  const destination = page.getByLabel('Save to');
  await destination.selectOption('choose');
  await expect(destination).toHaveValue('source');
});

test('dropped files are added, and files that cannot be used are named', async ({ page }) => {
  await desktop(page);
  await drop(page, ['C:\\Media\\photo.png', 'C:\\Media\\notes.txt']);
  await expect(rows(page)).toHaveCount(1);
  await expect(page.locator('#toast')).toHaveText('Skipped notes.txt: not an image, audio or video type that Convert Me reads.');
  expect(await calls(page)).toContain('add:C:\\Media\\photo.png|C:\\Media\\notes.txt');

  await primary(page).click();
  await expect(primary(page)).toHaveText('Cancel');
  await drop(page, ['C:\\Media\\late.png']);
  await expect(page.locator('#toast')).toHaveText('Wait for the current conversion to finish before adding files.');
  await expect(rows(page)).toHaveCount(1);
});

test('dragging files over the window shows where they will land', async ({ page }) => {
  await desktop(page);
  const hint = page.locator('.drop-hint');
  await expect(hint).toBeHidden();
  const drag = (kind: 'file' | 'text') => page.evaluate(what => {
    const data = new DataTransfer();
    if (what === 'file') data.items.add(new File(['x'], 'photo.png', { type: 'image/png' }));
    else data.setData('text/plain', 'just some text');
    const event = new DragEvent('dragover', { dataTransfer: data, bubbles: true, cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  }, kind);

  // Dragged text is none of the app's business.
  expect(await drag('text')).toBe(false);
  await expect(hint).toBeHidden();
  // Files show the hint, and the window is kept from opening the file itself.
  expect(await drag('file')).toBe(true);
  await expect(hint).toBeVisible();
  await expect(hint).toHaveText('Drop to add files');
  // It goes away by itself when the drag stops or leaves the window.
  await expect(hint).toBeHidden();
});

test('files dropped while a question is open are ignored', async ({ page }) => {
  await desktop(page);
  await page.getByRole('button', { name: 'About Convert Me' }).click();
  await drop(page, ['C:\\Media\\photo.png']);
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();
  await expect(page.locator('#empty')).toBeVisible();
  expect((await calls(page)).some(call => call.startsWith('add:'))).toBe(false);
});

test('messages about files handed over from outside the window are shown', async ({ page }) => {
  await desktop(page, 'ready');
  await announce(page, 'Skipped notes.txt: not an image, audio or video type that Convert Me reads.');
  await expect(page.getByRole('alert').filter({ hasText: 'Skipped notes.txt' })).toBeVisible();
});

test('file names are shown as text, never run as markup', async ({ page }) => {
  await desktop(page);
  await drop(page, ['C:\\Media\\<img src=x onerror="window.attacked=1">.png']);
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).locator('.row-name')).toHaveText('<img src=x onerror="window.attacked=1">.png');
  expect(await page.evaluate(() => (window as unknown as { attacked?: number }).attacked)).toBeUndefined();
  await expect(rows(page).locator('.row-name img')).toHaveCount(0);
});

test('the theme can be switched and is remembered', async ({ page }) => {
  await desktop(page, 'ready');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.getByRole('button', { name: 'Switch to dark theme' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('button', { name: 'Switch to light theme' })).toBeVisible();
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});

test('about names the engine and opens the licenses', async ({ page }) => {
  await desktop(page, 'ready');
  await page.getByRole('button', { name: 'About Convert Me' }).click();
  const dialog = page.getByRole('dialog', { name: /Convert Me/ });
  await expect(dialog).toContainText('0.1.0');
  await expect(dialog).toContainText('FFmpeg 8.1.3');
  await expect(dialog).toContainText('No uploads, no account');
  await dialog.getByRole('button', { name: 'Open licenses folder' }).click();
  await dialog.getByRole('button', { name: 'Close' }).click();
  await expect(dialog).toBeHidden();
  expect(await calls(page)).toContain('licenses');
});

test('the window buttons and the open shortcut reach the app', async ({ page }) => {
  await desktop(page);
  await page.getByRole('button', { name: 'Minimise' }).click();
  await page.getByRole('button', { name: 'Maximise' }).click();
  await page.keyboard.press('Control+o');
  await expect(rows(page)).toHaveCount(4);
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  expect(await calls(page)).toEqual(expect.arrayContaining(['minimise', 'maximise', 'choose', 'close']));
});

test('a lost connection to the app is shown and recovers by itself', async ({ page }) => {
  await desktop(page, 'ready');
  await connection(page, true);
  await expect(page.getByRole('alert')).toHaveText('The app stopped answering. It keeps trying to reconnect.');
  await expect(primary(page)).toBeDisabled();
  await connection(page, false);
  await expect(page.locator('#setup-error')).toBeHidden();
  await expect(primary(page)).toBeEnabled();
});

test('without the desktop app the page says so instead of pretending to work', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Desktop app required' })).toBeVisible();
  await expect(page.locator('#window')).toBeHidden();
});

test('the page only ever talks to itself', async ({ page }) => {
  const foreign: string[] = [];
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.origin !== 'http://127.0.0.1:5183' && url.protocol !== 'data:') foreign.push(request.url());
  });
  await desktop(page, 'problems');
  await page.getByRole('button', { name: 'About Convert Me' }).click();
  await page.waitForTimeout(600);
  expect(foreign).toEqual([]);
});

for (const scenario of ['empty', 'ready', 'converting', 'done', 'problems', 'mixed']) {
  test(`at the smallest window size the ${scenario} state fits without sideways scrolling`, async ({ page }) => {
    await page.setViewportSize({ width: 480, height: 520 });
    await desktop(page, scenario);
    const overflow = await page.evaluate(() => ({
      page: document.documentElement.scrollWidth - window.innerWidth,
      tall: document.documentElement.scrollHeight - window.innerHeight,
    }));
    expect(overflow.page).toBeLessThanOrEqual(0);
    expect(overflow.tall).toBeLessThanOrEqual(0);
    // Every control a person needs is inside the window, not pushed off its edge.
    const controls = scenario === 'empty' ? ['#choose-files', '#theme', '#win-close'] : ['#primary', '#add-files', '#theme', '#win-close'];
    for (const selector of controls) {
      const box = await page.locator(selector).boundingBox();
      expect(box, selector).not.toBeNull();
      expect(box!.x, selector).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width, selector).toBeLessThanOrEqual(480);
      expect(box!.y + box!.height, selector).toBeLessThanOrEqual(520);
    }
    if (scenario !== 'empty') {
      const names = await page.locator('#rows .row-name').evaluateAll(nodes => nodes.map(node => {
        const box = node.getBoundingClientRect();
        return box.width;
      }));
      // File names keep enough room to be recognised.
      for (const width of names) expect(width).toBeGreaterThan(120);
    }
  });
}

// Measures the contrast between text and what is behind it, the way a person's eyes do.
async function contrastOf(page: Page, selectors: string[]): Promise<Record<string, number>> {
  return page.evaluate(list => {
    const parse = (value: string) => (value.match(/[\d.]+/g) ?? []).slice(0, 3).map(Number);
    const luminance = ([r, g, b]: number[]) => {
      const channel = (v: number) => { const s = v / 255; return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4; };
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    };
    const background = (element: Element | null): number[] => {
      for (let node = element; node; node = node.parentElement) {
        const color = getComputedStyle(node).backgroundColor;
        if (color && !/rgba\(0, 0, 0, 0\)|transparent/.test(color)) return parse(color);
      }
      return [255, 255, 255];
    };
    const ratios: Record<string, number> = {};
    for (const selector of list) {
      const element = [...document.querySelectorAll(selector)].find(node => (node as HTMLElement).offsetParent !== null);
      if (!element) { ratios[selector] = -1; continue; }
      const a = luminance(parse(getComputedStyle(element).color));
      const b = luminance(background(element));
      ratios[selector] = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
    }
    return ratios;
  }, selectors);
}

const readable: Record<string, string[]> = {
  ready: ['.row-name', '.row-meta', '#summary-title', '#summary-detail', '.field span', '.field select', '#primary-label', '.tag-source', '.tag-target', '#add-files', '#clear'],
  problems: ['.row-note[data-tone="bad"] .row-note-text', '.row-note[data-tone="ok"] .row-note-text', '.row-note[data-tone="muted"] .row-note-text', '.row-warning', '.row-toggle', '#open-folder'],
  empty: ['#empty-title', '.lede', '.formats tbody th', '.formats-reads', '.formats-writes', '.formats-extra', '#choose-files span'],
};

for (const theme of ['light', 'dark'] as const) {
  for (const [scenario, selectors] of Object.entries(readable)) {
    test(`text is readable in the ${theme} theme (${scenario})`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await desktop(page, scenario);
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      const contrast = await contrastOf(page, selectors);
      // 4.5 to 1 is the accessibility guideline for normal text.
      for (const [selector, ratio] of Object.entries(contrast)) expect(ratio, `${selector} in ${theme}`).toBeGreaterThanOrEqual(4.5);
    });
  }
}