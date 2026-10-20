package entities

import (
	"errors"
	"path"
	"strings"
	"time"
)

// StorageKind identifies the data-plane implementation used by a persistent
// storage resource.
type StorageKind string

const (
	StorageKindObjectSnapshot StorageKind = "object_snapshot"
)

// StorageState describes whether a storage can be attached to a session.
type StorageState string

const (
	StorageStatePending  StorageState = "pending"
	StorageStateReady    StorageState = "ready"
	StorageStateDeleting StorageState = "deleting"
	StorageStateError    StorageState = "error"
)

// ErrStorageNotFound is returned when a storage resource does not exist.
type ErrStorageNotFound struct{ ID string }

func (e ErrStorageNotFound) Error() string { return "storage not found: " + e.ID }

// StorageFile is the current last-write-wins entry for one relative path.
// Deleted entries are retained as tombstones so an older writer cannot
// accidentally resurrect a file while applying a stale batch.
type StorageFile struct {
	Path      string    `json:"path"`
	Digest    string    `json:"digest,omitempty"`
	Size      int64     `json:"size,omitempty"`
	Mode      uint32    `json:"mode,omitempty"`
	Deleted   bool      `json:"deleted,omitempty"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Storage is a scope-owned persistent filesystem shared by sessions.
type Storage struct {
	id          string
	name        string
	description string
	ownerID     string
	scope       ResourceScope
	teamID      string
	kind        StorageKind
	pool        string
	provider    string
	size        string
	state       StorageState
	revision    int64
	files       map[string]StorageFile
	createdAt   time.Time
	updatedAt   time.Time
	version     string
}

func NewStorage(id, name, description, ownerID string, scope ResourceScope, teamID string, kind StorageKind, pool, provider, size string) *Storage {
	now := time.Now().UTC()
	return &Storage{
		id: id, name: name, description: description, ownerID: ownerID,
		scope: scope, teamID: teamID, kind: kind, pool: pool, provider: provider,
		size: size, state: StorageStateReady, files: make(map[string]StorageFile),
		createdAt: now, updatedAt: now,
	}
}

func (s *Storage) ID() string                        { return s.id }
func (s *Storage) Name() string                      { return s.name }
func (s *Storage) Description() string               { return s.description }
func (s *Storage) OwnerID() string                   { return s.ownerID }
func (s *Storage) Scope() ResourceScope              { return s.scope }
func (s *Storage) TeamID() string                    { return s.teamID }
func (s *Storage) Kind() StorageKind                 { return s.kind }
func (s *Storage) Pool() string                      { return s.pool }
func (s *Storage) Provider() string                  { return s.provider }
func (s *Storage) Size() string                      { return s.size }
func (s *Storage) State() StorageState               { return s.state }
func (s *Storage) Revision() int64                   { return s.revision }
func (s *Storage) CreatedAt() time.Time              { return s.createdAt }
func (s *Storage) UpdatedAt() time.Time              { return s.updatedAt }
func (s *Storage) Version() string                   { return s.version }
func (s *Storage) SetCreatedAt(v time.Time)          { s.createdAt = v }
func (s *Storage) SetUpdatedAt(v time.Time)          { s.updatedAt = v }
func (s *Storage) SetRevision(v int64)               { s.revision = v }
func (s *Storage) SetVersion(v string)               { s.version = v }
func (s *Storage) SetState(v StorageState)           { s.state, s.updatedAt = v, time.Now().UTC() }
func (s *Storage) SetName(v string)                  { s.name, s.updatedAt = v, time.Now().UTC() }
func (s *Storage) SetDescription(v string)           { s.description, s.updatedAt = v, time.Now().UTC() }
func (s *Storage) SetSize(v string)                  { s.size, s.updatedAt = v, time.Now().UTC() }
func (s *Storage) SetFiles(v map[string]StorageFile) { s.files = copyStorageFiles(v) }

// Files returns a copy of the current manifest, including tombstones.
func (s *Storage) Files() map[string]StorageFile { return copyStorageFiles(s.files) }

func (s *Storage) File(filePath string) (StorageFile, bool) {
	entry, ok := s.files[filePath]
	return entry, ok
}

// ApplyFiles commits one last-write-wins batch under a single server-assigned
// revision. The repository update is the serialization point.
func (s *Storage) ApplyFiles(files []StorageFile) int64 {
	s.revision++
	now := time.Now().UTC()
	if s.files == nil {
		s.files = make(map[string]StorageFile)
	}
	for _, file := range files {
		file.Revision = s.revision
		file.UpdatedAt = now
		s.files[file.Path] = file
	}
	s.updatedAt = now
	return s.revision
}

func (s *Storage) SetOwnership(scope ResourceScope, ownerID, teamID string) {
	s.scope, s.ownerID = scope, ownerID
	if scope == ScopeTeam {
		s.teamID = teamID
	} else {
		s.teamID = ""
	}
	s.updatedAt = time.Now().UTC()
}

func (s *Storage) Validate() error {
	if s.id == "" || s.name == "" || s.ownerID == "" {
		return errors.New("storage id, name, and owner_id are required")
	}
	if s.scope != ScopeUser && s.scope != ScopeTeam {
		return errors.New("storage scope must be 'user' or 'team'")
	}
	if s.scope == ScopeTeam && s.teamID == "" {
		return errors.New("storage team_id is required when scope is 'team'")
	}
	if s.kind != StorageKindObjectSnapshot {
		return errors.New("storage kind must be 'object_snapshot'")
	}
	return nil
}

// ValidateStoragePath accepts only normalized relative paths.
func ValidateStoragePath(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\\') {
		return errors.New("storage path must be a non-empty relative path")
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("storage path must be normalized and remain inside the storage root")
	}
	if clean == ".ccplant-storage" || strings.HasPrefix(clean, ".ccplant-storage/") {
		return errors.New("storage path uses a reserved prefix")
	}
	return nil
}

func copyStorageFiles(src map[string]StorageFile) map[string]StorageFile {
	if src == nil {
		return map[string]StorageFile{}
	}
	dst := make(map[string]StorageFile, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
