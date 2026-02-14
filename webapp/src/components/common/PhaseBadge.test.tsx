import React from 'react';

import PhaseBadge from './PhaseBadge';

import type {WorkflowPhase} from '../../types';

describe('PhaseBadge', () => {
    const cases: Array<{phase: WorkflowPhase; label: string; className: string}> = [
        {phase: 'context_review', label: 'Awaiting Review', className: 'cursor-phase-review'},
        {phase: 'planning', label: 'Planning...', className: 'cursor-phase-planning'},
        {phase: 'plan_review', label: 'Plan Ready', className: 'cursor-phase-review'},
        {phase: 'implementing', label: 'Implementing', className: 'cursor-phase-implementing'},
        {phase: 'rejected', label: 'Rejected', className: 'cursor-phase-rejected'},
        {phase: 'complete', label: 'Complete', className: 'cursor-phase-complete'},
    ];

    cases.forEach(({phase, label, className}) => {
        it(`renders "${label}" for phase "${phase}"`, () => {
            const element = React.createElement(PhaseBadge, {phase});
            expect(element).toBeDefined();
            expect(element.type).toBe(PhaseBadge);
            expect(element.props.phase).toBe(phase);

            // Directly test the component output
            const result = PhaseBadge({phase});
            expect(result).not.toBeNull();
            expect(result!.props.className).toContain('cursor-phase-badge');
            expect(result!.props.className).toContain(className);
            expect(result!.props.children).toBe(label);
        });
    });

    it('falls back to complete config for unknown phase', () => {
        const result = PhaseBadge({phase: 'unknown_phase' as WorkflowPhase});
        expect(result).not.toBeNull();
        expect(result!.props.children).toBe('Complete');
        expect(result!.props.className).toContain('cursor-phase-complete');
    });
});
