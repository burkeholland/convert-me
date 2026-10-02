import './style.css';
import type { AddResult, Conflict, Item, Snapshot } from './types.ts';
import {
  asSentence, errorText, fileSize, folderLabel, isBusy, itemMeta, overallProgress,
  primaryAction, sizeChange, summarize,
} from './format.ts';
import {
  alertIcon, audioIcon, checkIcon, closeIcon, fileIcon, folderIcon, imageIcon, infoIcon, logoIcon,
  maxIcon, minIcon, moonIcon, plusIcon, removeIcon, sunIcon, videoIcon,
} from './icons.ts';

const app = document.querySelector<HTMLDivElement>('#app')!;
app.innerHTML = `
  <div class="window" id="window">
    <header class="titlebar">
      <div class="titlebar-grab">
        <span class="logo">${logoIcon}</span>
        <span class="wordmark">Convert Me</span>
      </div>
      <div class="titlebar-actions">
        <button id="about-open" class="icon-button" type="button" aria-label="About Convert Me" title="About Convert Me">${infoIcon}</button>
        <button id="theme" class="icon-button" type="button" aria-label="Switch to dark theme" title="Switch to dark theme">${moonIcon}</button>
      </div>
      <div class="window-buttons" id="window-buttons" hidden>
        <button id="win-minimise" class="win-button" type="button" aria-label="Minimise">${minIcon}</button>
        <button id="win-maximise" class="win-button" type="button" aria-label="Maximise">${maxIcon}</button>
        <button id="win-close" class="win-button win-button-close" type="button" aria-label="Close">${closeIcon}</button>
      </div>
    </header>

    <main class="stage" id="stage">
      <div id="setup-error" class="banner" role="alert" hidden></div>

      <section id="empty" class="dropzone" aria-labelledby="empty-title">
        <div class="dropzone-body">
          <h1 id="empty-title">Drop files to convert</h1>
          <p class="lede">Images, audio and video. Everything happens on this PC, and nothing is uploaded.</p>
          <button id="choose-files" class="button-primary" type="button" disabled>${plusIcon}<span>Choose files</span></button>
          <table class="formats" aria-label="Formats Convert Me reads and writes">
            <thead><tr><th scope="col">Kind</th><th scope="col">Reads</th><th scope="col">Writes</th></tr></thead>
            <tbody id="formats-body"></tbody>
          </table>
        </div>
      </section>

      <section id="list" class="list" aria-label="Files to convert" hidden>
        <header class="list-head">
          <p class="list-summary" role="status" aria-live="polite"><strong id="summary-title"></strong><span id="summary-detail"></span></p>
          <div class="list-tools">
            <button id="add-files" class="button-secondary button-small" type="button">${plusIcon}<span>Add files</span></button>
            <button id="clear" class="button-quiet button-small" type="button">Clear</button>
          </div>
        </header>
        <div class="rows-frame">
          <progress id="overall" class="overall" max="100" value="0" aria-label="Progress of all files" hidden></progress>
          <ul id="rows" class="rows"></ul>
        </div>
      </section>

      <div id="toast" class="toast" role="status" aria-live="polite" hidden></div>
    </main>

    <footer class="bar" id="bar" hidden>
      <div class="bar-controls">
        <div id="pickers" class="pickers"></div>
        <label class="field field-destination"><span>Save to</span><select id="destination"></select></label>
      </div>
      <div class="bar-actions">
        <button id="open-folder" class="button-secondary" type="button" hidden>${folderIcon}<span>Open folder</span></button>
        <button id="primary" class="button-primary" type="button" disabled><span id="primary-label">Convert</span></button>
      </div>
    </footer>

    <div class="drop-hint" aria-hidden="true"><span>Drop to add files</span></div>
  </div>

  <dialog id="conflict" class="dialog" aria-labelledby="conflict-title">
    <h2 id="conflict-title"></h2>
    <p class="dialog-text">Nothing has been changed yet. Keep both saves the new files with a number, like photo (1).jpg. Replace overwrites the files listed here. Your originals are never replaced.</p>
    <ul id="conflict-list" class="dialog-list"></ul>
    <div class="dialog-actions">
      <button id="conflict-cancel" class="button-quiet" type="button">Cancel</button>
      <button id="conflict-replace" class="button-secondary" type="button">Replace</button>
      <button id="conflict-keep" class="button-primary" type="button">Keep both</button>
    </div>
  </dialog>

  <dialog id="about" class="dialog" aria-labelledby="about-title">
    <h2 id="about-title">Convert Me <span id="about-version" class="dialog-version"></span></h2>
    <p class="dialog-text">Converts images, audio and video on this PC. No uploads, no account, and no history is kept.</p>
    <p class="dialog-text" id="about-engine"></p>
    <div class="dialog-actions">
      <button id="about-licenses" class="button-secondary" type="button">Open licenses folder</button>
      <button id="about-close" class="button-primary" type="button">Close</button>
    </div>
  </dialog>

  <div id="browser-notice" class="browser-notice" hidden>
    <span class="logo">${logoIcon}</span>
    <h1>Desktop app required</h1>
    <p>Convert Me converts files with an engine that is part of the Windows app. This browser page cannot open or convert your files.</p>
    <p>Open <strong>ConvertMe.exe</strong> to use it.</p>
  </div>`;

