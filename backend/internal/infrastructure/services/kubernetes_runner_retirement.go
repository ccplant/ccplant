package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

func (m *KubernetesSessionManager) retireStockRunner(ctx context.Context, id string) (bool, error) {
	m.mutex.RLock()
	parent, manager, token := m.runnerParentURL, m.runnerManagerID, m.runnerManagerToken
	m.mutex.RUnlock()
	if parent == "" {
		return true, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parent+"/internal/session-managers/"+url.PathEscape(manager)+"/runners/"+url.PathEscape(id)+"/retire", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := instrumentedHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusConflict {
		return false, nil
	}
	if resp.StatusCode != http.StatusNoContent {
		return false, fmt.Errorf("parent retirement returned HTTP %d", resp.StatusCode)
	}
	return true, nil
}
