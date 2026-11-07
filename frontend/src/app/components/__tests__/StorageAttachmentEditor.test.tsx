import { describe, expect, it } from 'vitest'
import { validateStorageAttachments } from '../StorageAttachmentEditor'

describe('storage mount validation', () => {
  it.each(['/', '../relative/path', '/data/../other'])('rejects invalid path %s', path => {
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: path }])).not.toBeNull()
  })
  it('rejects overlapping target and source paths', () => {
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: '/data//' }, { storage_id: 'b', mount_path: '/data/files' }])).not.toBeNull()
    expect(validateStorageAttachments([{ storage_id: 'a', storage_path: 'state', mount_path: '/data' }, { storage_id: 'a', storage_path: 'state/sub', mount_path: '/other' }])).not.toBeNull()
  })
  it('accepts independent mounts', () => {
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: '/data' }, { storage_id: 'b', mount_path: '/database' }])).toBeNull()
    expect(validateStorageAttachments([{ storage_id: 'a', storage_path: 'codex', mount_path: '/codex' }, { storage_id: 'a', storage_path: 'claude', mount_path: '/claude' }])).toBeNull()
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: 'workdir/shared' }, { storage_id: 'b', mount_path: 'cache' }])).toBeNull()
  })

  it('checks relative paths after resolving them below HOME', () => {
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: 'workdir/shared' }, { storage_id: 'b', mount_path: '/home/agentapi/workdir/shared/files' }])).not.toBeNull()
    expect(validateStorageAttachments([{ storage_id: 'a', mount_path: 'workdir' }])).not.toBeNull()
  })
})