function el<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}

function text(element: HTMLElement, value: string): void {
  if (element.textContent !== value) element.textContent = value;
}

const bridge = window.go?.main?.App;

let snapshot: Snapshot | null = null;
let pending = false;
let disconnected = false;
let polling = false;
let stopped = false;
let timer: ReturnType<typeof setTimeout> | undefined;
let toastTimer: ReturnType<typeof setTimeout> | undefined;
let lastNotice = 0;
let lastRevision = -1;
let lastChangeAt = 0;
let formatsSignature = '';
let pickersSignature = '';
let destinationSignature = '';

/* ---------- window chrome ---------- */

el('win-minimise').addEventListener('click', () => void bridge?.MinimiseWindow?.());
el('win-maximise').addEventListener('click', () => void bridge?.ToggleMaximiseWindow?.());
el('win-close').addEventListener('click', () => void bridge?.CloseWindow?.());

function toast(message: string, error = false): void {
  const element = el('toast');
  clearTimeout(toastTimer);
  element.hidden = !message;
  element.classList.toggle('toast-error', error);
  element.setAttribute('role', error ? 'alert' : 'status');
  text(element, message);
  if (message) toastTimer = setTimeout(() => { el('toast').hidden = true; }, error ? 9000 : 6000);
}

/* ---------- actions ---------- */

function locked(): boolean {
  return pending || disconnected || !bridge;
}

// Every button press goes through here: one action at a time, errors as a toast, and a
// fresh snapshot straight afterwards so the result shows where the user is looking.
async function action(work: () => Promise<void>): Promise<void> {
  if (pending) return;
  pending = true;
  render();
  try {
    await work();
  } catch (error) {
    toast(asSentence(errorText(error)), true);
  } finally {
    pending = false;
    await refresh();
  }
}

function report(result: AddResult): void {
  if (result.message) toast(result.message, result.added === 0);
}

function chooseFiles(): void {
  if (!bridge || isBusy(snapshot)) return;
  void action(async () => report(await bridge.ChooseFiles()));
}

function addPaths(paths: string[]): void {
  if (!bridge || paths.length === 0) return;
  if (isBusy(snapshot)) {
    toast('Wait for the current conversion to finish before adding files.', true);
    return;
  }
  void action(async () => report(await bridge.AddFiles(paths)));
}

async function start(policy: string): Promise<void> {
  if (!bridge) return;
  const result = await bridge.Start(policy);
  if (!result.started && result.conflicts.length > 0) showConflicts(result.conflicts);
}

