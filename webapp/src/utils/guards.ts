import type {RunStatus} from '../types';
import {RUN_STATUSES} from '../types';

export function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function asArray(value: unknown): unknown[] {
    return Array.isArray(value) ? value : [];
}

export function asString(value: unknown): string {
    return typeof value === 'string' ? value : '';
}

export function asRunStatus(value: unknown): RunStatus | undefined {
    const candidate = asString(value).toUpperCase() as RunStatus;
    return RUN_STATUSES.includes(candidate) ? candidate : undefined;
}
