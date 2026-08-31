export interface TextSegment {
    type: 'text' | 'code';
    content: string;
}

function isFenceOpener(line: string): boolean {
    return line.trimStart().startsWith('```');
}

function isFenceCloser(line: string): boolean {
    return line.trim() === '```';
}

/**
 * Splits assistant text into plain and fenced-code segments so code can be
 * rendered in a monospace block. Everything else is left verbatim.
 *
 * Line-based and linear in the input length so an unterminated fence cannot
 * trigger catastrophic backtracking the way a /```[\s\S]*?```/ regex would.
 */
export function parseSegments(text: string): TextSegment[] {
    const segments: TextSegment[] = [];
    const lines = text.split('\n');
    const textLines: string[] = [];
    let i = 0;

    const flushText = () => {
        if (textLines.length === 0) {
            return;
        }
        segments.push({type: 'text', content: textLines.join('\n')});
        textLines.length = 0;
    };

    while (i < lines.length) {
        if (!isFenceOpener(lines[i])) {
            textLines.push(lines[i]);
            i += 1;
            continue;
        }

        let close = -1;
        for (let j = i + 1; j < lines.length; j++) {
            if (isFenceCloser(lines[j])) {
                close = j;
                break;
            }
        }

        if (close === -1) {
            // Unterminated fence: remainder is plain text. Do not rescan.
            while (i < lines.length) {
                textLines.push(lines[i]);
                i += 1;
            }
            break;
        }

        flushText();
        const body = lines.slice(i + 1, close).join('\n');
        segments.push({type: 'code', content: close > i + 1 ? `${body}\n` : ''});
        i = close + 1;
    }

    flushText();
    return segments.filter((segment) => segment.content.trim() !== '');
}
