export interface TextSegment {
    type: 'text' | 'code';
    content: string;
}

const FENCE = /```[^\n`]*\n?([\s\S]*?)```/g;

/**
 * Splits assistant text into plain and fenced-code segments so code can be
 * rendered in a monospace block. Everything else is left verbatim.
 */
export function parseSegments(text: string): TextSegment[] {
    const segments: TextSegment[] = [];
    const pattern = new RegExp(FENCE);
    let lastIndex = 0;

    for (let match = pattern.exec(text); match !== null; match = pattern.exec(text)) {
        if (match.index > lastIndex) {
            segments.push({type: 'text', content: text.slice(lastIndex, match.index)});
        }
        segments.push({type: 'code', content: match[1]});
        lastIndex = pattern.lastIndex;
    }

    if (lastIndex < text.length) {
        segments.push({type: 'text', content: text.slice(lastIndex)});
    }

    return segments.filter((segment) => segment.content.trim() !== '');
}
