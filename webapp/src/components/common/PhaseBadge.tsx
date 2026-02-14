import React from 'react';

import type {WorkflowPhase} from '../../types';

interface Props {
    phase: WorkflowPhase;
}

const PHASE_CONFIG: Record<WorkflowPhase, {label: string; className: string}> = {
    context_review: {label: 'Awaiting Review', className: 'cursor-phase-review'},
    planning: {label: 'Planning...', className: 'cursor-phase-planning'},
    plan_review: {label: 'Plan Ready', className: 'cursor-phase-review'},
    implementing: {label: 'Implementing', className: 'cursor-phase-implementing'},
    rejected: {label: 'Rejected', className: 'cursor-phase-rejected'},
    complete: {label: 'Complete', className: 'cursor-phase-complete'},
};

const PhaseBadge: React.FC<Props> = ({phase}) => {
    const config = PHASE_CONFIG[phase] || PHASE_CONFIG.complete;
    return (
        <span className={`cursor-phase-badge ${config.className}`}>
            {config.label}
        </span>
    );
};

export default PhaseBadge;
