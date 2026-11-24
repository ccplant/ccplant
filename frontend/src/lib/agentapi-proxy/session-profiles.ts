import { ResourceScope } from '../../types/agentapi';
import {
  CreateSessionProfileRequest,
  SessionProfile,
  SessionProfileListParams,
  SessionProfileListResponse,
  UpdateSessionProfileRequest,
} from '../../types/session_profile';
import type { ProxyOperationContext } from './operation-context';

export const sessionProfilesOperations = {
  /**
   * Create a new Session Profile
   */
  async createSessionProfile(data: CreateSessionProfileRequest): Promise<SessionProfile> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Creating session profile');
    }

    const profile = await this.makeRequest<SessionProfile>('/session-profiles', {
      method: 'POST',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Created session profile: ${profile.id}`);
    }

    return profile;
  },

  /**
   * Get list of Session Profiles
   */
  async getSessionProfiles(params?: SessionProfileListParams): Promise<SessionProfileListResponse> {
    const searchParams = new URLSearchParams();

    if (params?.scope) searchParams.set('scope', params.scope);
    if (params?.team_id) searchParams.set('team_id', params.team_id);
    if (params?.include_available_team_profiles) searchParams.set('include_available_team_profiles', 'true');

    const endpoint = `/session-profiles${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<SessionProfile[] | SessionProfileListResponse>(endpoint);

    // API may return array directly (not wrapped in object)
    if (Array.isArray(result)) {
      return { session_profiles: result };
    }

    return {
      ...result,
      session_profiles: result.session_profiles || [],
    };
  },

  /**
   * Preview which session profile /start would resolve for the given tags.
   * Returns the matched profile and the source that selected it.
   */
  async previewSessionProfile(params: {
    tags?: Record<string, string>;
    scope?: ResourceScope;
    team_id?: string;
  }): Promise<{
    source: 'selector_tags' | 'settings_default' | 'profile_default' | 'none';
    profile: SessionProfile | null;
  }> {
    return this.makeRequest('/session-profiles/preview', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },

  /**
   * Get a specific Session Profile by ID
   */
  async getSessionProfile(profileId: string): Promise<SessionProfile> {
    return this.makeRequest<SessionProfile>(`/session-profiles/${profileId}`);
  },

  /**
   * Update a Session Profile
   */
  async updateSessionProfile(profileId: string, data: UpdateSessionProfileRequest): Promise<SessionProfile> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Updating session profile ${profileId}`);
    }

    const profile = await this.makeRequest<SessionProfile>(`/session-profiles/${profileId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Updated session profile: ${profile.id}`);
    }

    return profile;
  },

  /**
   * Delete a Session Profile
   */
  async deleteSessionProfile(profileId: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting session profile: ${profileId}`);
    }

    await this.makeRequest<void>(`/session-profiles/${profileId}`, {
      method: 'DELETE',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleted session profile: ${profileId}`);
    }
  },
} satisfies ThisType<ProxyOperationContext>;

export type SessionProfilesOperations = typeof sessionProfilesOperations;
