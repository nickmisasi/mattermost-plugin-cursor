import {parseSegments} from './segments';

describe('parseSegments', () => {
    it('returns a single text segment for plain text', () => {
        expect(parseSegments('Hello from Cursor')).toEqual([
            {type: 'text', content: 'Hello from Cursor'},
        ]);
    });

    it('splits a fenced block with a language tag', () => {
        const text = 'Before\n```go\nfunc main() {}\n```\nAfter';

        expect(parseSegments(text)).toEqual([
            {type: 'text', content: 'Before'},
            {type: 'code', content: 'func main() {}\n'},
            {type: 'text', content: 'After'},
        ]);
    });

    it('handles multiple fences', () => {
        const text = '```js\nconst a = 1;\n```\nmid\n```py\nprint(2)\n```';

        expect(parseSegments(text)).toEqual([
            {type: 'code', content: 'const a = 1;\n'},
            {type: 'text', content: 'mid'},
            {type: 'code', content: 'print(2)\n'},
        ]);
    });

    it('treats an unterminated fence as plain text', () => {
        const text = '```' + 'a'.repeat(200);

        expect(parseSegments(text)).toEqual([
            {type: 'text', content: text},
        ]);
    });

    it('parses an unterminated long fence in linear time', () => {
        const text = '```' + 'a'.repeat(200000);
        const start = Date.now();
        const segments = parseSegments(text);
        const elapsed = Date.now() - start;

        expect(elapsed).toBeLessThan(200);
        expect(segments).toEqual([{type: 'text', content: text}]);
    });

    it('returns no segments for empty or whitespace-only input', () => {
        expect(parseSegments('')).toEqual([]);
        expect(parseSegments('   \n\t')).toEqual([]);
    });

    it('preserves inner code including blank lines', () => {
        const text = '```\nline one\n\nline three\n```';

        expect(parseSegments(text)).toEqual([
            {type: 'code', content: 'line one\n\nline three\n'},
        ]);
    });
});