el('choose-files').addEventListener('click', chooseFiles);
el('add-files').addEventListener('click', chooseFiles);
el('clear').addEventListener('click', () => {
  if (bridge) void action(() => bridge.ClearItems());
});
el('open-folder').addEventListener('click', () => {
  if (bridge) void action(() => bridge.OpenOutputFolder());
});
el('primary').addEventListener('click', () => {
  if (!bridge || !snapshot) return;
  const next = primaryAction(snapshot);
  if (!next.enabled) return;
  if (next.kind === 'convert') void action(() => start(''));
  if (next.kind === 'cancel') void action(() => bridge.Cancel());
  if (next.kind === 'open') void action(() => bridge.OpenOutputFolder());
});

/* ---------- existing files prompt ---------- */

function showConflicts(conflicts: Conflict[]): void {
  text(el('conflict-title'), conflicts.length === 1
    ? '1 file already exists'
    : `${conflicts.length} files already exist`);
  const fragment = document.createDocumentFragment();
  for (const conflict of conflicts.slice(0, 5)) {
    const entry = document.createElement('li');
    const name = document.createElement('strong');
    name.textContent = conflict.name;
    const folder = document.createElement('span');
    folder.textContent = conflict.folder;
    entry.append(name, folder);
    fragment.append(entry);
  }
  if (conflicts.length > 5) {
    const more = document.createElement('li');
    more.className = 'dialog-more';
    more.textContent = `and ${conflicts.length - 5} more`;
    fragment.append(more);
  }
  el('conflict-list').replaceChildren(fragment);
  const dialog = el<HTMLDialogElement>('conflict');
  if (!dialog.open) dialog.showModal();
  el('conflict-keep').focus();
}

function closeConflict(): void {
  el<HTMLDialogElement>('conflict').close();
}

el('conflict-cancel').addEventListener('click', closeConflict);
el('conflict-keep').addEventListener('click', () => {
  closeConflict();
  void action(() => start('keep'));
});
el('conflict-replace').addEventListener('click', () => {
  closeConflict();
  void action(() => start('replace'));
});

/* ---------- about ---------- */

el('about-open').addEventListener('click', () => {
  const dialog = el<HTMLDialogElement>('about');
  if (!dialog.open) dialog.showModal();
});
el('about-close').addEventListener('click', () => el<HTMLDialogElement>('about').close());
el('about-licenses').addEventListener('click', () => {
  if (bridge) void action(() => bridge.OpenLicenses());
});

/* ---------- rows ---------- */

interface Row {
  root: HTMLLIElement;
  thumb: HTMLSpanElement;
  name: HTMLParagraphElement;
  meta: HTMLParagraphElement;
  note: HTMLParagraphElement;
  noteIcon: HTMLSpanElement;
  noteText: HTMLSpanElement;
  warning: HTMLParagraphElement;
  toggle: HTMLButtonElement;
  details: HTMLPreElement;
  spoken: HTMLSpanElement;
  source: HTMLSpanElement;
  track: HTMLProgressElement;
  percent: HTMLSpanElement;
  target: HTMLSpanElement;
  action: HTMLButtonElement;
  signature: string;
  art: string;
  open: boolean;
}

const rows = new Map<string, Row>();

function make<K extends keyof HTMLElementTagNameMap>(tag: K, className: string): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag);
  element.className = className;
  return element;
}

