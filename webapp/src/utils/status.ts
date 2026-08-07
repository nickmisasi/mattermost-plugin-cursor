import type {RunStatus} from '../types';

export type StatusVariant = 'active' | 'finished' | 'error' | 'muted' | 'archived';

export function statusVariant(status: RunStatus | undefined, archived = false): StatusVariant {
    if (archived) {
        return 'archived';
    }
    switch (status) {
    case 'QUEUED':
    case 'CREATING':
    case 'RUNNING':
        return 'active';
    case 'FINISHED':
        return 'finished';
    case 'ERROR':
        return 'error';
    default:
        return 'muted';
    }
}

export function statusLabel(status: RunStatus | undefined, archived = false): string {
    if (archived) {
        return 'Archived';
    }
    switch (status) {
    case 'QUEUED':
        return 'Queued';
    case 'CREATING':
        return 'Starting';
    case 'RUNNING':
        return 'Running';
    case 'FINISHED':
        return 'Finished';
    case 'ERROR':
        return 'Failed';
    case 'CANCELLED':
        return 'Cancelled';
    case 'EXPIRED':
        return 'Expired';
    default:
        return 'No runs yet';
    }
}
