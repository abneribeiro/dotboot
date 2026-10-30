// Formatação para humanos, em português.

/** 0:42, 3:05, 1:02:09. Com décimas para o relógio do estúdio. */
export function formatTime(seconds: number, tenths = false): string {
	if (!Number.isFinite(seconds) || seconds < 0) seconds = 0;
	const whole = Math.floor(seconds);
	const h = Math.floor(whole / 3600);
	const m = Math.floor((whole % 3600) / 60);
	const s = whole % 60;
	const mm = h > 0 ? String(m).padStart(2, '0') : String(m);
	let out = `${h > 0 ? `${h}:` : ''}${mm}:${String(s).padStart(2, '0')}`;
	if (tenths) out += `.${Math.floor((seconds - whole) * 10)}`;
	return out;
}

/** 512 B, 2.0 KB, 3.5 MB. */
export function formatBytes(bytes: number): string {
	if (!Number.isFinite(bytes) || bytes < 1024) return `${Math.max(0, Math.round(bytes || 0))} B`;
	const units = ['KB', 'MB', 'GB', 'TB'];
	let value = bytes;
	let unit = -1;
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024;
		unit++;
	}
	return `${value.toFixed(1)} ${units[unit]}`;
}

/** Tempo restante: "falta 0:42", ou vazio quando é desconhecido. */
export function formatETA(seconds: number): string {
	if (!Number.isFinite(seconds) || seconds < 0) return '';
	return `falta ${formatTime(seconds)}`;
}

/** "há 5 min", "há 2 h", "ontem", "12/10". */
export function formatAgo(iso: string, now = Date.now()): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) return '';
	const s = Math.max(0, (now - t) / 1000);
	if (s < 60) return 'agora mesmo';
	if (s < 3600) return `há ${Math.floor(s / 60)} min`;
	if (s < 86400) return `há ${Math.floor(s / 3600)} h`;
	if (s < 2 * 86400) return 'ontem';
	const d = new Date(t);
	return `${String(d.getDate()).padStart(2, '0')}/${String(d.getMonth() + 1).padStart(2, '0')}`;
}

/** Estimativa do tempo de separação: duração × velocidade do modelo. */
export function estimateSeparation(durationSeconds: number, speed: number): string {
	if (!durationSeconds || !speed) return '';
	const total = durationSeconds * speed;
	if (total < 60) return 'menos de 1 min';
	return `~${Math.round(total / 60)} min`;
}
