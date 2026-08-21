import './style.css';
import './app.css';

import {
    CancelConversion,
    ClearLaunchRequest,
    GetFileSizes,
    GetFormats,
    GetLaunchRequest,
    GetSettings,
    IsExplorerIntegrationRegistered,
    OpenCodecStore,
    PreviewFile,
    Quit,
    SaveSettings,
    SelectFiles,
    StartConversion,
} from '../wailsjs/go/main/App';
import {EventsOn, OnFileDrop, WindowHide, WindowMinimise} from '../wailsjs/runtime/runtime';

type View = 'convert' | 'settings' | 'quick';
type Phase = 'idle' | 'running' | 'done';
type FormatOption = {
    id: string;
    name: string;
    extension: string;
    lossy: boolean;
    supportsAlpha: boolean;
};
type Settings = {
    jpegQuality: number;
    webpQuality: number;
    preserveMetadata: boolean;
    launchAtLogin: boolean;
    showNotifications: boolean;
    explorerIntegration: boolean;
};
type FileResult = {
    input: string;
    output: string;
    status: string;
    error: string;
};
type ConversionSummary = {
    total: number;
    completed: number;
    failed: number;
    canceled: number;
    results: FileResult[];
};
type ImageFile = {
    path: string;
    name: string;
    dir: string;
    ext: string;
};

const fallbackFormats: FormatOption[] = [
    {id: 'jpeg', name: 'JPEG', extension: '.jpg', lossy: true, supportsAlpha: false},
    {id: 'png', name: 'PNG', extension: '.png', lossy: false, supportsAlpha: true},
    {id: 'webp', name: 'WebP', extension: '.webp', lossy: true, supportsAlpha: true},
    {id: 'bmp', name: 'BMP', extension: '.bmp', lossy: false, supportsAlpha: false},
    {id: 'tiff', name: 'TIFF', extension: '.tiff', lossy: false, supportsAlpha: true},
    {id: 'heic', name: 'HEIC', extension: '.heic', lossy: true, supportsAlpha: false},
];

const formatHints: Record<string, string> = {
    jpeg: 'Universal choice for photos · lossy',
    png: 'Lossless · keeps transparency',
    webp: 'Smaller files · modern format',
    bmp: 'Uncompressed · very large files',
    tiff: 'High fidelity · large files',
    heic: 'Modern compression · needs HEIF extensions',
};

const defaultSettings: Settings = {
    jpegQuality: 88,
    webpQuality: 82,
    preserveMetadata: true,
    launchAtLogin: true,
    showNotifications: true,
    explorerIntegration: true,
};

const state = {
    view: 'convert' as View,
    phase: 'idle' as Phase,
    settings: {...defaultSettings} as Settings,
    formats: fallbackFormats,
    files: [] as ImageFile[],
    thumbs: {} as Record<string, string>,
    fileSizes: {} as Record<string, number>,
    outputFormat: 'jpeg',
    quality: defaultSettings.jpegQuality,
    results: [] as FileResult[],
    summary: null as ConversionSummary | null,
    explorerOn: true,
    quickFormat: '',
    toast: null as {message: string; action?: 'codec'} | null,
};

const app = document.querySelector<HTMLDivElement>('#app')!;
const inApp = typeof (window as any).go?.main?.App !== 'undefined';
let toastTimer = 0;

function fileName(path: string): string {
    return path.split(/[\\/]/).pop() ?? path;
}

function folderName(path: string): string {
    const parts = path.split(/[\\/]/);
    return parts.length > 1 ? parts.slice(0, -1).join('\\') : '';
}

function fileExt(path: string): string {
    const name = fileName(path);
    const dot = name.lastIndexOf('.');
    return dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
}

