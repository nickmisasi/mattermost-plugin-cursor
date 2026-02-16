import React from 'react';

import type {ReviewLoopPhase, WorkflowPhase} from '../../types';

interface Props {
    phase: WorkflowPhase | ReviewLoopPhase;
}

const PHASE_CONFIG: Record<string, {label: string; className: string}> = {

    // HITL workflow phases
    context_review: {label: 'Awaiting Review', className: 'cursor-phase-review'},
    planning: {label: 'Planning...', className: 'cursor-phase-planning'},
    plan_review: {label: 'Plan Ready', className: 'cursor-phase-review'},
    implementing: {label: 'Implementing', className: 'cursor-phase-implementing'},
    rejected: {label: 'Rejected', className: 'cursor-phase-rejected'},
    complete: {label: 'Complete', className: 'cursor-phase-complete'},

    // Review loop phases
    requesting_review: {label: 'Requesting Review', className: 'cursor-phase-rl-requesting'},
    awaiting_review: {label: 'AI Reviewing', className: 'cursor-phase-rl-awaiting'},
    cursor_fixing: {label: 'Cursor Fixing', className: 'cursor-phase-rl-fixing'},
    approved: {label: 'AI Approved', className: 'cursor-phase-rl-approved'},
    human_review: {label: 'Human Review', className: 'cursor-phase-rl-human'},
    max_iterations: {label: 'Needs Attention', className: 'cursor-phase-rl-maxiter'},
    failed: {label: 'Review Failed', className: 'cursor-phase-rl-failed'},
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
