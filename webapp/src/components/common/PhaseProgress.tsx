import React from 'react';

import type {ReviewLoopPhase, WorkflowPhase} from '../../types';

interface Props {
    phase: WorkflowPhase;
    planIterationCount: number;
    skipContextReview?: boolean;
    skipPlanLoop?: boolean;
    reviewLoopPhase?: ReviewLoopPhase;
    reviewLoopIteration?: number;
}

interface Step {
    key: string;
    label: string;
    isActive: boolean;
    isComplete: boolean;
}

function buildSteps(phase: WorkflowPhase, planIterationCount: number, skipContextReview?: boolean, skipPlanLoop?: boolean, reviewLoopPhase?: ReviewLoopPhase, reviewLoopIteration?: number): Step[] {
    const steps: Step[] = [];

    if (!skipContextReview) {
        steps.push({
            key: 'context',
            label: 'Context',
            isActive: phase === 'context_review',
            isComplete: phase !== 'context_review' && phase !== 'rejected',
        });
    }

    if (!skipPlanLoop) {
        const planLabel = planIterationCount > 1 ? `Plan (v${planIterationCount})` : 'Plan';
        steps.push({
            key: 'plan',
            label: planLabel,
            isActive: phase === 'planning' || phase === 'plan_review',
            isComplete: phase === 'implementing' || phase === 'complete',
        });
    }

    steps.push({
        key: 'implement',
        label: 'Implement',
        isActive: phase === 'implementing',
        isComplete: phase === 'complete',
    });

    // Optional "Review" step -- only shown when a review loop is active.
    if (reviewLoopPhase) {
        const isTerminal = reviewLoopPhase === 'complete' || reviewLoopPhase === 'max_iterations' || reviewLoopPhase === 'failed';
        const isActive = phase === 'complete' && !isTerminal;
        const isComplete = isTerminal && reviewLoopPhase === 'complete';
        const reviewLabel = reviewLoopIteration && reviewLoopIteration > 1 ?
            `Review (iter ${reviewLoopIteration})` :
            'Review';

        steps.push({
            key: 'review',
            label: reviewLabel,
            isActive,
            isComplete,
        });
    }

    return steps;
}

const PhaseProgress: React.FC<Props> = ({phase, planIterationCount, skipContextReview, skipPlanLoop, reviewLoopPhase, reviewLoopIteration}) => {
    if (phase === 'rejected') {
        return null;
    }

    const steps = buildSteps(phase, planIterationCount, skipContextReview, skipPlanLoop, reviewLoopPhase, reviewLoopIteration);

    return (
        <div className='cursor-phase-progress'>
            {steps.map((step, index) => (
                <React.Fragment key={step.key}>
                    {index > 0 && <div className='cursor-phase-progress-connector'/>}
                    <div
                        className={
                            'cursor-phase-progress-step' +
                            (step.isActive ? ' cursor-phase-progress-step--active' : '') +
                            (step.isComplete ? ' cursor-phase-progress-step--complete' : '')
                        }
                    >
                        <div className='cursor-phase-progress-dot'/>
                        <span className='cursor-phase-progress-label'>{step.label}</span>
                    </div>
                </React.Fragment>
            ))}
        </div>
    );
};

export default PhaseProgress;
