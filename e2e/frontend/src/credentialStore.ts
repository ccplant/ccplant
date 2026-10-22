const DB_NAME = 'ccplant-e2e';
const DB_VERSION = 1;
const KEY_STORE = 'keys';
const CREDENTIAL_STORE = 'credentials';
const ENCRYPTION_KEY_ID = 'credential-key';

export type AuthType = 'bearer' | 'api-key';
export type ConnectionProfile = { apiURL: string; frontendURL: string; authType: 'none' | AuthType; authValue: string };
export type SavedCredential = { name: string; updatedAt: string };
type EncryptedCredential = SavedCredential & { authType?: AuthType; iv: ArrayBuffer; ciphertext: ArrayBuffer };

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(KEY_STORE)) db.createObjectStore(KEY_STORE);
      if (!db.objectStoreNames.contains(CREDENTIAL_STORE)) db.createObjectStore(CREDENTIAL_STORE, { keyPath: 'name' });
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

async function encryptionKey(db: IDBDatabase): Promise<CryptoKey> {
  const existing = await requestResult<CryptoKey | undefined>(db.transaction(KEY_STORE, 'readonly').objectStore(KEY_STORE).get(ENCRYPTION_KEY_ID));
  if (existing) return existing;
  const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
  await requestResult(db.transaction(KEY_STORE, 'readwrite').objectStore(KEY_STORE).put(key, ENCRYPTION_KEY_ID));
  return key;
}

export async function listCredentials(): Promise<SavedCredential[]> {
  const db = await openDatabase();
  try {
    const records = await requestResult<EncryptedCredential[]>(db.transaction(CREDENTIAL_STORE, 'readonly').objectStore(CREDENTIAL_STORE).getAll());
    return records.map(({ name, updatedAt }) => ({ name, updatedAt })).sort((a, b) => a.name.localeCompare(b.name, 'ja'));
  } finally { db.close(); }
}

export async function saveCredential(name: string, profile: ConnectionProfile): Promise<void> {
  const db = await openDatabase();
  try {
    const key = await encryptionKey(db);
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const additionalData = new TextEncoder().encode(name);
    const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv, additionalData }, key, new TextEncoder().encode(JSON.stringify(profile)));
    await requestResult(db.transaction(CREDENTIAL_STORE, 'readwrite').objectStore(CREDENTIAL_STORE).put({ name, updatedAt: new Date().toISOString(), iv: iv.buffer.slice(0) as ArrayBuffer, ciphertext }));
  } finally { db.close(); }
}

export async function loadCredential(name: string): Promise<ConnectionProfile> {
  const db = await openDatabase();
  try {
    const record = await requestResult<EncryptedCredential | undefined>(db.transaction(CREDENTIAL_STORE, 'readonly').objectStore(CREDENTIAL_STORE).get(name));
    if (!record) throw new Error('保存済みの認証情報が見つかりません。');
    const key = await encryptionKey(db);
    let plaintext: ArrayBuffer;
    try {
      plaintext = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: new Uint8Array(record.iv), additionalData: new TextEncoder().encode(record.name) }, key, record.ciphertext);
      return JSON.parse(new TextDecoder().decode(plaintext)) as ConnectionProfile;
    } catch (error) {
      if (!record.authType) throw error;
      plaintext = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: new Uint8Array(record.iv), additionalData: new TextEncoder().encode(`${record.name}\0${record.authType}`) }, key, record.ciphertext);
      return { apiURL: '', frontendURL: '', authType: record.authType, authValue: new TextDecoder().decode(plaintext) };
    }
  } finally { db.close(); }
}

export async function deleteCredential(name: string): Promise<void> {
  const db = await openDatabase();
  try {
    await requestResult(db.transaction(CREDENTIAL_STORE, 'readwrite').objectStore(CREDENTIAL_STORE).delete(name));
  } finally { db.close(); }
}