function createRow(item: Item): Row {
  const root = make('li', 'row');
  const thumb = make('span', 'thumb');
  const main = make('div', 'row-main');
  const name = make('p', 'row-name');
  const meta = make('p', 'row-meta');
  const note = make('p', 'row-note');
  const noteIcon = make('span', 'row-note-icon');
  const noteText = make('span', 'row-note-text');
  note.append(noteIcon, noteText);
  const warning = make('p', 'row-warning');
  const toggle = make('button', 'row-toggle');
  toggle.type = 'button';
  const details = make('pre', 'row-details');
  const spoken = make('span', 'sr-only');
  main.append(name, meta, note, warning, toggle, details, spoken);

  const route = make('div', 'route');
  route.setAttribute('aria-hidden', 'true');
  const source = make('span', 'tag tag-source');
  const rail = make('span', 'rail');
  const track = make('progress', 'track');
  track.max = 100;
  const percent = make('span', 'rail-percent');
  rail.append(track, percent);
  const target = make('span', 'tag tag-target');
  route.append(source, rail, target);

  const button = make('button', 'icon-button row-action');
  button.type = 'button';
  root.append(thumb, main, route, button);

  const row: Row = {
    root, thumb, name, meta, note, noteIcon, noteText, warning, toggle, details, spoken,
    source, track, percent, target, action: button, signature: '', art: '', open: false,
  };
  toggle.addEventListener('click', () => {
    row.open = !row.open;
    row.signature = '';
    render();
  });
  button.addEventListener('click', () => {
    if (!bridge) return;
    const id = item.id;
    if (root.dataset.status === 'done') void action(() => bridge.RevealItem(id));
    else void action(() => bridge.RemoveItem(id));
  });
  return row;
}

function kindIcon(item: Item): string {
  if (item.kind === 'image') return imageIcon;
  if (item.kind === 'video') return videoIcon;
  if (item.kind === 'audio') return audioIcon;
  return fileIcon;
}

function setArt(row: Row, item: Item): void {
  const wanted = item.thumb ? `thumb:${item.id}` : `icon:${item.kind}`;
  if (row.art === wanted) return;
  row.art = wanted;
  if (!item.thumb) {
    row.thumb.innerHTML = kindIcon(item);
    return;
  }
  const image = new Image();
  image.alt = '';
  image.addEventListener('error', () => { row.thumb.innerHTML = kindIcon(item); });
  image.src = `thumb/${encodeURIComponent(item.id)}`;
  row.thumb.replaceChildren(image);
}

interface Note {
  tone: 'ok' | 'bad' | 'muted' | '';
  icon: string;
  message: string;
}

function noteFor(item: Item): Note {
  switch (item.status) {
    case 'done': {
      const parts = [`Saved as ${item.outputName}`, fileSize(item.outputSize)];
      const change = sizeChange(item.size, item.outputSize);
      if (change) parts.push(change);
      return { tone: 'ok', icon: checkIcon, message: parts.join(' · ') };
    }
    case 'failed':
    case 'unsupported':
      return { tone: 'bad', icon: alertIcon, message: item.error };
    case 'skipped':
      return { tone: 'muted', icon: '', message: item.error };
    case 'cancelled':
      return { tone: 'muted', icon: '', message: 'Cancelled. The original was not changed.' };
    default:
      return { tone: '', icon: '', message: '' };
  }
}

function spokenStatus(item: Item): string {
  const route = item.targetLabel ? `${item.source} to ${item.targetLabel}` : item.source;
  switch (item.status) {
    case 'checking': return 'Checking this file.';
    case 'ready': return `${route}. Ready.`;
    case 'queued': return `${route}. Waiting.`;
    case 'converting': return `${route}. Converting, ${Math.round(item.progress)} percent.`;
    case 'done': return `${route}. Done.`;
    default: return '';
  }
}

