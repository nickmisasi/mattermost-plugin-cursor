import React from 'react';

import PhaseBadge, {getDisplayPhase} from './PhaseBadge';

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

describe('getDisplayPhase', () => {
    it('returns review loop phase when both workflow and review loop exist (avoids contradictory states)', () => {
        // Bug 2: workflow "complete" + review "awaiting_review" must not both render
        expect(getDisplayPhase('complete', 'awaiting_review', false)).toBe('awaiting_review');
        expect(getDisplayPhase('implementing', 'cursor_fixing', false)).toBe('cursor_fixing');
    });

    it('returns workflow phase when only workflow exists', () => {
        expect(getDisplayPhase('planning', undefined, false)).toBe('planning');
        expect(getDisplayPhase('complete', undefined, false)).toBe('complete');
    });

    it('returns review loop phase when only review loop exists', () => {
        expect(getDisplayPhase(undefined, 'awaiting_review', false)).toBe('awaiting_review');
    });

    it('hides non-terminal workflow phase when aborted', () => {
        expect(getDisplayPhase('implementing', undefined, true)).toBeUndefined();
        expect(getDisplayPhase('planning', undefined, true)).toBeUndefined();
    });

    it('shows terminal workflow phase when aborted', () => {
        expect(getDisplayPhase('complete', undefined, true)).toBe('complete');
        expect(getDisplayPhase('rejected', undefined, true)).toBe('rejected');
    });

    it('prefers review loop over workflow when aborted and both exist', () => {
        expect(getDisplayPhase('complete', 'awaiting_review', true)).toBe('awaiting_review');
    });

    it('returns undefined when neither phase exists', () => {
        expect(getDisplayPhase(undefined, undefined, false)).toBeUndefined();
        expect(getDisplayPhase(undefined, undefined, true)).toBeUndefined();
    });
});
