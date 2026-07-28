package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const InitialMessageHistoryLimit = 40
const InitialMessageHistoryMaxContentBytes = 20 * 1024

// InitialMessageHistoryService owns the history policy shared by REST and ACP session creation.
type InitialMessageHistoryService struct {
	repo repositories.InitialMessageHistoryRepository
}

func NewInitialMessageHistoryService(repo repositories.InitialMessageHistoryRepository) *InitialMessageHistoryService {
	return &InitialMessageHistoryService{repo: repo}
}

func (s *InitialMessageHistoryService) Record(ctx context.Context, userID, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if len(content) > InitialMessageHistoryMaxContentBytes {
		return fmt.Errorf("initial message exceeds history storage limit")
	}
	_, err := s.repo.UpsertAndTrim(ctx, userID, content, InitialMessageHistoryLimit)
	return err
}

func (s *InitialMessageHistoryService) List(ctx context.Context, userID string, limit int) ([]entities.InitialMessageHistoryItem, error) {
	if limit <= 0 || limit > InitialMessageHistoryLimit {
		limit = InitialMessageHistoryLimit
	}
	return s.repo.List(ctx, userID, limit)
}

func (s *InitialMessageHistoryService) DeleteAll(ctx context.Context, userID string) error {
	return s.repo.DeleteAll(ctx, userID)
}
