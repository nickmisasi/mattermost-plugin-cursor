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

const StatusBadge: React.FC<Props> = ({status}) => {
    const className = STATUS_CLASS_MAP[status] || 'cursor-status-finished';
    return (
        <span
            className={`cursor-status-badge ${className}`}
            title={status}
        />
    );
};

export default StatusBadge;
