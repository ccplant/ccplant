package controllers

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (c *SettingsController) applyMCPSettings(settings *entities.Settings, req *UpdateSettingsRequest) {
	if req.MCPServers == nil {
		return
	}
	existing := settings.MCPServers()
	servers := entities.NewMCPServersSettings()
	for name, request := range req.MCPServers {
		var previous *entities.MCPServer
		if existing != nil {
			previous = existing.GetServer(name)
		}
		servers.SetServer(name, c.mergeMCPServer(name, request, previous))
	}
	settings.SetMCPServers(servers)
}

func (c *SettingsController) mergeMCPServer(name string, req *MCPServerRequest, previous *entities.MCPServer) *entities.MCPServer {
	server := entities.NewMCPServer(name, req.Type)
	server.SetURL(req.URL)
	server.SetCommand(req.Command)
	server.SetArgs(req.Args)
	env, headers := req.Env, req.Headers
	if previous != nil {
		env = c.mergeSecrets(previous.Env(), env)
		headers = c.mergeSecrets(previous.Headers(), headers)
	}
	server.SetEnv(env)
	server.SetHeaders(headers)
	return server
}

func (c *SettingsController) applyMarketplaceSettings(settings *entities.Settings, req *UpdateSettingsRequest) {
	// Update Marketplaces settings
	if req.Marketplaces != nil {
		marketplaces := entities.NewMarketplacesSettings()
		for marketplaceName, marketplaceReq := range req.Marketplaces {
			marketplace := entities.NewMarketplace(marketplaceName)
			marketplace.SetURL(marketplaceReq.URL)
			marketplaces.SetMarketplace(marketplaceName, marketplace)
		}
		settings.SetMarketplaces(marketplaces)
	}
}