function updateRow(row: Row, item: Item, busy: boolean): void {
  const signature = JSON.stringify([item, busy, row.open, locked()]);
  if (signature === row.signature) return;
  row.signature = signature;

  row.root.dataset.status = item.status;
  text(row.name, item.name);
  row.name.title = item.path;
  text(row.meta, item.status === 'checking' ? 'Checking' : itemMeta(item));
  setArt(row, item);

  // The result takes the place of the file details, so a row keeps its height when it finishes.
  const note = noteFor(item);
  row.meta.hidden = Boolean(note.message);
  row.note.hidden = !note.message;
  row.note.dataset.tone = note.tone;
  if (row.noteIcon.dataset.icon !== note.icon) {
    row.noteIcon.dataset.icon = note.icon;
    row.noteIcon.innerHTML = note.icon;
  }
  row.noteIcon.hidden = !note.icon;
  text(row.noteText, note.message);

  const settled = item.status === 'failed' || item.status === 'unsupported' || item.status === 'skipped';
  row.warning.hidden = !item.warning || settled;
  text(row.warning, item.warning);

  const hasDetails = Boolean(item.detail) && (item.status === 'failed' || item.status === 'unsupported');
  row.toggle.hidden = !hasDetails;
  text(row.toggle, row.open ? 'Hide details' : 'Show details');
  row.toggle.setAttribute('aria-expanded', String(row.open && hasDetails));
  row.details.hidden = !(row.open && hasDetails);
  text(row.details, item.detail);
  text(row.spoken, spokenStatus(item));

  text(row.source, item.source || '?');
  // A file that will not be converted shows what it is, without an arrow to nowhere.
  const routed = Boolean(item.targetLabel) && !['unsupported', 'checking', 'skipped'].includes(item.status);
  row.root.classList.toggle('row-unrouted', !routed);
  text(row.target, routed ? item.targetLabel : '');
  if (item.status === 'converting' && item.progress <= 0) {
    row.track.removeAttribute('value');
  } else {
    row.track.value = item.status === 'done' ? 100 : item.status === 'converting' ? Math.min(100, item.progress) : 0;
  }
  text(row.percent, item.status === 'converting' && item.progress > 0 ? `${Math.round(item.progress)}%` : '');

  const done = item.status === 'done';
  const label = done ? `Show ${item.outputName} in its folder` : `Remove ${item.name} from the list`;
  if (row.action.getAttribute('aria-label') !== label) {
    row.action.setAttribute('aria-label', label);
    row.action.title = done ? 'Show in folder' : 'Remove from list';
    row.action.innerHTML = done ? folderIcon : removeIcon;
  }
  row.action.hidden = busy && !done;
  row.action.disabled = locked();
}

function renderRows(busy: boolean): void {
  if (!snapshot) return;
  const list = el('rows');
  const seen = new Set<string>();
  let previous: Element | null = null;
  for (const item of snapshot.items) {
    seen.add(item.id);
    let row = rows.get(item.id);
    if (!row) {
      row = createRow(item);
      rows.set(item.id, row);
    }
    updateRow(row, item, busy);
    const expected: Element | null = previous ? previous.nextElementSibling : list.firstElementChild;
    if (expected !== row.root) list.insertBefore(row.root, expected);
    previous = row.root;
  }
  for (const [id, row] of rows) {
    if (!seen.has(id)) {
      row.root.remove();
      rows.delete(id);
    }
  }
}

/* ---------- format table, pickers, destination ---------- */

function renderFormats(): void {
  if (!snapshot) return;
  const signature = JSON.stringify(snapshot.formats);
  if (signature === formatsSignature) return;
  formatsSignature = signature;
  const fragment = document.createDocumentFragment();
  for (const format of snapshot.formats) {
    const line = document.createElement('tr');
    const kind = document.createElement('th');
    kind.scope = 'row';
    kind.textContent = format.label;
    const reads = make('td', 'formats-reads');
    reads.textContent = format.reads.join(' ');
    const writes = make('td', 'formats-writes');
    writes.textContent = format.writes.join(' ');
    if (format.kind === 'video') {
      const extra = make('span', 'formats-extra');
      extra.textContent = 'or audio only';
      writes.append(' ', extra);
    }
    line.append(kind, reads, writes);
    fragment.append(line);
  }
  el('formats-body').replaceChildren(fragment);
}

