import * as duckdb from '@duckdb/duckdb-wasm'

let databasePromise: Promise<duckdb.AsyncDuckDB> | null = null

async function createDatabase(): Promise<duckdb.AsyncDuckDB> {
  const bundles = duckdb.getJsDelivrBundles()
  const bundle = await duckdb.selectBundle(bundles)
  if (!bundle.mainWorker) throw new Error('DuckDB-Wasm worker is unavailable')

  const workerUrl = URL.createObjectURL(new Blob([
    `importScripts(${JSON.stringify(bundle.mainWorker)});`,
  ], { type: 'text/javascript' }))
  const worker = new Worker(workerUrl)
  const database = new duckdb.AsyncDuckDB(new duckdb.ConsoleLogger(), worker)
  await database.instantiate(bundle.mainModule, bundle.pthreadWorker)
  URL.revokeObjectURL(workerUrl)
  return database
}

function getDatabase(): Promise<duckdb.AsyncDuckDB> {
  if (typeof window === 'undefined') throw new Error('DuckDB-Wasm is only available in the browser')
  databasePromise ??= createDatabase()
  return databasePromise
}

function normalizeValue(value: unknown): unknown {
  if (typeof value === 'bigint') return Number(value)
  return value
}

/**
 * Downloads an authorized usage Parquet export and runs local SQL against a
 * temporary `usage_events` view. SQL executes only inside the browser's
 * DuckDB-Wasm instance; it is never sent to agentapi-proxy.
 */
export async function queryUsageParquet<T extends Record<string, unknown>>(
  exportUrl: string,
  sql: string,
): Promise<T[]> {
  const response = await fetch(exportUrl, { credentials: 'include' })
  if (!response.ok) throw new Error(`Usage export failed: ${response.status}`)

  const database = await getDatabase()
  const fileName = `usage-${crypto.randomUUID()}.parquet`
  await database.registerFileBuffer(fileName, new Uint8Array(await response.arrayBuffer()))
  const connection = await database.connect()
  try {
    await connection.query(`CREATE OR REPLACE TEMP VIEW usage_events AS SELECT * FROM read_parquet('${fileName}')`)
    const result = await connection.query(sql)
    const fields = result.schema.fields.map((field) => field.name)
    return result.toArray().map((row) => Object.fromEntries(
      fields.map((field) => [field, normalizeValue(row[field])]),
    ) as T)
  } finally {
    await connection.close()
    await database.dropFile(fileName)
  }
}
