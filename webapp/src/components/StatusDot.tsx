import React from 'react';

import type {RunStatus} from '../types';
import {ARCHIVED_LABEL, statusLabel, statusVariant} from '../utils/status';

interface Props {
    status?: RunStatus;
    archived?: boolean;
}

const StatusDot = ({status, archived = false}: Props) => {
    const variant = statusVariant(status, archived);
    const label = archived ? `${statusLabel(status)} · ${ARCHIVED_LABEL}` : statusLabel(status);

    return (
        <span
            className={`cursor-status-dot cursor-status-dot--${variant}`}
            title={label}
            aria-label={label}
            role='img'
        />
    );
};

export default StatusDot;