function formatSize(bytes: number): string {
    if (bytes < 0) return '';
    if (bytes < 1024) return `${bytes} B`;
    const units = ['KB', 'MB', 'GB'];
    let value = bytes / 1024;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit++;
    }
    return `${value >= 100 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

function escapeHtml(value: string): string {
    return value
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#039;');
}

function icon(name: string, size = 20): string {
    const paths: Record<string, string> = {
        check: '<path d="m5 10 3 3 7-7" />',
        clock: '<circle cx="10" cy="10" r="7" /><path d="M10 6v4l2.5 1.5" />',
        convert: '<path d="M4 7h8m-3-3 3 3-3 3M16 13H8m3-3-3 3 3 3" />',
        folder: '<path d="M3 6a1.5 1.5 0 0 1 1.5-1.5h3L9 6.5h6.5A1.5 1.5 0 0 1 17 8v7a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 3 15Z" />',
        gear: '<path d="M8.3 3.5 9 2h2l.7 1.5 1.3.8 1.6-.3 1.4 1.4-.3 1.6.8 1.3L18 9v2l-1.5.7-.8 1.3.3 1.6-1.4 1.4-1.6-.3-1.3.8L11 18H9l-.7-1.5-1.3-.8-1.6.3L4 16.6l.3-1.6L3.5 13 2 12.3v-2l1.5-.7.8-1.3L4 6.7l1.4-1.4 1.6.3 1.3-.8Z" /><circle cx="10" cy="11" r="2.4" />',
        image: '<rect x="3" y="3" width="14" height="14" rx="2" /><circle cx="7" cy="7" r="1.2" /><path d="m4 14 3.5-3.5 2.3 2.2 1.8-1.8L16 15" />',
        info: '<circle cx="10" cy="10" r="7" /><path d="M10 9v4M10 6.5h.01" />',
        plus: '<path d="M10 4v12M4 10h12" />',
        upload: '<path d="M10 13V4m0 0L6.5 7.5M10 4l3.5 3.5M4 12v3a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-3" />',
        x: '<path d="m5 5 10 10M15 5 5 15" />',
    };
    return `<svg class="icon" width="${size}" height="${size}" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] ?? paths.image}</svg>`;
}

function brandMark(size = 16): string {
    return `<svg class="brand-mark" width="${size}" height="${size}" viewBox="0 0 24 24" aria-hidden="true">
        <rect width="24" height="24" rx="6" fill="var(--accent-default)" />
        <path d="M7 7.5h8.5m-2.4-2.4 2.4 2.4-2.4 2.4M17 16.5H8.5m2.4 2.4-2.4-2.4 2.4-2.4" fill="none" stroke="var(--text-on-accent)" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
    </svg>`;
}

function currentFormat(): FormatOption {
    return state.formats.find((format) => format.id === state.outputFormat) ?? state.formats[0];
}

function qualityFor(format: FormatOption): number {
    if (!format.lossy) return 0;
    return format.id === 'webp' ? state.settings.webpQuality : state.settings.jpegQuality;
}

function isCodecError(error: string): boolean {
    return error.toLowerCase().includes('heif codec');
}

function isSupportedPath(path: string): boolean {
    const ext = fileExt(path);
    return state.formats.some(
        (format) => format.id === ext || (format.id === 'jpeg' && (ext === 'jpg' || ext === 'jpeg')) || (format.id === 'heic' && ext === 'heif') || (format.id === 'tiff' && ext === 'tif'),
    );
}

function render(): void {
    app.innerHTML = state.view === 'quick' ? renderQuick() : state.view === 'settings' ? renderSettings() : renderConvert();
    if (state.toast) {
        app.insertAdjacentHTML('beforeend', renderToast());
    }
    syncControls();
}

function renderTitlebar(closeRole: 'window-hide' | 'window-quit'): string {
    return `
    <header class="titlebar">
        ${brandMark(16)}
        <span class="titlebar-title">ConvertMe</span>
        <div class="titlebar-controls">
            <button type="button" class="titlebar-btn" data-role="window-min" aria-label="Minimize">&#x2013;</button>
            <button type="button" class="titlebar-btn titlebar-btn-close" data-role="${closeRole}" aria-label="Close">&#x2715;</button>
        </div>
    </header>`;
}

function renderQuick(): string {
    const format = state.formats.find((f) => f.id === state.quickFormat);
    const done = state.results.length;
    const total = state.summary?.total ?? state.files.length;
    const percent = total > 0 ? Math.round((done / total) * 100) : 0;
    const failed = state.results.filter((r) => r.status !== 'completed');
    const finished = state.phase === 'done';
    const heading = finished
        ? failed.length
            ? `${failed.length} of ${total} could not be converted`
            : `Converted ${total} image${total === 1 ? '' : 's'}`
        : `Converting to ${format?.name ?? state.quickFormat.toUpperCase()}…`;
    return `
    <div class="app-window">
        ${renderTitlebar('window-quit')}
        <div class="quick-window">
            <div class="quick-head">
                ${brandMark(20)}
                <strong>${escapeHtml(heading)}</strong>
            </div>
            ${finished ? '' : `<progress max="100" value="${percent}" aria-label="Conversion progress"></progress>
            <span class="quick-status">${done} of ${total}</span>`}
            ${finished && failed.length ? `
            <ul class="quick-errors" role="list">
                ${failed.map((r) => `<li><strong>${escapeHtml(fileName(r.input))}</strong><span>${escapeHtml(r.error)}</span></li>`).join('')}
            </ul>
            <button type="button" class="accent-btn" data-role="quick-close">Close</button>` : ''}
        </div>
    </div>`;
}

function renderShell(content: string): string {
    return `
    <div class="app-window">
        ${renderTitlebar('window-hide')}
        <div class="app-body">
            <nav class="nav-pane" aria-label="Main">
                <div class="nav-items">
                    ${navItem('convert', 'Convert', 'convert', state.view === 'convert')}
                    ${navItem('settings', 'Settings', 'gear', state.view === 'settings')}
                </div>
            </nav>
            <main class="main-panel">${content}</main>
        </div>
    </div>`;
}

function navItem(view: View, label: string, iconName: string, active: boolean): string {
    return `<button class="nav-item${active ? ' is-active' : ''}" type="button" data-role="nav" data-view="${view}" ${active ? 'aria-current="page"' : ''}>
        ${icon(iconName)}<span>${label}</span>
    </button>`;
}

function renderConvert(): string {
    const empty = state.files.length === 0;
    return renderShell(`
        <section class="page convert-page">
            <header class="page-header">
                <h1>Convert</h1>
                <p>Drop images here, or use the right-click menu in File Explorer.</p>
            </header>
            ${state.phase === 'done' ? renderResult() : renderWorking(empty)}
        </section>`);
}

function renderWorking(empty: boolean): string {
    if (empty) {
        return `
        <div class="dropzone" data-role="dropzone" tabindex="0" role="button" aria-label="Choose images to convert">
            ${icon('upload', 28)}
            <strong>Drop images here</strong>
            <button type="button" class="accent-btn" data-role="browse">Browse files</button>
            <span class="dropzone-formats">PNG · JPEG · WebP · BMP · TIFF · HEIC</span>
        </div>`;
    }
    const running = state.phase === 'running';
    const done = state.results.length;
    const total = state.summary?.total ?? state.files.length;
    const percent = total > 0 ? Math.round((done / total) * 100) : 0;
    return `
    <div class="card-group files-card">
        ${renderFileList()}
        ${running ? '' : `<div class="files-drop-hint" aria-hidden="true">${icon('plus', 14)}<span>Drop images to add more</span></div>`}
    </div>
    ${running ? '' : renderOutputCard()}
    ${running ? `
    <div class="progress-strip" role="status" aria-label="Conversion progress">
        <progress max="100" value="${percent}"></progress>
        <div class="progress-strip-row">
            <span class="progress-label">Converting ${done} of ${total}…</span>
            <button type="button" class="ghost-btn" data-role="cancel">Cancel</button>
        </div>
    </div>` : `
    <div class="action-row">
        <p class="action-hint">${state.files.length} image${state.files.length === 1 ? '' : 's'} will be saved next to the originals.</p>
        <button type="button" class="accent-btn convert-button" data-role="convert">Convert to ${escapeHtml(currentFormat().name)}</button>
    </div>`}`;
}

function renderFileList(): string {
    const rows = state.files
        .map((file) => {
            const thumb = state.thumbs[file.path];
            const visual = thumb
                ? `<img class="thumb" src="${thumb}" alt="" data-thumb="${escapeHtml(file.path)}" />`
                : `<span class="thumb thumb-fallback" data-thumb-box="${escapeHtml(file.path)}">${escapeHtml(file.ext.slice(0, 4).toUpperCase())}</span>`;
            const size = state.fileSizes[file.path];
            const sizeText = size !== undefined && size >= 0 ? formatSize(size) : '';
            const result = state.phase === 'running' || state.phase === 'done'
                ? state.results.find((r) => r.input === file.path)
                : undefined;
            const status = state.phase === 'running'
                ? result
                    ? `<span class="row-status ${result.status === 'completed' ? 'is-ok' : 'is-failed'}" title="${result.status === 'completed' ? 'Done' : escapeHtml(result.error || 'Failed')}">${icon(result.status === 'completed' ? 'check' : 'x', 13)}</span>`
                    : `<span class="row-status is-pending" title="Waiting"><span class="pending-dot"></span></span>`
                : '';
            return `
            <li class="file-row" role="listitem">
                ${visual}
                <span class="file-meta">
                    <strong class="file-name" title="${escapeHtml(file.name)}">${escapeHtml(file.name)}</strong>
                    ${file.dir ? `<small class="file-dir" title="${escapeHtml(file.dir)}">${escapeHtml(file.dir)}</small>` : ''}
                </span>
                ${status}
                ${sizeText ? `<span class="file-size" title="${size} bytes">${sizeText}</span>` : ''}
                ${state.phase === 'idle' ? `<button type="button" class="ghost-btn row-remove" title="Remove" aria-label="Remove ${escapeHtml(file.name)}" data-role="remove" data-path="${escapeHtml(file.path)}">${icon('x', 14)}</button>` : ''}
            </li>`;
        })
        .join('');
    return `
    <header class="files-header">
        <h2>Images <span class="count">${state.files.length}</span></h2>
        <div class="files-actions">
            <button type="button" class="ghost-btn" data-role="browse" ${state.phase !== 'idle' ? 'disabled' : ''}>${icon('plus', 14)}Add</button>
            <button type="button" class="ghost-btn" data-role="clear" ${state.phase !== 'idle' ? 'disabled' : ''}>Remove all</button>
        </div>
    </header>
    <ul class="file-list" role="list">${rows}</ul>`;
}

function renderOutputCard(): string {
    const format = currentFormat();
    const qualityRow = format.lossy
        ? `
        <div class="field quality-field">
            <div class="field-head">
                <label id="quality-label" for="quality">Quality</label>
                <output data-role="quality-output">${state.quality}</output>
            </div>
            <input id="quality" class="win-slider" type="range" data-role="quality" min="1" max="100" value="${state.quality}" aria-labelledby="quality-label">
        </div>`
        : '';
    const alphaInput = state.files.some((file) => ['png', 'webp', 'tiff'].includes(file.ext));
    const alphaNote = !format.supportsAlpha && alphaInput
        ? `<p class="output-note">${icon('info', 14)}<span>Transparent areas will be filled with white.</span></p>`
        : '';
    return `
    <div class="card-group output-card">
        <h2>Output</h2>
        <div class="field">
            <label id="format-label" for="format">Format</label>
            <select id="format" class="win-select" data-role="format" aria-labelledby="format-label">
                ${state.formats.map((option) => `<option value="${option.id}"${option.id === state.outputFormat ? ' selected' : ''}>${option.name}</option>`).join('')}
            </select>
            <small class="field-hint">${formatHints[format.id] ?? ''}</small>
        </div>
        ${qualityRow}
        <div class="field switch-field">
            <span class="switch-copy">
                <label id="metadata-label" for="metadata">Preserve metadata</label>
                <small>Keep camera details when the format supports it.</small>
            </span>
            <input id="metadata" class="toggle-switch" type="checkbox" role="switch" data-role="metadata" aria-labelledby="metadata-label" ${state.settings.preserveMetadata ? 'checked' : ''}>
        </div>
        ${alphaNote}
    </div>`;
}

function renderResult(): string {
    const summary = state.summary;
    const completed = summary?.completed ?? 0;
    const failed = summary?.failed ?? 0;
    const canceled = summary?.canceled ?? 0;
    const total = summary?.total ?? state.results.length;
    const firstOk = state.results.find((result) => result.status === 'completed');
    const folder = firstOk ? folderName(firstOk.output) : '';

    const heading = canceled > 0
        ? 'Conversion canceled'
        : failed > 0 && completed > 0
            ? `Converted ${completed} of ${total}`
            : failed > 0
                ? 'Conversion failed'
                : `Converted ${total} image${total === 1 ? '' : 's'}`;
    const headIcon = canceled > 0 ? 'clock' : failed > 0 && completed === 0 ? 'x' : 'check';

    const rows = state.results.map((result) => resultRow(result, true)).join('');
    return `
    <div class="card-group status-card">
        <div class="status-head ${failed > 0 ? 'has-failures' : ''}">
            <span class="status-mark">${icon(headIcon, 22)}</span>
            <div>
                <strong role="status">${heading}</strong>
                ${folder ? `<small title="${escapeHtml(folder)}">${icon('folder', 13)}${escapeHtml(folder)}</small>` : ''}
            </div>
        </div>
        ${rows ? `<ul class="result-list" role="list">${rows}</ul>` : ''}
        <div class="status-actions">
            <button type="button" class="accent-btn" data-role="again">Convert more images</button>
        </div>
    </div>`;
}

function resultRow(result: FileResult, withDetails: boolean): string {
    const ok = result.status === 'completed';
    const name = fileName(result.input);
    const codecAction = !ok && isCodecError(result.error)
        ? ` <button type="button" class="hyperlink-btn row-action" data-role="codec">Install HEIF codec</button>`
        : '';
    const inSize = state.fileSizes[result.input];
    const outSize = state.fileSizes[result.output];
    const sizeText = ok && withDetails && outSize !== undefined && outSize >= 0
        ? `<span class="result-size">${inSize !== undefined && inSize >= 0 ? `${formatSize(inSize)} → ` : ''}${formatSize(outSize)}</span>`
        : '';
    return `
    <li class="result-row ${ok ? 'is-ok' : 'is-failed'}" role="listitem">
        <span class="status-dot">${icon(ok ? 'check' : 'x', 14)}</span>
        <span class="result-name" title="${escapeHtml(result.input)}">${escapeHtml(name)}</span>
        ${ok && withDetails ? `<span class="result-arrow">${icon('convert', 13)}</span><span class="result-output">${escapeHtml(fileName(result.output))}</span>${sizeText}` : ''}
        ${!ok && withDetails ? `<span class="result-error">${escapeHtml(result.error)}${codecAction}</span>` : ''}
    </li>`;
}

function renderSettings(): string {
    return renderShell(`
        <section class="page settings-page">
            <header class="page-header">
                <h1>Settings</h1>
                <p>Presets for the Convert page and Explorer quick actions.</p>
            </header>
            <div class="settings-stack">
                <section class="settings-section">
                    <h2>Conversion</h2>
                    <div class="card-group">
                        ${qualityRow('JPEG quality', 'Preset for JPEG and HEIC output.', 'jpeg-quality', state.settings.jpegQuality)}
                        ${qualityRow('WebP quality', 'Preset for WebP output.', 'webp-quality', state.settings.webpQuality)}
                        ${switchRow('Preserve metadata', 'Keep camera details when the format supports it.', 'preserve-metadata', state.settings.preserveMetadata)}
                    </div>
                </section>
                <section class="settings-section">
                    <h2>App</h2>
                    <div class="card-group">
                        ${switchRow('Start at login', 'Run ConvertMe in the background when you sign in.', 'launch-login', state.settings.launchAtLogin)}
                        ${switchRow('Notifications', 'Show a notification when conversions finish.', 'notify', state.settings.showNotifications)}
                        ${switchRow('Explorer context menu', 'Add Convert image to the right-click menu in File Explorer.', 'explorer', state.explorerOn)}
                    </div>
                </section>
            </div>
            <p class="settings-footer">ConvertMe converts images right from File Explorer.</p>
        </section>`);
}

function qualityRow(title: string, caption: string, role: string, value: number): string {
    return `
    <div class="setting-row">
        <div class="setting-copy">
            <label for="${role}">${title}</label>
            <small>${caption}</small>
        </div>
        <div class="range-control">
            <input id="${role}" class="win-slider" type="range" data-role="${role}" min="1" max="100" value="${value}" aria-label="${title}">
            <output data-role="${role}-output">${value}</output>
        </div>
    </div>`;
}

function switchRow(title: string, caption: string, role: string, checked: boolean): string {
    return `
    <div class="setting-row">
        <div class="setting-copy">
            <label for="${role}">${title}</label>
            <small>${caption}</small>
        </div>
        <input id="${role}" class="toggle-switch" type="checkbox" role="switch" data-role="${role}" aria-label="${title}" ${checked ? 'checked' : ''}>
    </div>`;
}

function renderToast(): string {
    const action = state.toast?.action === 'codec'
        ? `<button type="button" class="hyperlink-btn" data-role="codec">Install HEIF codec</button>`
        : '';
    return `<div class="toast" role="status">${escapeHtml(state.toast?.message ?? '')}${action}</div>`;
}

function syncControls(): void {
    app.querySelectorAll<HTMLSelectElement>('select[data-role="format"]').forEach((el) => {
        el.addEventListener('change', () => {
            state.outputFormat = el.value || state.outputFormat;
            state.quality = qualityFor(currentFormat());
            render();
        });
    });
    app.querySelectorAll<HTMLInputElement>('input[data-role="quality"]').forEach((el) => {
        const output = app.querySelector<HTMLOutputElement>('output[data-role="quality-output"]');
        el.addEventListener('input', () => {
            state.quality = Number(el.value);
            if (output) output.textContent = String(el.value);
        });
    });
    app.querySelectorAll<HTMLInputElement>('input[data-role="metadata"]').forEach((el) => {
        el.addEventListener('change', () => {
            state.settings.preserveMetadata = el.checked;
            void persistSettings();
        });
    });
    bindSettingSlider('jpeg-quality', (value) => (state.settings.jpegQuality = value));
    bindSettingSlider('webp-quality', (value) => (state.settings.webpQuality = value));
    bindSettingSwitch('preserve-metadata', (checked) => (state.settings.preserveMetadata = checked));
    bindSettingSwitch('launch-login', (checked) => (state.settings.launchAtLogin = checked));
    bindSettingSwitch('notify', (checked) => (state.settings.showNotifications = checked));
    app.querySelectorAll<HTMLInputElement>('input[data-role="explorer"]').forEach((el) => {
        el.addEventListener('change', () => {
            void toggleExplorer(el.checked);
        });
    });
    app.querySelectorAll<HTMLElement>('[data-role="dropzone"]').forEach((el) => {
        el.addEventListener('keydown', (event) => {
            if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                void browse();
            }
        });
    });
}

function bindSettingSlider(role: string, commit: (value: number) => void): void {
    app.querySelectorAll<HTMLInputElement>(`input[data-role="${role}"]`).forEach((el) => {
        const output = app.querySelector<HTMLOutputElement>(`output[data-role="${role}-output"]`);
        el.addEventListener('input', () => {
            commit(Number(el.value));
            if (output) output.textContent = String(el.value);
        });
        el.addEventListener('change', () => {
            commit(Number(el.value));
            void persistSettings();
        });
    });
}

function bindSettingSwitch(role: string, commit: (checked: boolean) => void): void {
    app.querySelectorAll<HTMLInputElement>(`input[data-role="${role}"]`).forEach((el) => {
        el.addEventListener('change', () => {
            commit(el.checked);
            void persistSettings();
        });
    });
}

app.addEventListener('click', (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>('[data-role]');
    if (!target) return;
    const path = target.dataset.path ?? '';
    switch (target.dataset.role) {
        case 'nav':
            state.view = (target.dataset.view as View) ?? 'convert';
            render();
            break;
        case 'browse':
        case 'dropzone':
            void browse();
            break;
        case 'remove':
            removeFile(path);
            break;
        case 'clear':
            clearFiles();
            break;
        case 'convert':
            void startConversion();
            break;
        case 'cancel':
            void cancelConversion();
            break;
        case 'again':
            resetAfterDone();
            break;
        case 'codec':
            void OpenCodecStore().catch((error) => showToast(String(error)));
            break;
        case 'quick-close':
        case 'window-quit':
            void Quit();
            break;
        case 'window-hide':
            WindowHide();
            break;
        case 'window-min':
            WindowMinimise();
            break;
    }
});

function addPaths(paths: string[]): void {
    if (state.phase === 'running') return;
    const supported = paths.filter(isSupportedPath);
    const skipped = paths.length - supported.length;
    const existing = new Set(state.files.map((file) => file.path.toLowerCase()));
    for (const path of supported) {
        const key = path.toLowerCase();
        if (existing.has(key)) continue;
        existing.add(key);
        state.files.push({path, name: fileName(path), dir: folderName(path), ext: fileExt(path)});
    }
    state.phase = 'idle';
    state.results = [];
    state.summary = null;
    state.view = 'convert';
    render();
    if (skipped > 0) {
        showToast(`Skipped ${skipped} unsupported file${skipped === 1 ? '' : 's'}.`);
    }
    void loadThumbs();
    void loadSizes();
}

async function loadSizes(): Promise<void> {
    if (!inApp) return;
    const pending = state.files.map((f) => f.path).filter((p) => state.fileSizes[p] === undefined);
    if (!pending.length) return;
    try {
        const sizes = await GetFileSizes(pending);
        pending.forEach((p, i) => {
            state.fileSizes[p] = sizes[i];
        });
        render();
    } catch {
        // Sizes are a nicety; conversion doesn't depend on them.
    }
}

function addDroppedFiles(list: FileList): void {
    const images = Array.from(list).filter((file) => file.type.startsWith('image/'));
    if (!images.length) {
        showToast('No supported image files were found.');
        return;
    }
    for (const file of images) {
        if (state.files.some((existing) => existing.path === file.name)) continue;
        state.files.push({path: file.name, name: file.name, dir: '', ext: fileExt(file.name)});
        state.thumbs[file.name] = URL.createObjectURL(file);
    }
    state.phase = 'idle';
    state.results = [];
    state.summary = null;
    state.view = 'convert';
    render();
}

async function loadThumbs(): Promise<void> {
    const paths = state.files
        .slice(0, 40)
        .map((file) => file.path)
        .filter((path) => !state.thumbs[path]);
    if (!paths.length) return;
    await Promise.all(
        paths.map(async (path) => {
            try {
                state.thumbs[path] = await PreviewFile(path);
            } catch {
                // A preview is optional; conversion can still proceed.
            }
        }),
    );
    app.querySelectorAll<HTMLImageElement>('img[data-thumb]').forEach((img) => {
        const thumb = state.thumbs[img.dataset.thumb ?? ''];
        if (thumb) img.src = thumb;
    });
    app.querySelectorAll<HTMLElement>('[data-thumb-box]').forEach((box) => {
        const thumb = state.thumbs[box.dataset.thumbBox ?? ''];
        if (thumb) {
            box.outerHTML = `<img class="thumb" src="${thumb}" alt="" />`;
        }
    });
}

function removeFile(path: string): void {
    state.files = state.files.filter((file) => file.path !== path);
    render();
}

function clearFiles(): void {
    state.files = [];
    state.thumbs = {};
    render();
}

async function browse(): Promise<void> {
    if (!inApp) {
        showToast('Preview mode — pick files inside the installed app.');
        return;
    }
    try {
        const files = await SelectFiles();
        if (files?.length) {
            addPaths(files);
        }
    } catch (error) {
        showToast(String(error));
    }
}

async function startConversion(): Promise<void> {
    if (!inApp) {
        showToast('Preview mode — conversions run inside the installed app.');
        return;
    }
    const paths = state.files.map((file) => file.path);
    const format = currentFormat();
    state.phase = 'running';
    state.results = [];
    state.summary = {total: paths.length, completed: 0, failed: 0, canceled: 0, results: []};
    render();
    try {
        await StartConversion(paths, {
            format: format.id,
            quality: format.lossy ? state.quality : 0,
            preserveMetadata: state.settings.preserveMetadata,
        });
    } catch (error) {
        state.phase = 'idle';
        render();
        showToast(String(error));
    }
}

async function cancelConversion(): Promise<void> {
    try {
        await CancelConversion();
    } catch (error) {
        showToast(String(error));
    }
}

function resetAfterDone(): void {
    state.files = [];
    state.thumbs = {};
    state.results = [];
    state.summary = null;
    state.phase = 'idle';
    render();
}

async function toggleExplorer(on: boolean): Promise<void> {
    const previous = !on;
    state.settings.explorerIntegration = on;
    state.explorerOn = on;
    try {
        await SaveSettings(state.settings);
    } catch (error) {
        state.settings.explorerIntegration = previous;
        state.explorerOn = previous;
        render();
        showToast(`Explorer integration could not be ${on ? 'enabled' : 'disabled'}: ${String(error)}`);
    }
}

async function persistSettings(): Promise<void> {
    if (!inApp) return;
    try {
        await SaveSettings(state.settings);
    } catch (error) {
        showToast(`Settings could not be saved: ${String(error)}`);
    }
}

function showToast(message: string, action?: 'codec'): void {
    state.toast = {message, action};
    render();
    window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(() => {
        state.toast = null;
        render();
    }, 6000);
}

function onRuntimeEvent(name: string, handler: (...args: any[]) => void): void {
    if (!inApp) return;
    EventsOn(name, handler);
}

function wireRuntimeEvents(): void {
    onRuntimeEvent('view:settings', () => {
        state.view = 'settings';
        render();
    });
    onRuntimeEvent('conversion:started', (summary: ConversionSummary) => {
        state.phase = 'running';
        state.summary = summary;
        state.results = [];
        render();
    });
    onRuntimeEvent('conversion:file', (result: FileResult) => {
        state.results = [...state.results, result];
        render();
    });
    onRuntimeEvent('conversion:complete', (summary: ConversionSummary) => {
        state.phase = 'done';
        state.summary = summary;
        state.results = summary.results ?? state.results;
        if (state.view === 'quick') {
            if (summary.failed === 0 && summary.canceled === 0) {
                window.setTimeout(() => void Quit(), 600);
                return;
            }
            render();
            return;
        }
        render();
        const outputs = state.results.filter((r) => r.status === 'completed').map((r) => r.output);
        if (outputs.length) {
            void GetFileSizes(outputs).then((sizes) => {
                outputs.forEach((p, i) => {
                    state.fileSizes[p] = sizes[i];
                });
                render();
            }).catch(() => {});
        }
        if (state.results.some((result) => result.error && isCodecError(result.error))) {
            showToast('HEIC conversion needs the Microsoft HEIF Image Extensions.', 'codec');
        }
    });
}

function wireFileDrop(): void {
    if (inApp) {
        OnFileDrop((_x: number, _y: number, paths: string[]) => {
            if (paths?.length) {
                addPaths(paths);
            }
        }, true);
    }

    let depth = 0;
    const hasFiles = (event: DragEvent): boolean => Array.from(event.dataTransfer?.types ?? []).includes('Files');
    window.addEventListener('dragenter', (event) => {
        if (inApp || !hasFiles(event)) return;
        event.preventDefault();
        depth += 1;
        document.body.classList.add('is-dropping');
    });
    window.addEventListener('dragover', (event) => {
        if (!hasFiles(event)) return;
        event.preventDefault();
    });
    window.addEventListener('dragleave', () => {
        if (inApp) return;
        depth = Math.max(0, depth - 1);
        if (depth === 0) document.body.classList.remove('is-dropping');
    });
    window.addEventListener('drop', (event) => {
        if (inApp || !event.dataTransfer?.files?.length) return;
        event.preventDefault();
        depth = 0;
        document.body.classList.remove('is-dropping');
        addDroppedFiles(event.dataTransfer.files);
    });
}

async function initialise(): Promise<void> {
    try {
        state.settings = {...defaultSettings, ...(await GetSettings())};
        state.formats = await GetFormats();
        state.quality = qualityFor(currentFormat());
        state.explorerOn = await IsExplorerIntegrationRegistered();
    } catch {
        state.formats = fallbackFormats;
        state.quality = qualityFor(currentFormat());
    }
    wireRuntimeEvents();
    wireFileDrop();
    render();
    try {
        const request = await GetLaunchRequest();
        if (request.mode === 'convert' && request.files?.length && request.format) {
            state.view = 'quick';
            state.quickFormat = request.format;
            state.files = request.files.map((path) => ({path, name: fileName(path), dir: folderName(path), ext: fileExt(path)}));
            state.phase = 'running';
            state.results = [];
            state.summary = {total: request.files.length, completed: 0, failed: 0, canceled: 0, results: []};
            render();
            await ClearLaunchRequest();
            try {
                await StartConversion(request.files, {
                    format: request.format,
                    quality: 0,
                    preserveMetadata: state.settings.preserveMetadata,
                });
            } catch (error) {
                state.phase = 'done';
                state.results = [{input: request.files[0], output: '', status: 'failed', error: String(error)}];
                render();
            }
            return;
        }
        if (request.mode === 'settings') {
            state.view = 'settings';
            render();
        } else if (request.files?.length) {
            addPaths(request.files);
        }
        await ClearLaunchRequest();
    } catch {
        // Browser preview has no launch request.
    }
    void loadThumbs();
}

function applyDebugHook(): void {
    if (inApp) return;
    (window as any).__convertMeDebug = (scenario: string) => {
        const demoFiles = [
            {path: 'C:\\Users\\you\\Pictures\\garden.png', name: 'garden.png', dir: 'C:\\Users\\you\\Pictures', ext: 'png'},
            {path: 'C:\\Users\\you\\Pictures\\sunset.png', name: 'sunset.png', dir: 'C:\\Users\\you\\Pictures', ext: 'png'},
            {path: 'C:\\Users\\you\\Pictures\\portrait.heic', name: 'portrait.heic', dir: 'C:\\Users\\you\\Pictures', ext: 'heic'},
        ];
        const demoSizes: Record<string, number> = {
            [demoFiles[0].path]: 2_410_000,
            [demoFiles[1].path]: 1_180_000,
            [demoFiles[2].path]: 3_640_000,
            'C:\\Users\\you\\Pictures\\garden.jpg': 481_000,
            'C:\\Users\\you\\Pictures\\sunset.jpg': 236_000,
        };
        state.fileSizes = demoSizes;
        if (scenario === 'files') {
            state.files = demoFiles;
            state.phase = 'idle';
            state.results = [];
            state.summary = null;
        } else if (scenario === 'progress') {
            state.files = demoFiles;
            state.phase = 'running';
            state.summary = {total: 3, completed: 0, failed: 0, canceled: 0, results: []};
            state.results = [{input: demoFiles[0].path, output: 'C:\\Users\\you\\Pictures\\garden.jpg', status: 'completed', error: ''}];
        } else if (scenario === 'result') {
            state.files = demoFiles;
            state.phase = 'done';
            state.results = [
                {input: demoFiles[0].path, output: 'C:\\Users\\you\\Pictures\\garden.jpg', status: 'completed', error: ''},
                {input: demoFiles[1].path, output: 'C:\\Users\\you\\Pictures\\sunset.jpg', status: 'completed', error: ''},
                {input: demoFiles[2].path, output: '', status: 'failed', error: 'HEIF codec not available'},
            ];
            state.summary = {total: 3, completed: 2, failed: 1, canceled: 0, results: state.results};
        }
        state.view = 'convert';
        render();
    };
}

void initialise();
applyDebugHook();
