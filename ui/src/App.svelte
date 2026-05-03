<script lang="ts">
  import { onMount } from 'svelte'
  import { LocationDBClient, type StoreDefinition, type QueryResponse } from '@ruckstack/locationdb'

  const client = new LocationDBClient('')

  let stores: StoreDefinition[] = []
  let selectedStore = ''
  let loading = true
  let error = ''
  let queryResponse: QueryResponse | null = null

  let lat = 43.65
  let lon = -79.38
  let radius = 2000
  let labels = 'restaurant'
  let validAt = '2026-05-03T12:00:00Z'
  let limit = 50

  onMount(async () => {
    await refreshStores()
  })

  async function refreshStores() {
    loading = true
    error = ''
    try {
      const response = await client.listStores()
      stores = response.stores
      if (!selectedStore && stores.length > 0) {
        selectedStore = stores[0].config.name
      }
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load stores'
    } finally {
      loading = false
    }
  }

  async function runQuery() {
    if (!selectedStore) return
    error = ''
    try {
      queryResponse = await client.query(selectedStore, {
        near: { lat, lon, radius },
        labels: labels.split(',').map((value) => value.trim()).filter(Boolean),
        valid_at: validAt || undefined,
        limit
      })
    } catch (err) {
      error = err instanceof Error ? err.message : 'Query failed'
    }
  }
</script>

<div class="shell">
  <aside class="sidebar">
    <div class="sidebar-header">
      <h1>locationdb</h1>
      <button on:click={refreshStores}>Refresh</button>
    </div>

    {#if loading}
      <p class="muted">Loading stores...</p>
    {:else if stores.length === 0}
      <p class="muted">No stores configured yet.</p>
    {:else}
      <ul class="store-list">
        {#each stores as store}
          <li>
            <button class:selected={selectedStore === store.config.name} on:click={() => (selectedStore = store.config.name)}>
              <span>{store.config.name}</span>
              <small>{store.config.root_path}</small>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </aside>

  <main class="workspace">
    <header class="toolbar">
      <div>
        <h2>Query Console</h2>
        <p>Run near queries against the selected store.</p>
      </div>
      <button disabled={!selectedStore} on:click={runQuery}>Run Query</button>
    </header>

    <section class="panel form-grid">
      <label>
        <span>Store</span>
        <select bind:value={selectedStore}>
          <option value="">Select a store</option>
          {#each stores as store}
            <option value={store.config.name}>{store.config.name}</option>
          {/each}
        </select>
      </label>
      <label>
        <span>Latitude</span>
        <input type="number" bind:value={lat} step="0.0001" />
      </label>
      <label>
        <span>Longitude</span>
        <input type="number" bind:value={lon} step="0.0001" />
      </label>
      <label>
        <span>Radius (m)</span>
        <input type="number" bind:value={radius} step="100" />
      </label>
      <label>
        <span>Labels</span>
        <input type="text" bind:value={labels} placeholder="restaurant, cafe" />
      </label>
      <label>
        <span>Valid At</span>
        <input type="text" bind:value={validAt} placeholder="2026-05-03T12:00:00Z" />
      </label>
      <label>
        <span>Limit</span>
        <input type="number" bind:value={limit} min="1" />
      </label>
    </section>

    {#if error}
      <section class="panel error">{error}</section>
    {/if}

    <section class="panel">
      <div class="panel-header">
        <h3>Results</h3>
      </div>

      {#if queryResponse?.results?.length}
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Code</th>
              <th>Distance</th>
              <th>Labels</th>
            </tr>
          </thead>
          <tbody>
            {#each queryResponse.results as result}
              <tr>
                <td>{result.record.id}</td>
                <td>{result.record.code}</td>
                <td>{Math.round(result.distance_meters)}m</td>
                <td>{result.record.labels?.join(', ')}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="muted">No query results yet.</p>
      {/if}
    </section>

    <section class="panel plan-panel">
      <div class="panel-header">
        <h3>Execution Plan</h3>
      </div>
      {#if queryResponse}
        <ol>
          {#each queryResponse.plan.strategy as step}
            <li>{step}</li>
          {/each}
        </ol>
      {:else}
        <p class="muted">Run a query to inspect the execution plan.</p>
      {/if}
    </section>
  </main>
</div>

<style>
  .shell {
    display: grid;
    grid-template-columns: 320px 1fr;
    min-height: 100vh;
  }

  .sidebar {
    border-right: 1px solid rgba(255,255,255,0.08);
    background: rgba(12, 14, 18, 0.92);
    padding: 20px;
  }

  .sidebar-header, .toolbar, .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .workspace {
    padding: 24px;
    display: grid;
    gap: 18px;
  }

  .panel {
    background: rgba(255,255,255,0.03);
    border: 1px solid rgba(255,255,255,0.08);
    border-radius: 14px;
    padding: 18px;
  }

  .form-grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
  }

  label {
    display: grid;
    gap: 8px;
  }

  input, select, button {
    background: rgba(255,255,255,0.04);
    color: inherit;
    border: 1px solid rgba(255,255,255,0.1);
    border-radius: 10px;
    padding: 10px 12px;
  }

  button.selected {
    border-color: #5aa9ff;
  }

  .store-list {
    list-style: none;
    padding: 0;
    margin: 20px 0 0;
    display: grid;
    gap: 10px;
  }

  .store-list button {
    width: 100%;
    text-align: left;
    display: grid;
    gap: 4px;
  }

  .muted, small {
    color: rgba(230, 232, 235, 0.65);
  }

  table {
    width: 100%;
    border-collapse: collapse;
  }

  th, td {
    padding: 10px 8px;
    border-bottom: 1px solid rgba(255,255,255,0.08);
    text-align: left;
  }

  .error {
    color: #ff8f8f;
    border-color: rgba(255, 106, 106, 0.35);
  }

  @media (max-width: 980px) {
    .shell {
      grid-template-columns: 1fr;
    }

    .form-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
