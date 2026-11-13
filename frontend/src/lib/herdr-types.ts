export interface HerdrTarget {
  server_id: string;
  pane_id: string;
  generation: string;
}

export interface HerdrAgent extends HerdrTarget {
  server_name: string;
  runtime: 'herdr' | 'core';
  terminal_id: string;
  agent: string;
  status: string;
  label: string;
  session_id?: string;
  can_input: boolean;
}