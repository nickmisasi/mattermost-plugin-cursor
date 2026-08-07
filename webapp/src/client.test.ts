import Client, {API_BASE, ClientError, errorMessage, isNotConfiguredError} from './client';

interface FakeResponse {
    ok: boolean;
    status: number;
    text: () => Promise<string>;
}

const fetchMock = jest.fn<Promise<FakeResponse>, [string, RequestInit]>();
(global as unknown as {fetch: unknown}).fetch = fetchMock;

function respond(status: number, body?: string): FakeResponse {
    return {
        ok: status >= 200 && status < 300,
        status,
        text: () => Promise.resolve(body ?? ''),
    };
}

function lastRequest(): [string, RequestInit] {
    return fetchMock.mock.calls[fetchMock.mock.calls.length - 1];
}

describe('request construction', () => {
    it('sends GETs without the CSRF header and skips undefined query values', async () => {
        fetchMock.mockResolvedValue(respond(200, '{"items":[]}'));

        await Client.listAgents({limit: 100, includeArchived: true});

        const [url, options] = lastRequest();
        expect(url).toBe(`${API_BASE}/agents?limit=100&includeArchived=true`);
        expect(options.method).toBe('GET');
        expect(options.headers).not.toHaveProperty('X-Requested-With');
        expect(options.credentials).toBe('same-origin');
    });

    it('serializes includeArchived=false rather than dropping it', async () => {
        fetchMock.mockResolvedValue(respond(200, '{"items":[]}'));

        await Client.listAgents({includeArchived: false});

        expect(lastRequest()[0]).toBe(`${API_BASE}/agents?includeArchived=false`);
    });

    it('sends the CSRF header and a JSON body on writes', async () => {
        fetchMock.mockResolvedValue(respond(200, '{"configured":true,"email":"dev@example.com"}'));

        const status = await Client.setKey('key_123');

        const [url, options] = lastRequest();
        expect(url).toBe(`${API_BASE}/key`);
        expect(options.method).toBe('PUT');
        expect(options.headers).toMatchObject({
            'X-Requested-With': 'XMLHttpRequest',
            'Content-Type': 'application/json',
        });
        expect(options.body).toBe('{"apiKey":"key_123"}');
        expect(status).toEqual({configured: true, email: 'dev@example.com'});
    });

    it('escapes path parameters', async () => {
        fetchMock.mockResolvedValue(respond(204));

        await Client.cancelRun('bc/1', 'run 2');

        expect(lastRequest()[0]).toBe(`${API_BASE}/agents/bc%2F1/runs/run%202/cancel`);
    });

    it('resolves to undefined for empty and 204 responses', async () => {
        fetchMock.mockResolvedValue(respond(204));
        await expect(Client.deleteKey()).resolves.toBeUndefined();

        fetchMock.mockResolvedValue(respond(200, ''));
        await expect(Client.getMessages('bc-1')).resolves.toBeUndefined();
    });
});

describe('error handling', () => {
    it('surfaces the error message and code from the response body', async () => {
        fetchMock.mockResolvedValue(respond(403, '{"error":"Connect your Cursor account","code":"api_key_not_configured"}'));

        const error = await Client.getKeyStatus().catch((err: unknown) => err);

        expect(error).toBeInstanceOf(ClientError);
        expect(error).toMatchObject({
            status: 403,
            message: 'Connect your Cursor account',
            code: 'api_key_not_configured',
        });
        expect(isNotConfiguredError(error)).toBe(true);
    });

    it('preserves the status for conflicts so callers can special-case them', async () => {
        fetchMock.mockResolvedValue(respond(409, '{"error":"agent busy","code":"agent_busy"}'));

        const error = await Client.followup('bc-1', 'hi').catch((err: unknown) => err);

        expect(error).toMatchObject({status: 409, code: 'agent_busy'});
        expect(isNotConfiguredError(error)).toBe(false);
    });

    it('falls back to a status message when the error body is not JSON', async () => {
        fetchMock.mockResolvedValue(respond(500, '<html>gateway</html>'));

        const error = await Client.getAgent('bc-1').catch((err: unknown) => err);

        expect(error).toMatchObject({status: 500, code: ''});
        expect(errorMessage(error)).toBe('Request failed with status 500.');
    });

    it('rejects when a successful response is not valid JSON', async () => {
        fetchMock.mockResolvedValue(respond(200, 'not json'));

        const error = await Client.listModels().catch((err: unknown) => err);

        expect(error).toBeInstanceOf(ClientError);
        expect(errorMessage(error)).toBe('The server returned a response that could not be read.');
    });
});

describe('errorMessage', () => {
    it('uses the fallback for values that are not errors', () => {
        expect(errorMessage(undefined, 'fallback')).toBe('fallback');
        expect(errorMessage(new Error('boom'), 'fallback')).toBe('boom');
    });
});

describe('streamUrl', () => {
    it('points at the plugin SSE endpoint', () => {
        expect(Client.streamUrl('bc-1', 'run-1')).toBe(`${API_BASE}/agents/bc-1/runs/run-1/stream`);
    });
});
