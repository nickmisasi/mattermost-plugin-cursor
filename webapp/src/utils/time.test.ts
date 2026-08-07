import {formatDuration, relativeTime, toTimestamp} from './time';

const NOW = Date.parse('2024-05-01T12:00:00.000Z');

function ago(ms: number): string {
    return new Date(NOW - ms).toISOString();
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

describe('toTimestamp', () => {
    it('parses ISO strings and passes numbers through', () => {
        expect(toTimestamp('2024-05-01T12:00:00.000Z')).toBe(NOW);
        expect(toTimestamp(NOW)).toBe(NOW);
    });

    it('returns 0 for missing or unparseable values', () => {
        expect(toTimestamp(undefined)).toBe(0);
        expect(toTimestamp(null)).toBe(0);
        expect(toTimestamp('')).toBe(0);
        expect(toTimestamp('not a date')).toBe(0);
        expect(toTimestamp(Number.NaN)).toBe(0);
    });
});

describe('relativeTime', () => {
    it('returns an empty string when there is no usable timestamp', () => {
        expect(relativeTime(undefined, NOW)).toBe('');
        expect(relativeTime('nonsense', NOW)).toBe('');
    });

    it('uses short units that step up with the elapsed time', () => {
        expect(relativeTime(ago(2 * SECOND), NOW)).toBe('now');
        expect(relativeTime(ago(12 * SECOND), NOW)).toBe('12s');
        expect(relativeTime(ago(4 * MINUTE), NOW)).toBe('4m');
        expect(relativeTime(ago(9 * HOUR), NOW)).toBe('9h');
        expect(relativeTime(ago(3 * DAY), NOW)).toBe('3d');
        expect(relativeTime(ago(20 * DAY), NOW)).toBe('2w');
        expect(relativeTime(ago(800 * DAY), NOW)).toBe('2y');
    });

    it('rounds down at unit boundaries', () => {
        expect(relativeTime(ago(MINUTE - 1), NOW)).toBe('59s');
        expect(relativeTime(ago(HOUR - 1), NOW)).toBe('59m');
        expect(relativeTime(ago(DAY - 1), NOW)).toBe('23h');
    });

    it('treats future timestamps as now', () => {
        expect(relativeTime(new Date(NOW + HOUR).toISOString(), NOW)).toBe('now');
    });
});

describe('formatDuration', () => {
    it('formats sub-minute, sub-hour and longer durations', () => {
        expect(formatDuration(12_357)).toBe('12s');
        expect(formatDuration(5 * MINUTE)).toBe('5m 0s');
        expect(formatDuration((2 * HOUR) + (30 * MINUTE))).toBe('2h 30m');
    });

    it('returns an empty string for missing or invalid durations', () => {
        expect(formatDuration(undefined)).toBe('');
        expect(formatDuration(0)).toBe('');
        expect(formatDuration(-5)).toBe('');
    });
});
