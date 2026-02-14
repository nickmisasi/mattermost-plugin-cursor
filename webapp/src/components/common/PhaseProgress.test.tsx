import type React from 'react';

import PhaseProgress from './PhaseProgress';

// Helper to extract step labels from the rendered React element tree.
function getStepLabels(element: React.ReactElement | null): string[] {
    if (!element) {
        return [];
    }
    const labels: string[] = [];
    const children = element.props.children;
    if (!children) {
        return labels;
    }
    const items = Array.isArray(children) ? children : [children];
    for (const child of items) {
        if (!child || !child.props) {
            continue;
        }

        // Each Fragment has [connector?, step-div] as children
        const fragChildren = child.props.children;
        if (!fragChildren) {
            continue;
        }
        const fragItems = Array.isArray(fragChildren) ? fragChildren : [fragChildren];
        for (const fragChild of fragItems) {
            if (!fragChild || !fragChild.props) {
                continue;
            }

            // Step div has className containing 'cursor-phase-progress-step'
            if (fragChild.props.className && typeof fragChild.props.className === 'string' &&
                fragChild.props.className.includes('cursor-phase-progress-step')) {
                // The step div's children are [dot-div, label-span]
                const stepChildren = fragChild.props.children;
                const stepItems = Array.isArray(stepChildren) ? stepChildren : [stepChildren];
                for (const stepChild of stepItems) {
                    if (stepChild && stepChild.type === 'span' && stepChild.props.className &&
                        stepChild.props.className.includes('cursor-phase-progress-label')) {
                        labels.push(stepChild.props.children);
                    }
                }
            }
        }
    }
    return labels;
}

// Helper to extract step classNames.
function getStepClassNames(element: React.ReactElement | null): string[] {
    if (!element) {
        return [];
    }
    const classNames: string[] = [];
    const children = element.props.children;
    if (!children) {
        return classNames;
    }
    const items = Array.isArray(children) ? children : [children];
    for (const child of items) {
        if (!child || !child.props) {
            continue;
        }
        const fragChildren = child.props.children;
        if (!fragChildren) {
            continue;
        }
        const fragItems = Array.isArray(fragChildren) ? fragChildren : [fragChildren];
        for (const fragChild of fragItems) {
            if (!fragChild || !fragChild.props) {
                continue;
            }
            if (fragChild.props.className && typeof fragChild.props.className === 'string' &&
                fragChild.props.className.includes('cursor-phase-progress-step')) {
                classNames.push(fragChild.props.className);
            }
        }
    }
    return classNames;
}

describe('PhaseProgress', () => {
    it('renders all three steps when both HITL stages are enabled', () => {
        const result = PhaseProgress({phase: 'context_review', planIterationCount: 0});
        const labels = getStepLabels(result);
        expect(labels).toEqual(['Context', 'Plan', 'Implement']);
    });

    it('omits Context step when skipContextReview is true', () => {
        const result = PhaseProgress({phase: 'planning', planIterationCount: 0, skipContextReview: true});
        const labels = getStepLabels(result);
        expect(labels).toEqual(['Plan', 'Implement']);
        expect(labels).not.toContain('Context');
    });

    it('omits Plan step when skipPlanLoop is true', () => {
        const result = PhaseProgress({phase: 'context_review', planIterationCount: 0, skipPlanLoop: true});
        const labels = getStepLabels(result);
        expect(labels).toEqual(['Context', 'Implement']);
        expect(labels).not.toContain('Plan');
    });

    it('shows only Implement when both stages are skipped', () => {
        const result = PhaseProgress({
            phase: 'implementing', planIterationCount: 0, skipContextReview: true, skipPlanLoop: true,
        });
        const labels = getStepLabels(result);
        expect(labels).toEqual(['Implement']);
    });

    it('shows iteration count in Plan label when count > 1', () => {
        const result = PhaseProgress({phase: 'plan_review', planIterationCount: 3});
        const labels = getStepLabels(result);
        expect(labels).toContain('Plan (v3)');
    });

    it('shows plain Plan label when count is 0', () => {
        const result = PhaseProgress({phase: 'plan_review', planIterationCount: 0});
        const labels = getStepLabels(result);
        expect(labels).toContain('Plan');
        expect(labels).not.toContain('Plan (v0)');
    });

    it('shows plain Plan label when count is 1', () => {
        const result = PhaseProgress({phase: 'plan_review', planIterationCount: 1});
        const labels = getStepLabels(result);
        expect(labels).toContain('Plan');
        expect(labels).not.toContain('Plan (v1)');
    });

    it('returns null for rejected phase', () => {
        const result = PhaseProgress({phase: 'rejected', planIterationCount: 0});
        expect(result).toBeNull();
    });

    it('marks Context step as active during context_review', () => {
        const result = PhaseProgress({phase: 'context_review', planIterationCount: 0});
        const classNames = getStepClassNames(result);
        expect(classNames[0]).toContain('cursor-phase-progress-step--active');
        expect(classNames[0]).not.toContain('cursor-phase-progress-step--complete');
    });

    it('marks Context as complete and Plan as active during planning', () => {
        const result = PhaseProgress({phase: 'planning', planIterationCount: 0});
        const classNames = getStepClassNames(result);
        expect(classNames[0]).toContain('cursor-phase-progress-step--complete');
        expect(classNames[1]).toContain('cursor-phase-progress-step--active');
    });

    it('marks Implement step as active during implementing', () => {
        const result = PhaseProgress({phase: 'implementing', planIterationCount: 0});
        const classNames = getStepClassNames(result);

        // Context: complete, Plan: complete, Implement: active
        expect(classNames[0]).toContain('cursor-phase-progress-step--complete');
        expect(classNames[1]).toContain('cursor-phase-progress-step--complete');
        expect(classNames[2]).toContain('cursor-phase-progress-step--active');
    });

    it('marks all steps as complete during complete phase', () => {
        const result = PhaseProgress({phase: 'complete', planIterationCount: 1});
        const classNames = getStepClassNames(result);
        expect(classNames[0]).toContain('cursor-phase-progress-step--complete');
        expect(classNames[1]).toContain('cursor-phase-progress-step--complete');
        expect(classNames[2]).toContain('cursor-phase-progress-step--complete');
    });
});
