import React from 'react';

import type {RunStatus} from '../types';
import {statusLabel, statusVariant} from '../utils/status';

interface Props {
    status?: RunStatus;
    archived?: boolean;
}

const StatusBadge = ({status, archived = false}: Props) => {
    const variant = statusVariant(status, archived);

    return (
        <span className={`cursor-status-badge cursor-status-badge--${variant}`}>
            {statusLabel(status, archived)}
        </span>
    );
};

export default StatusBadge;
