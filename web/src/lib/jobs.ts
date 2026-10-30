// O estado da fila na página, atualizado pelas mensagens do /api/events.
// Função pura: o events.svelte.ts aplica-a ao estado reativo.

import type { Job } from './api.js';

export type ServerMessage =
	| { type: 'jobs'; jobs?: Job[] }
	| { type: 'job'; job: Job }
	| { type: 'log'; id: string; line: string }
	| { type: 'removed'; id: string }
	| { type: 'library' };

/** Lê uma mensagem do SSE; devolve undefined se não for válida. */
export function parseMessage(data: string): ServerMessage | undefined {
	try {
		const m = JSON.parse(data);
		if (m && typeof m.type === 'string') return m as ServerMessage;
	} catch {
		// uma linha estragada não pode partir a página
	}
	return undefined;
}

/** Aplica uma mensagem à lista de trabalhos (pela ordem de criação). */
export function applyMessage(jobs: Job[], m: ServerMessage): Job[] {
	switch (m.type) {
		case 'jobs':
			return m.jobs ?? [];
		case 'job': {
			const i = jobs.findIndex((j) => j.id === m.job.id);
			if (i < 0) return [...jobs, m.job];
			const next = jobs.slice();
			next[i] = m.job;
			return next;
		}
		case 'removed':
			return jobs.filter((j) => j.id !== m.id);
		default:
			return jobs;
	}
}

export function isActive(j: Job): boolean {
	return j.status === 'queued' || j.status === 'running';
}

export const KIND_LABEL: Record<Job['kind'], string> = {
	video: 'Vídeo',
	audio: 'Música',
	stems: 'Separação'
};

export const STATUS_LABEL: Record<Job['status'], string> = {
	queued: 'em fila',
	running: 'a correr',
	done: 'concluído',
	failed: 'falhou',
	canceled: 'cancelado'
};
