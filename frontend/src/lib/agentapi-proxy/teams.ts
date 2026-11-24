import { TeamConfig, TeamMembershipState, TeamMembershipSyncResult } from '../../types/team-config';
import { ProxyUserInfo } from '../../types/user';
import type { ProxyOperationContext } from './operation-context';

export const teamsOperations = {
  /**
   * Get user info from agentapi-proxy
   * Returns the stable principal ID, display username, and team memberships.
   */
  async getUserInfo(): Promise<ProxyUserInfo> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Getting user info');
    }
    return await this.makeRequest<ProxyUserInfo>('/user/info');
  },

  async getTeamConfig(teamId: string): Promise<TeamConfig> {
    return await this.makeRequest<TeamConfig>(`/teams/${encodeURIComponent(teamId)}/config`);
  },

  async listTeamConfigs(): Promise<TeamConfig[]> {
    const response = await this.makeRequest<{ teams: TeamConfig[] }>('/teams');
    return response.teams;
  },

  async createTeam(name: string): Promise<TeamConfig> {
    return await this.makeRequest<TeamConfig>('/teams', {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
  },

  async renameTeam(teamId: string, name: string): Promise<TeamConfig> {
    return await this.makeRequest<TeamConfig>(`/teams/${encodeURIComponent(teamId)}`, {
      method: 'PATCH',
      body: JSON.stringify({ name }),
    });
  },

  async deleteTeam(teamId: string): Promise<void> {
    await this.makeRequest<void>(`/teams/${encodeURIComponent(teamId)}`, {
      method: 'DELETE',
    });
  },

  async updateTeamConfig(teamId: string, githubTeams: string[]): Promise<TeamConfig> {
    return await this.makeRequest<TeamConfig>(`/teams/${encodeURIComponent(teamId)}/config`, {
      method: 'PUT',
      body: JSON.stringify({ github_teams: githubTeams }),
    });
  },

  async getTeamMembers(teamId: string): Promise<TeamMembershipState> {
    return await this.makeRequest<TeamMembershipState>(`/teams/${encodeURIComponent(teamId)}/members`);
  },

  async syncTeamMembers(teamId: string): Promise<TeamMembershipSyncResult> {
    return await this.makeRequest<TeamMembershipSyncResult>(`/teams/${encodeURIComponent(teamId)}/members/sync`, {
      method: 'POST',
    });
  },
} satisfies ThisType<ProxyOperationContext>;

export type TeamsOperations = typeof teamsOperations;
