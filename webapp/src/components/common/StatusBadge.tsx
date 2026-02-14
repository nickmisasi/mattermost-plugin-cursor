import React from 'react';

import type {AgentStatus} from '../../types';

interface Props {
    status: AgentStatus;
}

const STATUS_CLASS_MAP: Record<AgentStatus, string> = {
    CREATING: 'cursor-status-creating',
    RUNNING: 'cursor-status-running',
    FINISHED: 'cursor-status-finished',
    FAILED: 'cursor-status-failed',
    STOPPED: 'cursor-status-stopped',
};

const STATUS_LABEL_MAP: Record<AgentStatus, string> = {
    CREATING: 'Creating',
    RUNNING: 'Running',
    FINISHED: 'Finished',
    FAILED: 'Failed',
    STOPPED: 'Stopped',
};

const StatusBadge: React.FC<Props> = ({status}) => {
    const className = STATUS_CLASS_MAP[status] || 'cursor-status-finished';
    const label = STATUS_LABEL_MAP[status] || status;
    return (
        <span className='cursor-status-badge-wrapper'>
            <span className={`cursor-status-badge ${className}`}/>
            <span className='cursor-status-tooltip'>{label}</span>
        </span>
    );
};

export default StatusBadge;
