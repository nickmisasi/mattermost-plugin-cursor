import type {ConversationMessage, Run} from '../types';
import {isTerminalRunStatus} from '../types';

/**
 * A terminal run carries the final assistant reply in `result`. The messages
 * endpoint can lag behind it, so surface the result as the newest assistant
 * message until the conversation catches up.
 */
export function withRunResult(messages: ConversationMessage[], run: Run | null): ConversationMessage[] {
    if (!run?.result.trim() || !isTerminalRunStatus(run.status)) {
        return messages;
    }

    const alreadyShown = messages.some(
        (message) => message.role === 'assistant' && message.text.trim() === run.result.trim(),
    );
    if (alreadyShown) {
        return messages;
    }

    return [...messages, {id: `run-result-${run.id}`, role: 'assistant', text: run.result}];
}
