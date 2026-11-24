import {
  CreateScheduleRequest,
  Schedule,
  ScheduleListParams,
  ScheduleListResponse,
  TriggerScheduleResponse,
  UpdateScheduleRequest,
} from '../../types/schedule';
import type { ProxyOperationContext } from './operation-context';

export const schedulesOperations = {
  /**
   * Create a new schedule
   */
  async createSchedule(data: CreateScheduleRequest): Promise<Schedule> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Creating schedule');
    }

    const schedule = await this.makeRequest<Schedule>('/schedules', {
      method: 'POST',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Created schedule: ${schedule.id}`);
    }

    return schedule;
  },

  /**
   * Get list of schedules
   */
  async getSchedules(params?: ScheduleListParams): Promise<ScheduleListResponse> {
    const searchParams = new URLSearchParams();

    if (params?.status) searchParams.set('status', params.status);
    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.scope) searchParams.set('scope', params.scope);
    if (params?.team_id) searchParams.set('team_id', params.team_id);

    const endpoint = `/schedules${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<ScheduleListResponse>(endpoint);

    return {
      ...result,
      schedules: result.schedules || [],
    };
  },

  /**
   * Get a specific schedule by ID
   */
  async getSchedule(scheduleId: string): Promise<Schedule> {
    return this.makeRequest<Schedule>(`/schedules/${scheduleId}`);
  },

  /**
   * Update a schedule
   */
  async updateSchedule(scheduleId: string, data: UpdateScheduleRequest): Promise<Schedule> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Updating schedule ${scheduleId}`);
    }

    const schedule = await this.makeRequest<Schedule>(`/schedules/${scheduleId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Updated schedule: ${schedule.id}`);
    }

    return schedule;
  },

  /**
   * Delete a schedule
   */
  async deleteSchedule(scheduleId: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting schedule: ${scheduleId}`);
    }

    await this.makeRequest<void>(`/schedules/${scheduleId}`, {
      method: 'DELETE',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleted schedule: ${scheduleId}`);
    }
  },

  /**
   * Trigger a schedule immediately
   */
  async triggerSchedule(scheduleId: string): Promise<TriggerScheduleResponse> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Triggering schedule: ${scheduleId}`);
    }

    const result = await this.makeRequest<TriggerScheduleResponse>(`/schedules/${scheduleId}/trigger`, {
      method: 'POST',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Triggered schedule ${scheduleId}, session: ${result.session_id}`);
    }

    return result;
  },
} satisfies ThisType<ProxyOperationContext>;

export type SchedulesOperations = typeof schedulesOperations;
