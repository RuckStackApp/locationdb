export type IndexOptions = {
  spatial_cell_precision: number
  hot_spatial_cell_threshold: number
}

export type StoreConfig = {
  name: string
  root_path: string
  index_options: IndexOptions
  metadata?: Record<string, string>
}

export type StoreDefinition = {
  config: StoreConfig
}

export type NearFilter = {
  lat: number
  lon: number
  radius: number
}

export type QueryRequest = {
  near: NearFilter
  labels?: string[]
  valid_at?: string
  limit?: number
}

export type QueryPlan = {
  strategy: string[]
}

export type QueryResultRecord = {
  id: string
  code: string
  payload: string
  labels?: string[]
  metadata?: Record<string, string>
}

export type QueryResult = {
  record: QueryResultRecord
  distance_meters: number
}

export type QueryResponse = {
  store_name: string
  request: QueryRequest
  plan: QueryPlan
  results?: QueryResult[]
  status: string
}

export type RecordRequest = {
  id: string
  code?: string
  lat?: number
  lon?: number
  precision?: number
  valid_from?: string
  valid_until?: string
  labels?: string[]
  metadata?: Record<string, string>
}

export class LocationDBClient {
  constructor(private readonly baseUrl = '') {}

  async health(): Promise<{ status: string }> {
    return this.request('/healthz')
  }

  async listStores(): Promise<{ stores: StoreDefinition[] }> {
    return this.request('/v1/stores')
  }

  async createStore(config: StoreConfig): Promise<StoreDefinition> {
    return this.request('/v1/stores', {
      method: 'POST',
      body: JSON.stringify(config)
    })
  }

  async getStore(name: string): Promise<StoreDefinition> {
    return this.request(`/v1/stores/${encodeURIComponent(name)}`)
  }

  async insertRecord(storeName: string, record: RecordRequest): Promise<{ status: string }> {
    return this.request(`/v1/stores/${encodeURIComponent(storeName)}/records`, {
      method: 'POST',
      body: JSON.stringify(record)
    })
  }

  async query(storeName: string, request: QueryRequest): Promise<QueryResponse> {
    return this.request(`/v1/stores/${encodeURIComponent(storeName)}/queries`, {
      method: 'POST',
      body: JSON.stringify(request)
    })
  }

  private async request<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      headers: {
        'Content-Type': 'application/json',
        ...(init?.headers ?? {})
      },
      ...init
    })

    if (!response.ok) {
      const message = await response.text()
      throw new Error(message || `request failed with status ${response.status}`)
    }

    return (await response.json()) as T
  }
}
