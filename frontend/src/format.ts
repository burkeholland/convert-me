import type { Item, Kind, Snapshot } from './types.ts';

export function fileSize(bytes: number): string {
  const safe = Number.isFinite(bytes) ? Math.max(0, bytes) : 0;
  // One decimal is enough, and "182 MB" reads better than "182.0 MB".
  const short = (value: number) => value.toFixed(value >= 100 ? 0 : 1).replace(/\.0$/, '');
  if (safe < 1024) return `${safe} B`;
  if (safe < 1024 ** 2) return `${short(safe / 1024)} KB`;
  if (safe < 1024 ** 3) return `${short(safe / 1024 ** 2)} MB`;
  return `${(safe / 1024 ** 3).toFixed(2).replace(/0$/, '').replace(/\.0$/, '')} GB`;
}

// Reading format: hours only appear when the recording is actually that long.
export function clock(milliseconds: number): string {
  const seconds = Math.round(Math.max(0, Number.isFinite(milliseconds) ? milliseconds : 0) / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const pad = (value: number) => String(value).padStart(2, '0');
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds % 60)}` : `${minutes}:${pad(seconds % 60)}`;
}

// The short side decides the label, so a phone video held upright still reads "1080p".
export function resolution(width: number, height: number): string {
  const short = Math.min(width, height);
  if (!Number.isFinite(short) || short <= 0) return '';
  if (short >= 2000) return '4K';
  return `${Math.round(short)}p`;
}

export function plural(count: number, one: string, many = `${one}s`): string {
  return `${count} ${count === 1 ? one : many}`;
}

export function kindWord(kind: Kind, count: number): string {
  if (kind === 'image') return count === 1 ? 'image' : 'images';
  if (kind === 'video') return count === 1 ? 'video' : 'videos';
  return count === 1 ? 'audio file' : 'audio files';
}

export function itemMeta(item: Item): string {
  const parts = [fileSize(item.size)];
  if (item.kind === 'image' && item.width > 0) parts.push(`${item.width} x ${item.height}`);
  if (item.kind !== 'image' && item.durationMs > 0) parts.push(clock(item.durationMs));
  if (item.kind === 'video' && item.height > 0) parts.push(resolution(item.width, item.height));
  return parts.filter(Boolean).join(' · ');
}

// How the size changed, in words a person would use.
export function sizeChange(before: number, after: number): string {
  if (!(before > 0) || !(after > 0)) return '';
  const ratio = after / before;
  if (ratio < 0.95) return `${Math.round((1 - ratio) * 100)}% smaller`;
  if (ratio > 1.05) return ratio >= 2 ? `${ratio.toFixed(1)}x larger` : `${Math.round((ratio - 1) * 100)}% larger`;
  return 'about the same size';
}

export function isBusy(snapshot: Snapshot | null): boolean {
  return snapshot?.batch.state === 'running' || snapshot?.batch.state === 'cancelling';
}

export function isConvertible(item: Item): boolean {
  return item.status === 'ready' || item.status === 'failed' || item.status === 'cancelled';
}

// The last one or two parts of a folder path, enough to recognise it.
export function folderName(folder: string): string {
  const parts = folder.replace(/[\\/]+$/, '').split(/[\\/]/).filter(Boolean);
  if (parts.length === 0) return folder;
  if (parts.length === 1) return `${parts[0]}\\`;
  return parts[parts.length - 1];
}

export function folderLabel(folder: string): string {
  const name = folderName(folder);
  return name === folder || name === `${folder}\\` ? folder : `${name} (${folder})`;
}

export interface Summary {
  title: string;
  detail: string;
}

// One line that always says what state the list is in.
export function summarize(snapshot: Snapshot): Summary {
  const { items, batch } = snapshot;
  if (batch.state === 'running' || batch.state === 'cancelling') {
    const settled = batch.done + batch.failed + batch.skipped + batch.cancelled;
    const current = items.find(item => item.id === batch.currentId);
    return {
      title: batch.state === 'cancelling' ? 'Cancelling' : `Converting ${Math.min(batch.total, settled + 1)} of ${batch.total}`,
      detail: current ? current.name : '',
    };
  }
  if (batch.state === 'finished') {
    const notes: string[] = [];
    if (batch.failed) notes.push(`${batch.failed} failed`);
    if (batch.skipped) notes.push(`${batch.skipped} skipped`);
    if (batch.cancelled) notes.push(`${batch.cancelled} cancelled`);
    if (batch.done > 0) {
      notes.push(snapshot.destination.mode === 'folder'
        ? `Saved to ${folderName(snapshot.destination.folder)}`
        : 'Saved next to the originals');
    }
    return {
      title: batch.done === batch.total
        ? `${plural(batch.done, 'file')} converted`
        : `${batch.done} of ${plural(batch.total, 'file')} converted`,
      detail: notes.join(' · '),
    };
  }
  const notes = snapshot.kinds.map(kind => `${kind.count} ${kindWord(kind.kind, kind.count)}`);
  const checking = items.filter(item => item.status === 'checking').length;
  const unusable = items.filter(item => item.status === 'unsupported').length;
  const skipped = items.filter(item => item.status === 'skipped').length;
  if (skipped > 0 && checking === 0 && !items.some(item => isConvertible(item) || item.status === 'done')) {
    // Without this line the Convert button would be switched off with no explanation.
    return { title: 'Nothing to convert', detail: 'Pick another format below to convert these files' };
  }
  if (checking) notes.push(`checking ${checking}`);
  if (skipped) notes.push(`${skipped} will be skipped`);
  if (unusable) notes.push(`${unusable} cannot be converted`);
  return { title: plural(items.length, 'file'), detail: notes.join(' · ') };
}

export interface Action {
  kind: 'convert' | 'cancel' | 'open' | 'none';
  label: string;
  enabled: boolean;
}

// The one main button. It is always the next sensible step.
export function primaryAction(snapshot: Snapshot): Action {
  const { items, batch } = snapshot;
  if (batch.state === 'running') return { kind: 'cancel', label: 'Cancel', enabled: true };
  if (batch.state === 'cancelling') return { kind: 'cancel', label: 'Cancelling', enabled: false };
  const waiting = items.filter(isConvertible);
  if (waiting.length > 0) {
    const fresh = waiting.some(item => item.status === 'ready');
    const checking = items.some(item => item.status === 'checking');
    return {
      kind: 'convert',
      label: fresh ? `Convert ${plural(waiting.length, 'file')}` : waiting.length === 1 ? 'Try again' : `Try ${waiting.length} again`,
      enabled: snapshot.state === 'ready' && !checking,
    };
  }
  if (items.some(item => item.status === 'done')) return { kind: 'open', label: 'Open folder', enabled: true };
  return { kind: 'none', label: 'Convert', enabled: false };
}

// Progress of the whole batch, counting the running file by how far it is.
export function overallProgress(snapshot: Snapshot): number {
  const { batch, items } = snapshot;
  if (batch.total <= 0) return 0;
  const settled = batch.done + batch.failed + batch.skipped + batch.cancelled;
  const current = items.find(item => item.id === batch.currentId && item.status === 'converting');
  const partial = current ? Math.max(0, Math.min(100, current.progress)) / 100 : 0;
  return Math.max(0, Math.min(100, ((settled + partial) / batch.total) * 100));
}

export function errorText(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string' && error) return error;
  return 'Something went wrong. Try again, or restart the app.';
}

// Go errors start in lower case. Shown to a person they should read as a sentence.
export function asSentence(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) return trimmed;
  const capital = trimmed[0].toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(capital) ? capital : `${capital}.`;
}
