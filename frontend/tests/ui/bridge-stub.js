// A stand-in for the Go backend, used by the UI tests and by design captures.
// It keeps the same contract as app.go and a small state machine, so the real interface
// code runs unchanged. Pick a scenario with window.__scenario before this script loads.
(() => {
  const scenario = window.__scenario || 'empty';
  const MB = 1024 * 1024;

  const options = {
    image: ['jpg:JPG', 'png:PNG', 'webp:WebP', 'gif:GIF', 'bmp:BMP', 'tiff:TIFF'],
    audio: ['mp3:MP3', 'm4a:M4A', 'wav:WAV', 'flac:FLAC'],
    video: ['mp4:MP4', 'mov:MOV', 'mkv:MKV', 'gif:GIF', 'mp3:MP3:Audio only', 'm4a:M4A:Audio only', 'wav:WAV:Audio only', 'flac:FLAC:Audio only'],
  };
  const labels = { image: 'Images', video: 'Video', audio: 'Audio' };
  const extensions = { jpg: '.jpg', png: '.png', webp: '.webp', gif: '.gif', bmp: '.bmp', tiff: '.tiff', mp3: '.mp3', m4a: '.m4a', wav: '.wav', flac: '.flac', mp4: '.mp4', mov: '.mov', mkv: '.mkv' };
  const targetLabels = { jpg: 'JPG', png: 'PNG', webp: 'WebP', gif: 'GIF', bmp: 'BMP', tiff: 'TIFF', mp3: 'MP3', m4a: 'M4A', wav: 'WAV', flac: 'FLAC', mp4: 'MP4', mov: 'MOV', mkv: 'MKV' };
  const h264Missing = scenario === 'no-h264';
  const h264Reason = 'MP4, MOV and MKV need the H.264 encoder that comes with Windows, and it did not start on this PC. Windows N editions need the Media Feature Pack.';

  const targets = { image: 'jpg', audio: 'mp3', video: h264Missing ? 'gif' : 'mp4' };
  const destination = { mode: 'source', folder: '' };
  if (scenario === 'folder') { destination.mode = 'folder'; destination.folder = 'D:\\Media\\Exports'; }

  let counter = 0;
  function file(name, kind, source, size, extra = {}) {
    counter += 1;
    const item = {
      id: `item-${counter}`, path: `C:\\Users\\burke\\Pictures\\Trip\\${name}`, name,
      folder: 'C:\\Users\\burke\\Pictures\\Trip', size, kind, status: 'ready', source,
      width: 0, height: 0, durationMs: 0, target: '', targetLabel: '', progress: 0,
      outputPath: '', outputName: '', outputSize: 0, error: '', detail: '', warning: '', thumb: kind !== 'audio',
      ...extra,
    };
    assign(item);
    return item;
  }
  function assign(item) {
    if (!item.kind) return;
    item.target = targets[item.kind];
    item.targetLabel = targetLabels[item.target];
    // Like the backend: a file that is already in the chosen format is skipped up front.
    if (item.status === 'ready' && item.source === item.targetLabel) {
      item.status = 'skipped';
      item.error = `Already ${item.targetLabel}, so there is nothing to convert.`;
    }
  }

  const samples = () => [
    file('beach-sunset.png', 'image', 'PNG', Math.round(2.4 * MB), { width: 4032, height: 3024 }),
    file('product-shot.webp', 'image', 'WebP', 412 * 1024, { width: 1600, height: 1200 }),
    file('interview-final.mov', 'video', 'MOV', 182 * MB, { width: 1920, height: 1080, durationMs: 724000 }),
    file('voice-memo.m4a', 'audio', 'M4A', Math.round(3.1 * MB), { durationMs: 167000 }),
  ];

  let items = [];
  let batch = { state: 'idle', total: 0, done: 0, failed: 0, skipped: 0, cancelled: 0, currentId: '' };
  let revision = 1;
  let notice = '';
  let noticeError = false;
  let noticeSeq = 0;
  const calls = [];

  function finish(item, ratio = 0.34) {
    const stem = item.name.replace(/\.[^.]+$/, '');
    item.status = 'done';
    item.progress = 100;
    item.outputName = stem + extensions[item.target];
    item.outputPath = `${destination.mode === 'folder' ? destination.folder : item.folder}\\${item.outputName}`;
    item.outputSize = Math.round(item.size * ratio);
  }

  if (['ready', 'conflict', 'folder', 'no-h264', 'slow-start'].includes(scenario)) items = samples();
  if (scenario === 'checking') {
    items = samples();
    items[2].status = 'checking';
    items[2].kind = '';
    items[2].thumb = false;
    items[3].status = 'checking';
    items[3].kind = '';
  }
  if (scenario === 'converting') {
    items = samples();
    finish(items[0], 0.31);
    finish(items[1], 1.9);
    items[1].target = 'jpg';
    items[2].status = 'converting';
    items[2].progress = 42;
    items[3].status = 'queued';
    batch = { state: 'running', total: 4, done: 2, failed: 0, skipped: 0, cancelled: 0, currentId: items[2].id };
  }
  if (scenario === 'done') {
    items = samples();
    finish(items[0], 0.31);
    finish(items[1], 1.9);
    finish(items[2], 0.46);
    finish(items[3], 1.24);
    batch = { state: 'finished', total: 4, done: 4, failed: 0, skipped: 0, cancelled: 0, currentId: '' };
  }
  if (scenario === 'problems') {
    items = samples();
    finish(items[0], 0.31);
    items[1].status = 'failed';
    items[1].error = 'This file looks damaged or incomplete.';
    items[1].detail = '[webp @ 000001d2c41b5e40] image data not found\n[in#0 @ 000001d2c41b2a00] Error opening input: Invalid data found when processing input';
    items[2].warning = 'This is HDR video. Colors can look flat after converting, because HDR is not supported yet.';
    finish(items[2], 0.46);
    finish(items[3], 1.24);
    items.push(file('drone-footage.mp4', 'video', 'MP4', 96 * MB, {
      status: 'unsupported', thumb: false, target: '', targetLabel: '',
      error: 'This file uses AV1, which Convert Me cannot read yet.',
    }));
    items.push(file('screen-recording.mkv', 'video', 'MKV', 44 * MB, { width: 2560, height: 1440, durationMs: 95000 }));
    items[5].status = 'failed';
    items[5].error = 'The Windows H.264 encoder could not handle this video.';
    items[5].detail = '[h264_mf @ 0000023ddf4f0800] could not set output type (MF_E_INVALIDMEDIATYPE)';
    items.push(file('logo.jpg', 'image', 'JPG', 96 * 1024, { width: 512, height: 512 }));
    batch = { state: 'finished', total: 5, done: 3, failed: 2, skipped: 0, cancelled: 0, currentId: '' };
  }
  if (scenario === 'nothing') {
    items = [
      file('logo.jpg', 'image', 'JPG', 96 * 1024, { width: 512, height: 512 }),
      file('IMG_2041.jpg', 'image', 'JPG', Math.round(3.8 * MB), { width: 4032, height: 3024 }),
    ];
  }
  // More files added after a finished run: the longest status line the window has to fit.
  if (scenario === 'mixed') {
    items = samples();
    finish(items[0], 0.31);
    finish(items[1], 1.9);
    finish(items[2], 0.46);
    finish(items[3], 1.24);
    items.push(file('drone-footage.mp4', 'video', 'MP4', 96 * MB, {
      status: 'unsupported', thumb: false, error: 'This file uses AV1, which Convert Me cannot read yet.',
    }));
    items.push(file('damaged-download.mp4', '', 'MP4', 13 * 1024, {
      status: 'unsupported', thumb: false,
      error: 'Convert Me could not read this file. It may be damaged, or in a format that is not supported yet.',
      detail: '[mov,mp4,m4a,3gp,3g2,mj2 @ 000001d2c41b5e40] moov atom not found',
    }));
    items.push(file('IMG_2041.jpg', 'image', 'JPG', Math.round(3.8 * MB), { width: 3024, height: 4032 }));
    items.push(file('logo-draft.tiff', 'image', 'TIFF', 75 * 1024, { width: 1200, height: 900 }));
  }

  function kinds() {
    const counts = {};
    for (const item of items) if (item.kind && item.status !== 'unsupported') counts[item.kind] = (counts[item.kind] || 0) + 1;
    return ['image', 'video', 'audio'].filter(kind => counts[kind]).map(kind => ({
      kind, label: labels[kind], count: counts[kind], target: targets[kind],
      options: options[kind].map(entry => {
        const [id, label, group = ''] = entry.split(':');
        const blocked = h264Missing && ['mp4', 'mov', 'mkv'].includes(id);
        return { id, label, group, available: !blocked, reason: blocked ? h264Reason : '' };
      }),
    }));
  }

  function snapshot() {
    return structuredClone({
      version: '0.2.0', engine: 'FFmpeg 8.1.3',
      state: scenario === 'starting' ? 'starting' : scenario === 'setup-error' ? 'failed' : 'ready',
      setupError: scenario === 'setup-error'
        ? 'The conversion engine is missing or damaged (runtime/ffmpeg/bin/ffmpeg.exe: the file is missing). Extract the complete Convert Me download again.'
        : '',
      items, kinds: kinds(), destination, batch,
      formats: [
        { kind: 'image', label: 'Images', reads: ['JPG', 'PNG', 'WebP', 'GIF', 'BMP', 'TIFF', 'HEIC'], writes: ['JPG', 'PNG', 'WebP', 'GIF', 'BMP', 'TIFF'] },
        { kind: 'video', label: 'Video', reads: ['MP4', 'MOV', 'MKV', 'WebM', 'AVI', 'WMV', 'MPG', 'TS'], writes: ['MP4', 'MOV', 'MKV', 'GIF'] },
        { kind: 'audio', label: 'Audio', reads: ['MP3', 'WAV', 'FLAC', 'M4A', 'AAC', 'OGG', 'Opus', 'WMA', 'AIFF'], writes: ['MP3', 'M4A', 'WAV', 'FLAC'] },
      ],
      revision, notice, noticeError, noticeSeq,
    });
  }

  function busy() {
    return batch.state === 'running' || batch.state === 'cancelling';
  }
  function touch() { revision += 1; }
  function reject(message) { return Promise.reject(message); }

  function add(paths) {
    const result = { added: 0, skipped: [], message: '' };
    for (const path of paths) {
      const name = path.split(/[\\/]/).pop();
      const extension = (name.match(/\.[^.]+$/) || [''])[0].toLowerCase();
      const kind = ['.png', '.jpg', '.jpeg', '.webp', '.gif', '.bmp', '.tiff', '.heic'].includes(extension) ? 'image'
        : ['.mp4', '.mov', '.mkv', '.webm', '.avi'].includes(extension) ? 'video'
        : ['.mp3', '.wav', '.flac', '.m4a', '.ogg'].includes(extension) ? 'audio' : '';
      if (!kind) {
        result.skipped.push({ name, reason: 'not an image, audio or video type that Convert Me reads' });
        continue;
      }
      const item = file(name, kind, extension.slice(1).toUpperCase(), 5 * MB, { path, thumb: false });
      items.push(item);
      result.added += 1;
    }
    if (result.skipped.length === 1) result.message = `Skipped ${result.skipped[0].name}: ${result.skipped[0].reason}.`;
    if (result.skipped.length > 1) result.message = `Skipped ${result.skipped.length} files (${result.skipped[0].reason}): ${result.skipped.map(entry => entry.name).join(', ')}.`;
    if (result.added) batch = { ...batch, state: 'idle' };
    touch();
    return result;
  }

  const controls = {
    calls,
    connectionError: false,
    snapshot,
    // Move the running file forward. At 100 it finishes and the next one starts.
    advance(percent) {
      const current = items.find(item => item.id === batch.currentId);
      if (!current) return;
      current.progress = percent;
      if (percent >= 100) controls.finishCurrent();
      touch();
    },
    finishCurrent(failure = '') {
      const current = items.find(item => item.id === batch.currentId);
      if (!current) return;
      if (failure) {
        current.status = 'failed';
        current.error = failure;
        current.progress = 0;
        batch.failed += 1;
      } else {
        finish(current);
        batch.done += 1;
      }
      const next = items.find(item => item.status === 'queued');
      if (next) {
        next.status = 'converting';
        batch.currentId = next.id;
      } else {
        batch.state = 'finished';
        batch.currentId = '';
      }
      touch();
    },
    finishAll() {
      while (batch.state === 'running') controls.finishCurrent();
    },
    announce(text, isError = false) {
      notice = text;
      noticeError = isError;
      noticeSeq += 1;
      touch();
    },
    drop(paths) { dropHandler?.(0, 0, paths); },
  };
  let dropHandler;
  Object.assign(window, { __stub: controls });
  window.runtime = {
    OnFileDrop: callback => { dropHandler = callback; },
    OnFileDropOff: () => { dropHandler = undefined; },
  };

  window.go = { main: { App: {
    Status: async () => {
      if (controls.connectionError) throw new Error('Native bridge unavailable');
      if (scenario === 'slow-start') await new Promise(resolve => setTimeout(resolve, 30));
      return snapshot();
    },
    AddFiles: async paths => {
      calls.push(`add:${paths.join('|')}`);
      if (busy()) return reject('wait for the current conversion to finish before adding files');
      return add(paths);
    },
    ChooseFiles: async () => {
      calls.push('choose');
      if (scenario === 'cancel-choose') return { added: 0, skipped: [], message: '' };
      const before = items.length;
      items.push(...samples());
      batch = { ...batch, state: 'idle' };
      touch();
      return { added: items.length - before, skipped: [], message: '' };
    },
    RemoveItem: async id => {
      calls.push(`remove:${id}`);
      items = items.filter(item => item.id !== id);
      batch = { ...batch, state: 'idle' };
      touch();
    },
    ClearItems: async () => {
      calls.push('clear');
      items = [];
      batch = { state: 'idle', total: 0, done: 0, failed: 0, skipped: 0, cancelled: 0, currentId: '' };
      touch();
    },
    SetTarget: async (kind, target) => {
      calls.push(`target:${kind}:${target}`);
      targets[kind] = target;
      for (const item of items) {
        if (item.kind !== kind || !['ready', 'done', 'failed', 'cancelled', 'skipped'].includes(item.status)) continue;
        Object.assign(item, { status: 'ready', progress: 0, outputName: '', outputPath: '', outputSize: 0, error: '', detail: '' });
        assign(item);
      }
      batch = { ...batch, state: 'idle' };
      touch();
    },
    ChooseDestination: async () => {
      calls.push('choose-destination');
      if (scenario === 'cancel-choose') return false;
      destination.mode = 'folder';
      destination.folder = 'D:\\Media\\Exports';
      touch();
      return true;
    },
    UseSourceFolder: async () => { calls.push('use-source'); destination.mode = 'source'; touch(); },
    UseChosenFolder: async () => { calls.push('use-folder'); destination.mode = 'folder'; touch(); },
    Start: async policy => {
      calls.push(`start:${policy}`);
      const queue = items.filter(item => ['ready', 'failed', 'cancelled'].includes(item.status));
      if (queue.length === 0) {
        const skipped = items.filter(item => item.status === 'skipped');
        if (skipped.length === 1) return reject(skipped[0].error);
        if (skipped.length > 1) return reject('These files are already in the chosen format. Pick another format to convert them.');
        return reject('there is nothing to convert');
      }
      if (scenario === 'conflict' && !policy) {
        return { started: false, conflicts: [
          { itemId: queue[0].id, name: 'beach-sunset.jpg', folder: queue[0].folder },
          { itemId: queue[1].id, name: 'product-shot.jpg', folder: queue[1].folder },
        ] };
      }
      for (const item of queue) Object.assign(item, { status: 'queued', progress: 0, error: '', detail: '' });
      queue[0].status = 'converting';
      batch = { state: 'running', total: queue.length, done: 0, failed: 0, skipped: 0, cancelled: 0, currentId: queue[0].id };
      touch();
      return { started: true, conflicts: [] };
    },
    Cancel: async () => {
      calls.push('cancel');
      for (const item of items) {
        if (item.status === 'converting' || item.status === 'queued') {
          item.status = 'cancelled';
          item.progress = 0;
          batch.cancelled += 1;
        }
      }
      batch.state = 'finished';
      batch.currentId = '';
      touch();
    },
    OpenOutputFolder: async () => { calls.push('open-folder'); },
    RevealItem: async id => { calls.push(`reveal:${id}`); },
    OpenLicenses: async () => { calls.push('licenses'); },
    MinimiseWindow: async () => { calls.push('minimise'); },
    ToggleMaximiseWindow: async () => { calls.push('maximise'); },
    CloseWindow: async () => { calls.push('close'); },
  } } };
})();