function renderPickers(disabled: boolean): void {
  if (!snapshot) return;
  const host = el('pickers');
  const signature = JSON.stringify(snapshot.kinds.map(kind => [kind.kind, kind.label, kind.target, kind.options]));
  if (signature !== pickersSignature) {
    pickersSignature = signature;
    const fragment = document.createDocumentFragment();
    for (const kind of snapshot.kinds) {
      const field = make('label', 'field');
      const caption = document.createElement('span');
      // The word "to" is dropped in a narrow window so all three pickers fit on one line.
      const joiner = make('span', 'field-joiner');
      joiner.textContent = ' to';
      caption.append(kind.label, joiner);
      const select = document.createElement('select');
      select.id = `target-${kind.kind}`;
      let group: HTMLOptGroupElement | null = null;
      for (const option of kind.options) {
        const node = new Option(option.available ? option.label : `${option.label} (not available)`, option.id);
        node.disabled = !option.available;
        if (option.reason) node.title = option.reason;
        if (!option.group) {
          select.append(node);
          continue;
        }
        if (!group || group.label !== option.group) {
          group = document.createElement('optgroup');
          group.label = option.group;
          select.append(group);
        }
        group.append(node);
      }
      select.value = kind.target;
      select.addEventListener('change', () => {
        if (bridge) void action(() => bridge.SetTarget(kind.kind, select.value));
      });
      field.append(caption, select);
      fragment.append(field);
    }
    host.replaceChildren(fragment);
  }
  for (const select of host.querySelectorAll('select')) select.disabled = disabled;
}

function renderDestination(disabled: boolean): void {
  if (!snapshot) return;
  const select = el<HTMLSelectElement>('destination');
  const { mode, folder } = snapshot.destination;
  const signature = `${mode}|${folder}`;
  if (signature !== destinationSignature) {
    destinationSignature = signature;
    const options = [new Option('Same folder as original', 'source')];
    if (folder) options.push(new Option(folderLabel(folder), 'folder'));
    options.push(new Option(folder ? 'Choose another folder...' : 'Choose a folder...', 'choose'));
    select.replaceChildren(...options);
    select.value = mode === 'folder' && folder ? 'folder' : 'source';
    select.title = mode === 'folder' && folder ? folder : 'Each converted file is saved next to its original';
  }
  select.disabled = disabled;
}

el<HTMLSelectElement>('destination').addEventListener('change', () => {
  if (!bridge) return;
  const select = el<HTMLSelectElement>('destination');
  const choice = select.value;
  // The menu is rebuilt from the next snapshot, so a dismissed folder dialog snaps back.
  destinationSignature = '';
  void action(async () => {
    if (choice === 'source') await bridge.UseSourceFolder();
    else if (choice === 'folder') await bridge.UseChosenFolder();
    else await bridge.ChooseDestination();
  });
});

/* ---------- render ---------- */

function render(): void {
  if (!snapshot) return;
  const busy = isBusy(snapshot);
  const ready = snapshot.state === 'ready';
  const hasItems = snapshot.items.length > 0;

  el('setup-error').hidden = !snapshot.setupError && !disconnected;
  text(el('setup-error'), disconnected
    ? 'The app stopped answering. It keeps trying to reconnect.'
    : snapshot.setupError);

  el('empty').hidden = hasItems;
  el('list').hidden = !hasItems;
  el('bar').hidden = !hasItems;
  el<HTMLButtonElement>('choose-files').disabled = !ready || locked();
  renderFormats();

  const summary = summarize(snapshot);
  text(el('summary-title'), summary.title);
  text(el('summary-detail'), summary.detail);
  // In a narrow window the line can be cut short. The tooltip always has all of it.
  el('summary-detail').title = summary.detail;
  el<HTMLButtonElement>('add-files').disabled = busy || !ready || locked();
  el<HTMLButtonElement>('clear').disabled = busy || locked();
  const overall = el<HTMLProgressElement>('overall');
  overall.hidden = !busy;
  overall.value = busy ? overallProgress(snapshot) : 0;
  el('window').classList.toggle('is-busy', busy);

  renderRows(busy);
  renderPickers(busy || locked());
  renderDestination(busy || locked());

  const next = primaryAction(snapshot);
  const primary = el<HTMLButtonElement>('primary');
  text(el('primary-label'), next.label);
  primary.dataset.kind = next.kind;
  primary.disabled = !next.enabled || locked();
  const outputs = snapshot.items.some(item => item.status === 'done');
  el('open-folder').hidden = !(outputs && next.kind === 'convert');
  el<HTMLButtonElement>('open-folder').disabled = locked();

  text(el('about-version'), snapshot.version);
  text(el('about-engine'), `Conversions are done by ${snapshot.engine}, a free tool under the LGPL license that ships next to this app. H.264 video uses the encoder that comes with Windows.`);
}

