import { describe, expect, it } from 'vitest'
import { validateStorageAttachments } from '../StorageAttachmentEditor'

describe('storage mount validation', () => {
  it.each(['/', 'relative/path', '/data/../other'])('rejects invalid path %s', path => {
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: path }])).not.toBeNull()
  })
  it('rejects overlapping normalized paths and duplicate IDs', () => {
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: '/data//' }, { storage_id: 'b', mount_path: '/data/files' }])).not.toBeNull()
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: '/data' }, { storage_id: 'a', mount_path: '/other' }])).not.toBeNull()
  })
  it('accepts independent mounts', () => {
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: '/data' }, { storage_id: 'b', mount_path: '/database' }])).toBeNull()
  })
})
