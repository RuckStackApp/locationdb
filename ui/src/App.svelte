<script lang="ts">
  import { onMount } from 'svelte'
  import {
    LocationDBClient,
    type QueryResponse,
    type RecordSchema,
    type FieldSchema,
    type StoreDefinition
  } from '@ruckstack/locationdb'

  // Not a real server field — kept to satisfy linter references only
  type CollectionType = string

  type FieldType = 'string' | 'int' | 'float' | 'bool' | 'datetime'
  type TabKind = 'query' | 'schema'

  type WorkspaceTab = {
    id: string
    storeName: string
    kind: TabKind
  }

  type FieldRow = {
    _id: string
    name: string
    type: FieldType
    required: boolean
    indexed: boolean
    enum: string
  }

  const client = new LocationDBClient('')

  let stores: StoreDefinition[] = []
  let search = ''
  let loading = true
  let error = ''
  let saveMessage = ''
  let tabs: WorkspaceTab[] = []
  let activeTabId = ''
  let queryResults: Record<string, QueryResponse | null> = {}
  let fieldRows: Record<string, FieldRow[]> = {}
  let selectedFieldId: Record<string, string> = {}
  let collectionTypes: Record<string, CollectionType> = {}
  let menuOpenFor = ''

  // query params per tab
  let queryParams: Record<string, { lat: number; lon: number; radius: number; labels: string; validAt: string; limit: number }> = {}

  // new collection modal
  let showNewModal = false
  let newName = ''
  let newRootPath = ''
  let newPrecision = 12
  let newHotThreshold = 10
  let newModalError = ''
  let newModalSaving = false

  let nextId = 0
  function uid() { return String(nextId++) }

  onMount(async () => { await refreshStores() })

  async function refreshStores() {
    loading = true; error = ''
    try {
      const response = await client.listStores()
      stores = response.stores
      for (const store of stores) {
        const tabId = `${store.config.name}:schema`
        if (!fieldRows[tabId]) fieldRows[tabId] = schemaToRows(store.config.schema)
        if (!collectionTypes[tabId]) collectionTypes[tabId] = store.config.schema?.collection_type ?? 'point_of_interest'
      }
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load stores'
    } finally { loading = false }
  }

  function schemaToRows(schema: RecordSchema | undefined): FieldRow[] {
    if (!schema?.fields) return []
    return Object.entries(schema.fields).map(([name, f]) => ({
      _id: uid(),
      name,
      type: f.type as FieldType,
      required: f.required ?? false,
      indexed: f.indexed ?? false,
      enum: (f.enum ?? []).join(', ')
    }))
  }

  function rowsToSchema(tabId: string, rows: FieldRow[]): RecordSchema {
    const fields: Record<string, FieldSchema> = {}
    for (const row of rows) {
      if (!row.name.trim()) continue
      const enumVals = row.enum.split(',').map(s => s.trim()).filter(Boolean)
      fields[row.name.trim()] = {
        type: row.type,
        required: row.required,
        indexed: row.indexed,
        ...(enumVals.length ? { enum: enumVals } : {})
      }
    }
    return { fields }
  }

  function openTab(store: StoreDefinition, kind: TabKind) {
    const id = `${store.config.name}:${kind}`
    if (!tabs.find(t => t.id === id)) {
      tabs = [...tabs, { id, storeName: store.config.name, kind }]
      if (kind === 'schema' && !fieldRows[id]) {
        fieldRows[id] = schemaToRows(store.config.schema)
        collectionTypes[id] = store.config.schema?.collection_type ?? 'point_of_interest'
      }
      if (kind === 'query' && !queryParams[id]) {
        queryParams[id] = { lat: 43.65, lon: -79.38, radius: 2000, labels: '', validAt: '', limit: 50 }
      }
    }
    activeTabId = id
  }

  function closeTab(id: string, e: MouseEvent) {
    e.stopPropagation()
    tabs = tabs.filter(t => t.id !== id)
    if (activeTabId === id) activeTabId = tabs[tabs.length - 1]?.id ?? ''
  }

  function addField(tabId: string) {
    fieldRows[tabId] = [...(fieldRows[tabId] ?? []), { _id: uid(), name: '', type: 'string', required: false, indexed: false, enum: '' }]
    selectedFieldId[tabId] = fieldRows[tabId][fieldRows[tabId].length - 1]._id
  }

  function removeField(tabId: string, rowId: string) {
    fieldRows[tabId] = fieldRows[tabId].filter(r => r._id !== rowId)
    if (selectedFieldId[tabId] === rowId) selectedFieldId[tabId] = ''
  }

  async function saveSchema(tab: WorkspaceTab) {
    error = ''; saveMessage = ''
    try {
      const schema = rowsToSchema(tab.id, fieldRows[tab.id] ?? [])
      const updated = await client.updateSchema(tab.storeName, schema)
      stores = stores.map(s => s.config.name === updated.config.name ? updated : s)
      fieldRows[tab.id] = schemaToRows(updated.config.schema)
      collectionTypes[tab.id] = updated.config.schema?.collection_type ?? 'point_of_interest'
      saveMessage = `Saved schema for ${tab.storeName}`
      setTimeout(() => { saveMessage = '' }, 3000)
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to save schema'
    }
  }

  async function runQuery(tab: WorkspaceTab) {
    error = ''; saveMessage = ''
    const p = queryParams[tab.id]
    try {
      queryResults = {
        ...queryResults,
        [tab.id]: await client.query(tab.storeName, {
          near: { lat: p.lat, lon: p.lon, radius: p.radius },
          labels: p.labels.split(',').map(v => v.trim()).filter(Boolean),
          valid_at: p.validAt || undefined,
          limit: p.limit
        })
      }
    } catch (err) {
      error = err instanceof Error ? err.message : 'Query failed'
    }
  }

  function openNewModal() {
    newName = ''; newRootPath = ''; newPrecision = 12; newHotThreshold = 10
    newModalError = ''; newModalSaving = false
    showNewModal = true
  }

  async function createCollection() {
    newModalError = ''
    if (!newName.trim()) { newModalError = 'Name is required'; return }
    if (!newRootPath.trim()) { newModalError = 'Root path is required'; return }
    newModalSaving = true
    try {
      const def = await client.createStore({
        name: newName.trim(),
        root_path: newRootPath.trim(),
        index_options: {
          spatial_cell_precision: newPrecision,
          hot_spatial_cell_threshold: newHotThreshold
        }
      })
      stores = [...stores, def]
      showNewModal = false
      openTab(def, 'schema')
    } catch (err) {
      newModalError = err instanceof Error ? err.message : 'Failed to create collection'
    } finally {
      newModalSaving = false
    }
  }

  function handleModalKey(e: KeyboardEvent) {
    if (e.key === 'Escape') showNewModal = false
    if (e.key === 'Enter' && !newModalSaving) createCollection()
  }

  $: filteredStores = stores.filter(s =>
    s.config.name.toLowerCase().includes(search.toLowerCase())
  )
  $: activeTab = tabs.find(t => t.id === activeTabId) ?? null
  $: activeRows = activeTab?.kind === 'schema' ? (fieldRows[activeTab.id] ?? []) : []
  $: activeSelectedField = activeTab ? (activeRows.find(r => r._id === selectedFieldId[activeTab.id]) ?? null) : null
  $: activeQueryResponse = activeTab ? queryResults[activeTab.id] ?? null : null
  $: activeQueryParams = activeTab?.kind === 'query' ? queryParams[activeTab.id] : null
