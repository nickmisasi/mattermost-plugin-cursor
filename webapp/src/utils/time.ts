const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const YEAR = 365 * DAY;

export function toTimestamp(value?: string | number | null): number {
    if (typeof value === 'number') {
        return Number.isFinite(value) ? value : 0;
    }
    if (typeof value !== 'string' || !value) {
        return 0;
    }

    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? 0 : parsed;
}

/**
 * Short relative time in the style of the Cursor agents sidebar: "12s", "4m",
 * "9h", "3d", "2w", "1y". Returns an empty string for unparseable input.
 */
export function relativeTime(value?: string | number | null, now: number = Date.now()): string {
    const timestamp = toTimestamp(value);
    if (!timestamp) {
        return '';
    }

    const diff = now - timestamp;
    if (diff < 5 * SECOND) {
        return 'now';
    }
    if (diff < MINUTE) {
        return `${Math.floor(diff / SECOND)}s`;
    }
    if (diff < HOUR) {
        return `${Math.floor(diff / MINUTE)}m`;
    }
    if (diff < DAY) {
        return `${Math.floor(diff / HOUR)}h`;
    }
    if (diff < WEEK) {
        return `${Math.floor(diff / DAY)}d`;
    }
    if (diff < YEAR) {
        return `${Math.floor(diff / WEEK)}w`;
    }
    return `${Math.floor(diff / YEAR)}y`;
}

export function formatDateTime(value?: string | number | null): string {
    const timestamp = toTimestamp(value);
    if (!timestamp) {
        return '';
    }
    return new Date(timestamp).toLocaleString();
}

export function formatDuration(durationMs?: number): string {
    if (typeof durationMs !== 'number' || !Number.isFinite(durationMs) || durationMs <= 0) {
        return '';
    }
    if (durationMs < MINUTE) {
        return `${Math.max(1, Math.round(durationMs / SECOND))}s`;
    }
    if (durationMs < HOUR) {
        return `${Math.floor(durationMs / MINUTE)}m ${Math.round((durationMs % MINUTE) / SECOND)}s`;
    }
    return `${Math.floor(durationMs / HOUR)}h ${Math.round((durationMs % HOUR) / MINUTE)}m`;
}
