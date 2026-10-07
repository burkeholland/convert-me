import assert from 'node:assert/strict';
import test from 'node:test';
import {
  asSentence, clock, errorText, fileSize, folderLabel, folderName, isBusy, isConvertible, itemMeta,
  kindWord, overallProgress, plural, primaryAction, resolution, sizeChange, summarize,
} from '../src/format.ts';
import type { Batch, Item, Snapshot } from '../src/types.ts';

function item(overrides: Partial<Item> = {}): Item {
  return {
    id: 'a', path: 'C:\\Media\\photo.png', name: 'photo.png', folder: 'C:\\Media', size: 1024 * 1024,
    kind: 'image', status: 'ready', source: 'PNG', width: 4032, height: 3024, durationMs: 0,
    target: 'jpg', targetLabel: 'JPG', progress: 0, outputPath: '', outputName: '', outputSize: 0,
    error: '', detail: '', warning: '', thumb: true, ...overrides,
  };
}

function batch(overrides: Partial<Batch> = {}): Batch {
  return { state: 'idle', total: 0, done: 0, failed: 0, skipped: 0, cancelled: 0, currentId: '', ...overrides };
}

function snapshot(items: Item[], overrides: Partial<Snapshot> = {}): Snapshot {
  const counts = new Map<string, number>();
  for (const entry of items) {
    if (entry.kind && entry.status !== 'unsupported') counts.set(entry.kind, (counts.get(entry.kind) ?? 0) + 1);
  }
  return {
    version: '0.2.0', engine: 'FFmpeg 8.1.3', state: 'ready', setupError: '', items,
    kinds: (['image', 'video', 'audio'] as const).filter(kind => counts.has(kind)).map(kind => ({
      kind, label: kind, count: counts.get(kind)!, target: '', options: [],
    })),
    destination: { mode: 'source', folder: '' }, batch: batch(), formats: [],
    revision: 1, notice: '', noticeError: false, noticeSeq: 0, ...overrides,
  };
}

test('file sizes read the way people say them', () => {
  assert.equal(fileSize(0), '0 B');
  assert.equal(fileSize(1023), '1023 B');
  assert.equal(fileSize(1024), '1 KB');
  assert.equal(fileSize(1536), '1.5 KB');
  assert.equal(fileSize(412 * 1024), '412 KB');
  assert.equal(fileSize(2.4 * 1024 ** 2), '2.4 MB');
  assert.equal(fileSize(182 * 1024 ** 2), '182 MB');
  assert.equal(fileSize(1024 ** 3), '1 GB');
  assert.equal(fileSize(1.25 * 1024 ** 3), '1.25 GB');
  assert.equal(fileSize(2.5 * 1024 ** 3), '2.5 GB');
  assert.equal(fileSize(-5), '0 B');
  assert.equal(fileSize(Number.NaN), '0 B');
});

test('durations only show hours when there are any', () => {
  assert.equal(clock(0), '0:00');
  assert.equal(clock(59_400), '0:59');
  assert.equal(clock(59_600), '1:00');
  assert.equal(clock(724_000), '12:04');
  assert.equal(clock(3_600_000), '1:00:00');
  assert.equal(clock(5_025_000), '1:23:45');
  assert.equal(clock(-1), '0:00');
  assert.equal(clock(Number.NaN), '0:00');
});

test('resolution is named by the short side', () => {
  assert.equal(resolution(1920, 1080), '1080p');
  assert.equal(resolution(1080, 1920), '1080p');
  assert.equal(resolution(3840, 2160), '4K');
  assert.equal(resolution(1280, 720), '720p');
  assert.equal(resolution(0, 0), '');
});

test('item details depend on the kind of file', () => {
  assert.equal(itemMeta(item()), '1 MB · 4032 x 3024');
  assert.equal(itemMeta(item({ kind: 'video', width: 1920, height: 1080, durationMs: 724_000, size: 182 * 1024 ** 2 })), '182 MB · 12:04 · 1080p');
  assert.equal(itemMeta(item({ kind: 'audio', width: 0, height: 0, durationMs: 167_000 })), '1 MB · 2:47');
  assert.equal(itemMeta(item({ kind: '', width: 0, height: 0 })), '1 MB');
});

test('size change is described in plain words', () => {
  assert.equal(sizeChange(1000, 310), '69% smaller');
  assert.equal(sizeChange(1000, 1000), 'about the same size');
  assert.equal(sizeChange(1000, 1040), 'about the same size');
  assert.equal(sizeChange(1000, 1240), '24% larger');
  assert.equal(sizeChange(1000, 1900), '90% larger');
  assert.equal(sizeChange(1000, 2500), '2.5x larger');
  assert.equal(sizeChange(0, 500), '');
  assert.equal(sizeChange(500, 0), '');
});