</script>

<div class="shell">
  <!-- SIDEBAR -->
  <aside class="sidebar">
    <div class="sidebar-top">
      <div class="brand">
        <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
          <circle cx="9" cy="9" r="8" stroke="#5aa9ff" stroke-width="1.5"/>
          <circle cx="9" cy="9" r="3" fill="#5aa9ff"/>
        </svg>
        <span class="brand-name">locationdb</span>
      </div>
      <div class="sidebar-btns">
        <button class="icon-btn" title="New collection" on:click={openNewModal}>
          <svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor">
            <line x1="7" y1="1" x2="7" y2="13" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/>
            <line x1="1" y1="7" x2="13" y2="7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/>
          </svg>
        </button>
        <button class="icon-btn" title="Refresh" on:click={refreshStores}>
          <svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor">
            <path d="M11.07 2.93A6 6 0 1 0 12.8 8h-1.5a4.5 4.5 0 1 1-1.18-3.07l-1.62 1.62H13V2l-1.93.93z"/>
          </svg>
        </button>
      </div>
    </div>

    <div class="search-wrap">
      <svg class="search-icon" width="13" height="13" viewBox="0 0 13 13" fill="currentColor">
        <circle cx="5.5" cy="5.5" r="4" stroke="currentColor" stroke-width="1.3" fill="none"/>
        <line x1="8.5" y1="8.5" x2="12" y2="12" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/>
      </svg>
      <input class="search-input" placeholder="Search collections…" bind:value={search} />
    </div>

    <div class="collection-section">
      <div class="section-label">Collections</div>
      {#if loading}
        <div class="muted sidebar-msg">Loading…</div>
      {:else if filteredStores.length === 0}
        <div class="muted sidebar-msg">{search ? 'No match' : 'No collections'}</div>
      {:else}
        {#each filteredStores as store}
          {@const isActive = tabs.some(t => t.storeName === store.config.name && t.id === activeTabId)}
          <div class="collection-item" class:active={isActive}>
            <div class="collection-main">
              <svg width="13" height="13" viewBox="0 0 13 13" fill="none" class="coll-icon">
                <rect x="1" y="3" width="11" height="8" rx="1.5" stroke="#5aa9ff" stroke-width="1.2"/>
                <path d="M4 3V2a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v1" stroke="#5aa9ff" stroke-width="1.2"/>
              </svg>
              <span class="coll-name">{store.config.name}</span>
            </div>
            <div class="coll-actions">
              <button class="coll-action-btn" title="Schema" on:click|stopPropagation={() => openTab(store, 'schema')}>
                <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor">
                  <rect x="1" y="1" width="10" height="10" rx="1" stroke="currentColor" stroke-width="1.2" fill="none"/>
                  <line x1="1" y1="4" x2="11" y2="4" stroke="currentColor" stroke-width="1"/>
                  <line x1="4" y1="4" x2="4" y2="11" stroke="currentColor" stroke-width="1"/>
                </svg>
              </button>
              <button class="coll-action-btn" title="Query" on:click|stopPropagation={() => openTab(store, 'query')}>
                <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor">
                  <circle cx="5" cy="5" r="3.5" stroke="currentColor" stroke-width="1.2" fill="none"/>
                  <line x1="7.5" y1="7.5" x2="11" y2="11" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/>
                </svg>
              </button>
            </div>
          </div>
        {/each}
      {/if}
    </div>
  </aside>

  <!-- WORKSPACE -->
  <main class="workspace">
    <!-- TAB BAR -->
    <div class="tabbar">
      {#each tabs as tab}
        <div
          class="tab"
          class:active={activeTabId === tab.id}
          role="tab"
          tabindex="0"
          on:click={() => (activeTabId = tab.id)}
          on:keydown={(e) => e.key === 'Enter' && (activeTabId = tab.id)}
        >
          {#if tab.kind === 'schema'}
            <svg width="11" height="11" viewBox="0 0 12 12" fill="currentColor" class="tab-icon">
              <rect x="1" y="1" width="10" height="10" rx="1" stroke="currentColor" stroke-width="1.2" fill="none"/>
              <line x1="1" y1="4" x2="11" y2="4" stroke="currentColor" stroke-width="1"/>
              <line x1="4" y1="4" x2="4" y2="11" stroke="currentColor" stroke-width="1"/>
            </svg>
          {:else}
            <svg width="11" height="11" viewBox="0 0 12 12" fill="currentColor" class="tab-icon">
              <circle cx="5" cy="5" r="3.5" stroke="currentColor" stroke-width="1.2" fill="none"/>
              <line x1="7.5" y1="7.5" x2="11" y2="11" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/>
            </svg>
          {/if}
          <span>{tab.storeName}</span>
          <span class="tab-kind">{tab.kind}</span>
          <button class="tab-close" on:click={(e) => closeTab(tab.id, e)}>×</button>
        </div>
      {/each}
      {#if tabs.length === 0}
        <div class="tabbar-empty">Open a collection →</div>
      {/if}
    </div>

    <!-- NOTIFICATIONS -->
    {#if error}
      <div class="notify error">{error}</div>
    {/if}
    {#if saveMessage}
      <div class="notify success">{saveMessage}</div>
    {/if}

    <!-- CONTENT -->
    {#if !activeTab}
      <div class="empty-state">
        <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
          <circle cx="24" cy="24" r="22" stroke="rgba(255,255,255,0.1)" stroke-width="2"/>
          <circle cx="24" cy="24" r="8" stroke="rgba(90,169,255,0.4)" stroke-width="2"/>
        </svg>
        <p>Select a collection from the sidebar to get started.</p>
      </div>

    {:else if activeTab.kind === 'schema'}
      <!-- SCHEMA EDITOR -->
      <div class="schema-editor">
        <div class="editor-toolbar">
          <div class="editor-title">
            <strong>{activeTab.storeName}</strong>
            <span class="kind-badge">Schema</span>
          </div>
          <div class="toolbar-actions">
            <button class="btn-primary" on:click={() => saveSchema(activeTab)}>Save</button>
          </div>
        </div>

        <div class="schema-grid-wrap">
          <table class="schema-grid">
            <thead>
              <tr>
                <th class="col-name">Name</th>
                <th class="col-type">Type</th>
                <th class="col-check">Not Null</th>
                <th class="col-check">Indexed</th>
                <th class="col-del"></th>
              </tr>
            </thead>
            <tbody>
              {#each activeRows as row (row._id)}
                {@const isSelected = selectedFieldId[activeTab.id] === row._id}
                <tr
                  class:selected={isSelected}
                  on:click={() => { selectedFieldId[activeTab.id] = row._id }}
                >
                  <td class="col-name">
                    <input
                      class="cell-input"
                      bind:value={row.name}
                      placeholder="field_name"
                      on:click|stopPropagation
                    />
                  </td>
                  <td class="col-type">
                    <select class="cell-select" bind:value={row.type} on:click|stopPropagation>
                      <option value="string">string</option>
                      <option value="int">int</option>
                      <option value="float">float</option>
                      <option value="bool">bool</option>
                      <option value="datetime">datetime</option>
                    </select>
                  </td>
                  <td class="col-check">
                    <input type="checkbox" class="cell-check" bind:checked={row.required} on:click|stopPropagation />
                  </td>
                  <td class="col-check">
                    <input type="checkbox" class="cell-check" bind:checked={row.indexed} on:click|stopPropagation />
                  </td>
                  <td class="col-del">
                    <button class="del-row" title="Remove field" on:click|stopPropagation={() => removeField(activeTab.id, row._id)}>×</button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>

          <div class="add-field-row">
            <button class="add-field-btn" on:click={() => addField(activeTab.id)}>
              <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor">
                <line x1="6" y1="1" x2="6" y2="11" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
                <line x1="1" y1="6" x2="11" y2="6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
              </svg>
              Add Field
            </button>
            <span class="field-count">{activeRows.length} field{activeRows.length === 1 ? '' : 's'}</span>
          </div>
        </div>

        <!-- DETAIL PANEL -->
        {#if activeSelectedField}
          <div class="detail-panel">
            <div class="detail-row">
              <label class="detail-label" for="enum-values">Enum Values</label>
              <input id="enum-values" class="detail-input" placeholder="val1, val2, val3" bind:value={activeSelectedField.enum} />
              <span class="detail-hint">Comma-separated allowed values (optional)</span>
            </div>
          </div>
        {/if}
      </div>

    {:else if activeTab.kind === 'query' && activeQueryParams}
      <!-- QUERY CONSOLE -->
      <div class="query-editor">
        <div class="editor-toolbar">
          <div class="editor-title">
            <strong>{activeTab.storeName}</strong>
            <span class="kind-badge">Query</span>
          </div>
          <button class="btn-primary" on:click={() => runQuery(activeTab)}>
            <svg width="11" height="11" viewBox="0 0 12 12" fill="currentColor">
              <polygon points="2,1 11,6 2,11" fill="currentColor"/>
            </svg>
            Run
          </button>
        </div>

        <div class="query-params">
          <div class="param-group">
            <span class="param-group-label">Location</span>
            <label class="param-label">
              <span>Latitude</span>
              <input class="param-input" type="number" bind:value={activeQueryParams.lat} step="0.0001" />
            </label>
            <label class="param-label">
              <span>Longitude</span>
              <input class="param-input" type="number" bind:value={activeQueryParams.lon} step="0.0001" />
            </label>
            <label class="param-label">
              <span>Radius (m)</span>
              <input class="param-input" type="number" bind:value={activeQueryParams.radius} step="100" />
            </label>
          </div>
          <div class="param-group">
            <span class="param-group-label">Filters</span>
            <label class="param-label">
              <span>Labels</span>
              <input class="param-input" type="text" bind:value={activeQueryParams.labels} placeholder="restaurant, cafe" />
            </label>
            <label class="param-label">
              <span>Valid At</span>
              <input class="param-input" type="text" bind:value={activeQueryParams.validAt} placeholder="2026-05-03T12:00:00Z" />
            </label>
            <label class="param-label">
              <span>Limit</span>
              <input class="param-input" type="number" bind:value={activeQueryParams.limit} min="1" />
            </label>
          </div>
        </div>

        <div class="results-area">
          <div class="results-header">
            Results
            {#if activeQueryResponse?.results?.length}
              <span class="result-count">{activeQueryResponse.results.length} row{activeQueryResponse.results.length === 1 ? '' : 's'}</span>
            {/if}
          </div>
          {#if activeQueryResponse?.results?.length}
            <table class="results-table">
              <thead>
                <tr><th>ID</th><th>Code</th><th>Distance</th><th>Labels</th></tr>
              </thead>
              <tbody>
                {#each activeQueryResponse.results as result}
                  <tr>
                    <td class="mono">{result.record.id}</td>
                    <td>{result.record.code ?? '—'}</td>
                    <td>{Math.round(result.distance_meters)}m</td>
                    <td>{result.record.labels?.join(', ') ?? '—'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {:else}
            <div class="muted results-empty">No results. Run a query.</div>
          {/if}
        </div>

        {#if activeQueryResponse}
          <div class="plan-area">
            <div class="results-header">Execution Plan</div>
            <ol class="plan-list">
              {#each activeQueryResponse.plan.strategy as step}
                <li>{step}</li>
              {/each}
            </ol>
          </div>
        {/if}
      </div>
    {/if}
  </main>
</div>

<!-- NEW COLLECTION MODAL -->
{#if showNewModal}
  <div class="modal-backdrop" role="presentation" on:click={() => (showNewModal = false)}>
    <div
      class="modal"
      role="dialog"
      tabindex="-1"
      aria-modal="true"
      aria-label="New collection"
      on:click|stopPropagation
      on:keydown={handleModalKey}
    >
      <div class="modal-header">
        <span class="modal-title">New Collection</span>
        <button class="modal-close" on:click={() => (showNewModal = false)}>×</button>
      </div>

      <div class="modal-body">
        {#if newModalError}
          <div class="modal-error">{newModalError}</div>
        {/if}

        <div class="modal-field">
          <label class="modal-label" for="nc-name">Name</label>
          <input id="nc-name" class="modal-input" bind:value={newName} placeholder="my_collection" />
        </div>

        <div class="modal-field">
          <label class="modal-label" for="nc-path">Root Path</label>
          <input id="nc-path" class="modal-input" bind:value={newRootPath} placeholder="/data/my_collection" />
        </div>

        <div class="modal-section-label">Index Options</div>

        <div class="modal-row">
          <div class="modal-field">
            <label class="modal-label" for="nc-precision">Spatial Cell Precision</label>
            <input id="nc-precision" class="modal-input" type="number" bind:value={newPrecision} min="1" max="30" />
          </div>
          <div class="modal-field">
            <label class="modal-label" for="nc-hot">Hot Cell Threshold</label>
            <input id="nc-hot" class="modal-input" type="number" bind:value={newHotThreshold} min="1" />
          </div>
        </div>
      </div>

      <div class="modal-footer">
        <button class="btn-ghost" on:click={() => (showNewModal = false)}>Cancel</button>
        <button class="btn-primary" disabled={newModalSaving} on:click={createCollection}>
          {newModalSaving ? 'Creating…' : 'Create'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  :global(*, *::before, *::after) { box-sizing: border-box; }
  :global(body) {
    margin: 0;
    background: #0d0f12;
    color: #d4d8de;
    font: 13px/1.5 -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  }

  .shell {
    display: grid;
    grid-template-columns: 240px 1fr;
    height: 100vh;
    overflow: hidden;
  }

  /* ── SIDEBAR ── */
  .sidebar {
    background: #111418;
    border-right: 1px solid #1e2228;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .sidebar-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 14px 10px;
    border-bottom: 1px solid #1e2228;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .brand-name {
    font-weight: 600;
    font-size: 13px;
    color: #e0e4ea;
    letter-spacing: 0.02em;
  }

  .icon-btn {
    background: transparent;
    border: none;
    color: #6b7280;
    cursor: pointer;
    padding: 4px;
    border-radius: 4px;
    display: flex;
    align-items: center;
  }

  .icon-btn:hover { color: #9ca3af; background: rgba(255,255,255,0.05); }

  .search-wrap {
    position: relative;
    padding: 10px 10px 6px;
  }

  .search-icon {
    position: absolute;
    left: 20px;
    top: 50%;
    transform: translateY(-50%);
    color: #4b5563;
    pointer-events: none;
    margin-top: 2px;
  }

  .search-input {
    width: 100%;
    background: #0d0f12;
    border: 1px solid #1e2228;
    border-radius: 6px;
    padding: 6px 10px 6px 28px;
    color: #d4d8de;
    font: inherit;
    font-size: 12px;
    outline: none;
  }

  .search-input:focus { border-color: #2d4a6e; }
  .search-input::placeholder { color: #3d4451; }

  .collection-section {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
  }

  .section-label {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: #4b5563;
    padding: 8px 14px 4px;
  }

  .sidebar-msg {
    padding: 8px 14px;
    font-size: 12px;
  }

  .collection-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 5px 10px 5px 14px;
    cursor: pointer;
    border-radius: 0;
    user-select: none;
  }

  .collection-item:hover { background: rgba(255,255,255,0.04); }
  .collection-item.active { background: rgba(90,169,255,0.1); }
  .collection-item:hover .coll-actions { opacity: 1; }

  .collection-main {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
  }

  .coll-icon { flex-shrink: 0; }

  .coll-name {
    font-size: 12.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: #c9ced6;
  }

  .coll-actions {
    display: flex;
    gap: 2px;
    opacity: 0;
    transition: opacity 0.1s;
  }

  .coll-action-btn {
    background: transparent;
    border: none;
    color: #6b7280;
    cursor: pointer;
    padding: 3px 4px;
    border-radius: 4px;
    display: flex;
    align-items: center;
  }

  .coll-action-btn:hover { color: #9ca3af; background: rgba(255,255,255,0.08); }

  /* ── WORKSPACE ── */
  .workspace {
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: #0d0f12;
  }

  /* ── TABBAR ── */
  .tabbar {
    display: flex;
    align-items: stretch;
    gap: 0;
    background: #111418;
    border-bottom: 1px solid #1e2228;
    min-height: 38px;
    overflow-x: auto;
    flex-shrink: 0;
  }

  .tabbar::-webkit-scrollbar { height: 3px; }
  .tabbar::-webkit-scrollbar-track { background: transparent; }
  .tabbar::-webkit-scrollbar-thumb { background: #2d3340; }

  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 12px;
    background: transparent;
    border: none;
    border-right: 1px solid #1e2228;
    color: #6b7280;
    cursor: pointer;
    font: inherit;
    font-size: 12px;
    white-space: nowrap;
    position: relative;
    min-width: 120px;
  }

  .tab:hover { background: rgba(255,255,255,0.03); color: #9ca3af; }

  .tab.active {
    background: #0d0f12;
    color: #e0e4ea;
    border-bottom: 2px solid #5aa9ff;
  }

  .tab-icon { opacity: 0.6; }
  .tab.active .tab-icon { opacity: 1; color: #5aa9ff; }

  .tab-kind {
    font-size: 10px;
    color: #4b5563;
    background: rgba(255,255,255,0.05);
    border-radius: 3px;
    padding: 1px 4px;
  }

  .tab.active .tab-kind { color: #6b7280; }

  .tab-close {
    background: transparent;
    border: none;
    color: #4b5563;
    cursor: pointer;
    font-size: 14px;
    line-height: 1;
    padding: 0 0 0 4px;
    margin-left: auto;
  }

  .tab-close:hover { color: #9ca3af; }

  .tabbar-empty {
    display: flex;
    align-items: center;
    padding: 0 16px;
    color: #3d4451;
    font-size: 12px;
  }

  /* ── NOTIFICATIONS ── */
  .notify {
    padding: 8px 16px;
    font-size: 12px;
    flex-shrink: 0;
  }

  .notify.error { background: rgba(239,68,68,0.1); color: #f87171; border-bottom: 1px solid rgba(239,68,68,0.2); }
  .notify.success { background: rgba(34,197,94,0.08); color: #4ade80; border-bottom: 1px solid rgba(34,197,94,0.15); }

  /* ── EMPTY STATE ── */
  .empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    color: #3d4451;
  }

  /* ── SHARED EDITOR CHROME ── */
  .schema-editor, .query-editor {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .editor-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    border-bottom: 1px solid #1e2228;
    flex-shrink: 0;
    background: #111418;
  }

  .editor-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: #e0e4ea;
  }

  .kind-badge {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: #5aa9ff;
    background: rgba(90,169,255,0.1);
    border: 1px solid rgba(90,169,255,0.2);
    border-radius: 4px;
    padding: 2px 6px;
  }

  .toolbar-actions { display: flex; gap: 8px; }

  .btn-primary {
    display: flex;
    align-items: center;
    gap: 6px;
    background: #1a3a5c;
    border: 1px solid #2d5a8e;
    color: #90c8ff;
    border-radius: 6px;
    padding: 6px 14px;
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }

  .btn-primary:hover { background: #1f4470; border-color: #3a70b0; }

  /* ── SCHEMA GRID ── */
  .schema-grid-wrap {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .schema-grid {
    width: 100%;
    border-collapse: collapse;
    table-layout: fixed;
  }

  .schema-grid thead {
    position: sticky;
    top: 0;
    z-index: 2;
  }

  .schema-grid th {
    background: #111418;
    border-bottom: 1px solid #1e2228;
    border-right: 1px solid #1e2228;
    padding: 8px 12px;
    text-align: left;
    font-size: 11px;
    font-weight: 600;
    color: #6b7280;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    user-select: none;
  }

  .schema-grid th:last-child { border-right: none; }

  .col-name { width: 36%; }
  .col-type { width: 20%; }
  .col-check { width: 12%; text-align: center; }
  .col-del { width: 5%; }

  .schema-grid td {
    border-bottom: 1px solid #181c22;
    border-right: 1px solid #181c22;
    padding: 0;
  }

  .schema-grid td:last-child { border-right: none; }

  .schema-grid tbody tr:hover { background: rgba(255,255,255,0.025); }
  .schema-grid tbody tr.selected { background: rgba(90,169,255,0.08); }
  .schema-grid tbody tr.selected td { border-bottom-color: rgba(90,169,255,0.15); }

  .cell-input, .cell-select {
    width: 100%;
    background: transparent;
    border: none;
    color: #d4d8de;
    font: inherit;
    font-size: 13px;
    padding: 8px 12px;
    outline: none;
  }

  .cell-input:focus, .cell-select:focus {
    background: rgba(90,169,255,0.06);
    outline: none;
  }

  .cell-select { cursor: pointer; appearance: auto; }
  .cell-select option { background: #111418; }

  .col-check { text-align: center; vertical-align: middle; padding: 8px; }

  .cell-check {
    width: 15px;
    height: 15px;
    cursor: pointer;
    accent-color: #5aa9ff;
  }

  .del-row {
    background: transparent;
    border: none;
    color: #3d4451;
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    padding: 8px 10px;
    width: 100%;
  }

  .del-row:hover { color: #f87171; }

  .add-field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    border-top: 1px solid #1e2228;
    background: #111418;
    position: sticky;
    bottom: 0;
  }

  .add-field-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px dashed #2d3340;
    color: #6b7280;
    border-radius: 5px;
    padding: 5px 12px;
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }

  .add-field-btn:hover { border-color: #5aa9ff; color: #5aa9ff; }

  .field-count {
    font-size: 11px;
    color: #4b5563;
  }

  /* ── DETAIL PANEL ── */
  .detail-panel {
    border-top: 1px solid #1e2228;
    background: #111418;
    padding: 14px 16px;
    flex-shrink: 0;
  }

  .detail-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .detail-label {
    font-size: 11px;
    color: #6b7280;
    white-space: nowrap;
    width: 100px;
    text-align: right;
  }

  .detail-input {
    flex: 1;
    background: #0d0f12;
    border: 1px solid #1e2228;
    border-radius: 5px;
    color: #d4d8de;
    font: inherit;
    font-size: 12px;
    padding: 6px 10px;
    outline: none;
    max-width: 400px;
  }

  .detail-input:focus { border-color: #2d5a8e; }

  .detail-hint {
    font-size: 11px;
    color: #3d4451;
  }

  /* ── QUERY EDITOR ── */
  .query-params {
    display: flex;
    gap: 0;
    border-bottom: 1px solid #1e2228;
    flex-shrink: 0;
  }

  .param-group {
    display: flex;
    flex-direction: column;
    gap: 0;
    border-right: 1px solid #1e2228;
    padding: 12px 16px;
    flex: 1;
  }

  .param-group:last-child { border-right: none; }

  .param-group-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    color: #4b5563;
    margin-bottom: 8px;
  }

  .param-label {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
  }

  .param-label span {
    font-size: 12px;
    color: #6b7280;
    width: 80px;
    text-align: right;
    flex-shrink: 0;
  }

  .param-input {
    flex: 1;
    background: #0d0f12;
    border: 1px solid #1e2228;
    border-radius: 5px;
    color: #d4d8de;
    font: inherit;
    font-size: 12px;
    padding: 5px 9px;
    outline: none;
    min-width: 0;
  }

  .param-input:focus { border-color: #2d5a8e; }

  /* ── RESULTS ── */
  .results-area {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .results-header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    color: #4b5563;
    border-bottom: 1px solid #1e2228;
    background: #111418;
    position: sticky;
    top: 0;
    z-index: 1;
  }

  .result-count {
    font-size: 11px;
    color: #6b7280;
    font-weight: 400;
    text-transform: none;
    letter-spacing: 0;
  }

  .results-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12.5px;
  }

  .results-table th {
    background: #111418;
    border-bottom: 1px solid #1e2228;
    border-right: 1px solid #181c22;
    padding: 7px 12px;
    text-align: left;
    font-size: 11px;
    font-weight: 600;
    color: #6b7280;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .results-table th:last-child { border-right: none; }

  .results-table td {
    border-bottom: 1px solid #181c22;
    border-right: 1px solid #181c22;
    padding: 7px 12px;
  }

  .results-table td:last-child { border-right: none; }
  .results-table tbody tr:hover { background: rgba(255,255,255,0.025); }

  .mono { font-family: 'SFMono-Regular', ui-monospace, monospace; font-size: 11.5px; }

  .results-empty {
    padding: 24px 16px;
    font-size: 12px;
  }

  .plan-area {
    border-top: 1px solid #1e2228;
    flex-shrink: 0;
  }

  .plan-list {
    margin: 0;
    padding: 10px 14px 10px 30px;
    font-size: 12px;
    color: #6b7280;
  }

  .plan-list li { padding: 2px 0; }

  .muted { color: #4b5563; }

  .sidebar-btns { display: flex; gap: 2px; }

  /* ── MODAL ── */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
    backdrop-filter: blur(2px);
  }

  .modal {
    background: #141720;
    border: 1px solid #252b36;
    border-radius: 12px;
    width: 440px;
    box-shadow: 0 24px 60px rgba(0,0,0,0.5);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px 18px 14px;
    border-bottom: 1px solid #1e2228;
  }

  .modal-title {
    font-size: 14px;
    font-weight: 600;
    color: #e0e4ea;
  }

  .modal-close {
    background: transparent;
    border: none;
    color: #4b5563;
    font-size: 18px;
    cursor: pointer;
    line-height: 1;
    padding: 0 2px;
  }

  .modal-close:hover { color: #9ca3af; }

  .modal-body {
    padding: 18px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .modal-error {
    background: rgba(239,68,68,0.1);
    border: 1px solid rgba(239,68,68,0.25);
    border-radius: 6px;
    color: #f87171;
    font-size: 12px;
    padding: 8px 12px;
  }

  .modal-field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    flex: 1;
  }

  .modal-label {
    font-size: 11px;
    font-weight: 600;
    color: #6b7280;
    letter-spacing: 0.03em;
  }

  .modal-input {
    background: #0d0f12;
    border: 1px solid #252b36;
    border-radius: 6px;
    color: #d4d8de;
    font: inherit;
    font-size: 13px;
    padding: 8px 10px;
    outline: none;
    width: 100%;
  }

  .modal-input:focus { border-color: #2d5a8e; }

  .modal-section-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: #4b5563;
    padding-bottom: 2px;
    border-bottom: 1px solid #1e2228;
  }

  .modal-row {
    display: flex;
    gap: 12px;
  }

  .modal-footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding: 14px 18px;
    border-top: 1px solid #1e2228;
    background: #111418;
  }

  .btn-ghost {
    background: transparent;
    border: 1px solid #252b36;
    color: #6b7280;
    border-radius: 6px;
    padding: 7px 14px;
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }

  .btn-ghost:hover { border-color: #3d4451; color: #9ca3af; }
</style>