/* ---------- polling ---------- */

// Poll quickly while something is moving, and for a few seconds after the last change so
// thumbnails that are still being made show up promptly. Otherwise poll slowly.
function wait(): number {
  if (!snapshot) return 300;
  const active = snapshot.state === 'starting' || isBusy(snapshot) ||
    snapshot.items.some(item => item.status === 'checking') || Date.now() - lastChangeAt < 4000;
  return active ? 250 : 1200;
}

async function poll(): Promise<void> {
  if (stopped || !bridge || polling) return;
  polling = true;
  clearTimeout(timer);
  try {
    const next = await bridge.Status();
    if (stopped) return;
    disconnected = false;
    snapshot = next;
    if (next.revision !== lastRevision) {
      lastRevision = next.revision;
      lastChangeAt = Date.now();
    }
    if (next.noticeSeq !== lastNotice) {
      lastNotice = next.noticeSeq;
      if (next.notice) toast(next.notice, next.noticeError);
    }
  } catch {
    disconnected = true;
  } finally {
    polling = false;
    render();
    if (!stopped) timer = setTimeout(() => void poll(), wait());
  }
}

// Ask for the newest state now instead of waiting for the next tick.
async function refresh(): Promise<void> {
  if (polling) {
    render();
    return;
  }
  await poll();
}

/* ---------- preferences ---------- */

function writeStored(key: string, value: string): void {
  try { localStorage.setItem(key, value); } catch { /* Preferences are optional. */ }
}

function updateTheme(): void {
  const dark = document.documentElement.dataset.theme === 'dark';
  const label = `Switch to ${dark ? 'light' : 'dark'} theme`;
  const button = el('theme');
  button.setAttribute('aria-label', label);
  button.setAttribute('title', label);
  button.innerHTML = dark ? sunIcon : moonIcon;
}
updateTheme();
el('theme').addEventListener('click', () => {
  const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  writeStored('convert-me-theme', next);
  updateTheme();
});

document.addEventListener('keydown', event => {
  if (event.key.toLowerCase() === 'o' && (event.ctrlKey || event.metaKey) && !event.altKey) {
    event.preventDefault();
    if (snapshot?.state === 'ready' && !document.querySelector('dialog[open]')) chooseFiles();
  }
});

/* ---------------- dropping files ---------------- */

// While files are dragged over the window the browser sends "dragover" many times a
// second. The hint shows on each one and hides shortly after the last, which also covers
// the drag leaving the window or being abandoned.
let dropHintTimer: ReturnType<typeof setTimeout> | undefined;

function showDropHint(visible: boolean): void {
  clearTimeout(dropHintTimer);
  el('window').classList.toggle('is-dropping', visible);
  if (visible) dropHintTimer = setTimeout(() => showDropHint(false), 220);
}

window.addEventListener('dragover', event => {
  if (!event.dataTransfer?.types.includes('Files')) return;
  // Without this the window would try to open the dropped file itself.
  event.preventDefault();
  showDropHint(!document.querySelector('dialog[open]'));
});
window.addEventListener('drop', event => {
  if (event.dataTransfer?.types.includes('Files')) event.preventDefault();
  showDropHint(false);
});

/* ---------------- boot ---------------- */

if (!bridge) {
  el('window').hidden = true;
  el('browser-notice').hidden = false;
} else {
  el('window-buttons').hidden = false;
  // The whole window is the drop area, except while a question is open on top of it.
  window.runtime?.OnFileDrop?.((_x, _y, paths) => {
    if (!document.querySelector('dialog[open]')) addPaths(paths);
  }, false);
  void poll();
}

window.addEventListener('beforeunload', () => {
  stopped = true;
  clearTimeout(timer);
  clearTimeout(dropHintTimer);
  window.runtime?.OnFileDropOff?.();
});
