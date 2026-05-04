export class LocationDBClient {
    baseUrl;
    constructor(baseUrl = '') {
        this.baseUrl = baseUrl;
    }
    async health() {
        return this.request('/healthz');
    }
    async listStores() {
        return this.request('/v1/stores');
    }
    async createStore(config) {
        return this.request('/v1/stores', {
            method: 'POST',
            body: JSON.stringify(config)
        });
    }
    async getStore(name) {
        return this.request(`/v1/stores/${encodeURIComponent(name)}`);
    }
    async updateSchema(storeName, schema) {
        return this.request(`/v1/stores/${encodeURIComponent(storeName)}/schema`, {
            method: 'PUT',
            body: JSON.stringify(schema)
        });
    }
    async insertRecord(storeName, record) {
        return this.request(`/v1/stores/${encodeURIComponent(storeName)}/records`, {
            method: 'POST',
            body: JSON.stringify(record)
        });
    }
    async query(storeName, request) {
        return this.request(`/v1/stores/${encodeURIComponent(storeName)}/queries`, {
            method: 'POST',
            body: JSON.stringify(request)
        });
    }
    async request(path, init) {
        const response = await fetch(`${this.baseUrl}${path}`, {
            headers: {
                'Content-Type': 'application/json',
                ...(init?.headers ?? {})
            },
            ...init
        });
        if (!response.ok) {
            const message = await response.text();
            throw new Error(message || `request failed with status ${response.status}`);
        }
        return (await response.json());
    }
}