test('words agree with counts', () => {
  assert.equal(plural(1, 'file'), '1 file');
  assert.equal(plural(3, 'file'), '3 files');
  assert.equal(kindWord('image', 1), 'image');
  assert.equal(kindWord('video', 2), 'videos');
  assert.equal(kindWord('audio', 1), 'audio file');
  assert.equal(kindWord('audio', 4), 'audio files');
});

test('folders are shown by their last part with the full path for certainty', () => {
  assert.equal(folderName('D:\\Media\\Exports'), 'Exports');
  assert.equal(folderName('D:\\Media\\Exports\\'), 'Exports');
  assert.equal(folderName('D:\\'), 'D:\\');
  assert.equal(folderLabel('D:\\Media\\Exports'), 'Exports (D:\\Media\\Exports)');
  assert.equal(folderLabel('D:\\'), 'D:\\');
});

test('the summary line always describes the state of the list', () => {
  const ready = snapshot([item(), item({ id: 'b', kind: 'video' }), item({ id: 'c', kind: 'audio' }), item({ id: 'd', kind: 'audio' })]);
  assert.deepEqual(summarize(ready), { title: '4 files', detail: '1 image · 1 video · 2 audio files' });

  const checking = snapshot([item(), item({ id: 'b', status: 'checking', kind: '' }), item({ id: 'c', status: 'unsupported', kind: '' })]);
  assert.deepEqual(summarize(checking), { title: '3 files', detail: '1 image · checking 1 · 1 cannot be converted' });

  const mixed = snapshot([item(), item({ id: 'b', status: 'skipped', source: 'JPG' })]);
  assert.deepEqual(summarize(mixed), { title: '2 files', detail: '2 images · 1 will be skipped' });

  const running = snapshot([item({ status: 'done' }), item({ id: 'b', name: 'clip.mov', status: 'converting' }), item({ id: 'c', status: 'queued' })],
    { batch: batch({ state: 'running', total: 3, done: 1, currentId: 'b' }) });
  assert.deepEqual(summarize(running), { title: 'Converting 2 of 3', detail: 'clip.mov' });

  const cancelling = snapshot([item({ status: 'converting' })], { batch: batch({ state: 'cancelling', total: 1, currentId: 'a' }) });
  assert.equal(summarize(cancelling).title, 'Cancelling');

  const finished = snapshot([item({ status: 'done' }), item({ id: 'b', status: 'done' })], { batch: batch({ state: 'finished', total: 2, done: 2 }) });
  assert.deepEqual(summarize(finished), { title: '2 files converted', detail: 'Saved next to the originals' });

  const toFolder = snapshot([item({ status: 'done' })], {
    batch: batch({ state: 'finished', total: 1, done: 1 }), destination: { mode: 'folder', folder: 'D:\\Media\\Exports' },
  });
  assert.deepEqual(summarize(toFolder), { title: '1 file converted', detail: 'Saved to Exports' });

  const partial = snapshot([item({ status: 'done' }), item({ id: 'b', status: 'failed' }), item({ id: 'c', status: 'cancelled' })],
    { batch: batch({ state: 'finished', total: 3, done: 1, failed: 1, cancelled: 1 }) });
  assert.deepEqual(summarize(partial), { title: '1 of 3 files converted', detail: '1 failed · 1 cancelled · Saved next to the originals' });

  const nothingSaved = snapshot([item({ status: 'cancelled' })], { batch: batch({ state: 'finished', total: 1, cancelled: 1 }) });
  assert.deepEqual(summarize(nothingSaved), { title: '0 of 1 file converted', detail: '1 cancelled' });
});

test('a list where nothing needs converting says what to do next', () => {
  const nothing = snapshot([item({ status: 'skipped', source: 'JPG' }), item({ id: 'b', status: 'skipped', source: 'JPG' })]);
  assert.deepEqual(summarize(nothing), { title: 'Nothing to convert', detail: 'Pick another format below to convert these files' });
  assert.deepEqual(primaryAction(nothing), { kind: 'none', label: 'Convert', enabled: false });

  // While files are still being checked it is too early to say that.
  const early = snapshot([item({ status: 'skipped' }), item({ id: 'b', status: 'checking', kind: '' })]);
  assert.equal(summarize(early).title, '2 files');
  // Finished results stay the headline even when the rest was skipped.
  const afterwards = snapshot([item({ status: 'skipped' }), item({ id: 'b', status: 'done' })]);
  assert.equal(summarize(afterwards).title, '2 files');
});

