import {withRunResult} from './conversation';

import type {ConversationMessage, Run} from '../types';

const RUN: Run = {
    id: 'run-1',
    status: 'FINISHED',
    durationMs: 12357,
    result: 'Added README.md.',
    branch: 'cursor/add-readme',
    prUrl: '',
};

const MESSAGES: ConversationMessage[] = [{id: 'm1', role: 'user', text: 'Add a README'}];

describe('withRunResult', () => {
    it('appends the final reply when the conversation has not caught up', () => {
        expect(withRunResult(MESSAGES, RUN)).toEqual([
            ...MESSAGES,
            {id: 'run-result-run-1', role: 'assistant', text: 'Added README.md.'},
        ]);
    });

    it('does not duplicate a reply the conversation already contains', () => {
        const messages = [...MESSAGES, {id: 'm2', role: 'assistant' as const, text: 'Added README.md.\n'}];

        expect(withRunResult(messages, RUN)).toBe(messages);
    });

    it('ignores runs that are still in flight or carry no result', () => {
        expect(withRunResult(MESSAGES, {...RUN, status: 'RUNNING'})).toBe(MESSAGES);
        expect(withRunResult(MESSAGES, {...RUN, result: ''})).toBe(MESSAGES);
        expect(withRunResult(MESSAGES, null)).toBe(MESSAGES);
    });
});
