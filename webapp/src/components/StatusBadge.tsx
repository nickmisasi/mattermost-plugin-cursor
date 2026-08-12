import React from 'react';

import type {RunStatus} from '../types';
import {ARCHIVED_LABEL, statusLabel, statusVariant} from '../utils/status';

interface Props {
    status?: RunStatus;
    archived?: boolean;
}

/**
 * Archived is a separate chip rather than a status value: an archived agent
 * still has a last run, and collapsing the two made a cancelled run read as
 * "Archived".
 */
const StatusBadge = ({status, archived = false}: Props) => (
    <span className='cursor-status'>
        <span className={`cursor-status-badge cursor-status-badge--${statusVariant(status)}`}>
            {statusLabel(status)}
        </span>
        {archived ? (
            <span className='cursor-status-badge cursor-status-badge--archived'>{ARCHIVED_LABEL}</span>
        ) : null}
    </span>
);

export default StatusBadge;