test('the main button is always the next sensible step', () => {
  assert.deepEqual(primaryAction(snapshot([])), { kind: 'none', label: 'Convert', enabled: false });
  assert.deepEqual(primaryAction(snapshot([item()])), { kind: 'convert', label: 'Convert 1 file', enabled: true });
  assert.deepEqual(primaryAction(snapshot([item(), item({ id: 'b' }), item({ id: 'c', status: 'unsupported' })])),
    { kind: 'convert', label: 'Convert 2 files', enabled: true });
  // Files that will be skipped are not counted as work.
  assert.deepEqual(primaryAction(snapshot([item(), item({ id: 'b', status: 'skipped' })])),
    { kind: 'convert', label: 'Convert 1 file', enabled: true });
  // Converting waits until every file has been checked.
  assert.equal(primaryAction(snapshot([item(), item({ id: 'b', status: 'checking' })])).enabled, false);
  assert.equal(primaryAction(snapshot([item()], { state: 'starting' })).enabled, false);

  assert.deepEqual(primaryAction(snapshot([item({ status: 'converting' })], { batch: batch({ state: 'running', total: 1 }) })),
    { kind: 'cancel', label: 'Cancel', enabled: true });
  assert.deepEqual(primaryAction(snapshot([item({ status: 'converting' })], { batch: batch({ state: 'cancelling', total: 1 }) })),
    { kind: 'cancel', label: 'Cancelling', enabled: false });

  const finished = batch({ state: 'finished', total: 2, done: 2 });
  assert.deepEqual(primaryAction(snapshot([item({ status: 'done' }), item({ id: 'b', status: 'done' })], { batch: finished })),
    { kind: 'open', label: 'Open folder', enabled: true });
  assert.deepEqual(primaryAction(snapshot([item({ status: 'done' }), item({ id: 'b', status: 'failed' })], { batch: finished })),
    { kind: 'convert', label: 'Try again', enabled: true });
  assert.deepEqual(primaryAction(snapshot([item({ status: 'failed' }), item({ id: 'b', status: 'cancelled' })], { batch: finished })),
    { kind: 'convert', label: 'Try 2 again', enabled: true });
  // A new file next to a failed one is simply more to convert.
  assert.equal(primaryAction(snapshot([item({ status: 'failed' }), item({ id: 'b' })])).label, 'Convert 2 files');
});

test('overall progress counts the running file by how far it is', () => {
  const running = (progress: number, done: number) => snapshot(
    [item({ status: 'done' }), item({ id: 'b', status: 'converting', progress }), item({ id: 'c', status: 'queued' }), item({ id: 'd', status: 'queued' })],
    { batch: batch({ state: 'running', total: 4, done, currentId: 'b' }) });
  assert.equal(overallProgress(running(0, 1)), 25);
  assert.equal(overallProgress(running(50, 1)), 37.5);
  assert.equal(overallProgress(running(250, 1)), 50);
  assert.equal(overallProgress(running(-10, 1)), 25);
  assert.equal(overallProgress(snapshot([])), 0);
  assert.equal(overallProgress(snapshot([item({ status: 'done' })], { batch: batch({ state: 'finished', total: 1, done: 1 }) })), 100);
});

test('state helpers', () => {
  assert.equal(isBusy(null), false);
  assert.equal(isBusy(snapshot([], { batch: batch({ state: 'running' }) })), true);
  assert.equal(isBusy(snapshot([], { batch: batch({ state: 'cancelling' }) })), true);
  assert.equal(isBusy(snapshot([], { batch: batch({ state: 'finished' }) })), false);
  for (const status of ['ready', 'failed', 'cancelled'] as const) assert.equal(isConvertible(item({ status })), true);
  for (const status of ['checking', 'unsupported', 'queued', 'converting', 'done', 'skipped'] as const) {
    assert.equal(isConvertible(item({ status })), false);
  }
});

test('errors become readable sentences', () => {
  assert.equal(errorText(new Error('boom')), 'boom');
  assert.equal(errorText('there is nothing to convert'), 'there is nothing to convert');
  assert.equal(errorText(undefined), 'Something went wrong. Try again, or restart the app.');
  assert.equal(errorText(''), 'Something went wrong. Try again, or restart the app.');
  assert.equal(asSentence('there is nothing to convert'), 'There is nothing to convert.');
  assert.equal(asSentence('Already JPG, so there is nothing to convert.'), 'Already JPG, so there is nothing to convert.');
  assert.equal(asSentence('  is it done?  '), 'Is it done?');
  assert.equal(asSentence(''), '');
});
