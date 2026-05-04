export type IndexOptions = {
    spatial_cell_precision: number;
    hot_spatial_cell_threshold: number;
};
export type StoreConfig = {
    name: string;
    root_path: string;
    index_options: IndexOptions;
    schema?: RecordSchema;
    metadata?: Record<string, string>;
};
export type FieldType = 'string' | 'int' | 'float' | 'bool' | 'datetime';
export type CollectionType = 'point_of_interest' | 'moving_object';
export type FieldSchema = {
    type: FieldType;
    required?: boolean;
    indexed?: boolean;
    enum?: string[];
};
export type RecordSchema = {
    collection_type?: CollectionType;
    fields: Record<string, FieldSchema>;
};
export type StoreDefinition = {
    config: StoreConfig;
};
export type NearFilter = {
    lat: number;
    lon: number;
    radius: number;
};
export type QueryRequest = {
    near: NearFilter;
    labels?: string[];
    valid_at?: string;
    limit?: number;
};
export type QueryPlan = {
    strategy: string[];
};
export type QueryResultRecord = {
    id: string;
    code: string;
    payload: string;
    labels?: string[];
    metadata?: Record<string, string>;
};
export type QueryResult = {
    record: QueryResultRecord;
    distance_meters: number;
};
export type QueryResponse = {
    store_name: string;
    request: QueryRequest;
    plan: QueryPlan;
    results?: QueryResult[];
    status: string;
};
export type RecordRequest = {
    id: string;
    code?: string;
    lat?: number;
    lon?: number;
    precision?: number;
    valid_from?: string;
    valid_until?: string;
    fields?: Record<string, unknown>;
    labels?: string[];
    metadata?: Record<string, string>;
};
export declare class LocationDBClient {
    private readonly baseUrl;
    constructor(baseUrl?: string);
    health(): Promise<{
        status: string;
    }>;
    listStores(): Promise<{
        stores: StoreDefinition[];
    }>;
    createStore(config: StoreConfig): Promise<StoreDefinition>;
    getStore(name: string): Promise<StoreDefinition>;
    updateSchema(storeName: string, schema: RecordSchema): Promise<StoreDefinition>;
    insertRecord(storeName: string, record: RecordRequest): Promise<{
        status: string;
    }>;
    query(storeName: string, request: QueryRequest): Promise<QueryResponse>;
    private request;
}
