// Ligação única ao /api/events, partilhada por todas as páginas: a lista de
// trabalhos, as linhas de log que chegam e um contador que muda sempre que a
// biblioteca muda (as páginas voltam a pedir a lista quando ele muda).

import type { Job } from './api.js';
import { applyMessage, isActive, parseMessage } from './jobs.js';

/** Linhas de log guardadas por trabalho (o servidor guarda as mesmas). */
const MAX_LOG_LINES = 500;

class Live {
	jobs = $state<Job[]>([]);
	connected = $state(false);
	/** Muda sempre que uma sessão ou um download aparece ou desaparece. */
	library = $state(0);
	logs = $state<Record<string, string[]>>({});

	#source: EventSource | undefined;

	get running(): number {
		return this.jobs.filter(isActive).length;
	}

	connect() {
		if (this.#source) return;
		const source = new EventSource('/api/events');
		this.#source = source;
		source.onopen = () => (this.connected = true);
		// O EventSource volta a ligar-se sozinho; ao voltar, a primeira
		// mensagem é o estado completo.
		source.onerror = () => (this.connected = false);
		source.onmessage = (e) => {
			const m = parseMessage(e.data);
			if (!m) return;
			if (m.type === 'log') {
				const lines = this.logs[m.id];
				if (lines) {
					lines.push(m.line);
					if (lines.length > MAX_LOG_LINES) lines.splice(0, lines.length - MAX_LOG_LINES);
				}
				return;
			}
			if (m.type === 'library') {
				this.library++;
				return;
			}
			this.jobs = applyMessage(this.jobs, m);
		};
	}

	/** Começa a guardar o log de um trabalho, a partir das linhas já pedidas. */
	watchLog(id: string, initial: string[]) {
		this.logs[id] = initial.slice(-MAX_LOG_LINES);
	}
}

export const live = new Live();
