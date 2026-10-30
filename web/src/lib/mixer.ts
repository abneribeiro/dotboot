// Lógica pura do estúdio: o que se ouve (mute, solo, volume), a correção da
// sincronização entre pistas e o que se pede ao servidor para exportar.

export interface TrackState {
	stem: string;
	/** 0 a 2 (o fader vai até 200%). */
	volume: number;
	muted: boolean;
	solo: boolean;
	/** Marcada para exportar. */
	selected: boolean;
}

export function newTrack(stem: string): TrackState {
	return { stem, volume: 1, muted: false, solo: false, selected: false };
}

/**
 * O ganho que cada pista tem de ter agora. Com alguma pista em solo, só as
 * pistas em solo se ouvem; o mute ganha sempre ao solo (como nas DAWs).
 */
export function effectiveGains(tracks: TrackState[], master = 1): number[] {
	const anySolo = tracks.some((t) => t.solo);
	return tracks.map((t) => {
		if (t.muted) return 0;
		if (anySolo && !t.solo) return 0;
		return t.volume * master;
	});
}

/** As pistas que se estão a ouvir (ganho acima de zero). */
export function audibleTracks(tracks: TrackState[]): TrackState[] {
	const gains = effectiveGains(tracks);
	return tracks.filter((_, i) => gains[i] > 0);
}

/** O desvio acima do qual uma pista é realinhada com a mestra, em segundos. */
export const DRIFT_THRESHOLD = 0.04;

/**
 * Quais pistas precisam de ser realinhadas com o tempo da mestra. Cada
 * <audio> tem o seu relógio; à volta de 40 ms o desvio começa a ouvir-se
 * como um eco ou um flam na bateria.
 */
export function tracksToResync(
	masterTime: number,
	times: number[],
	threshold = DRIFT_THRESHOLD
): number[] {
	const out: number[] = [];
	times.forEach((t, i) => {
		if (Math.abs(t - masterTime) > threshold) out.push(i);
	});
	return out;
}

export type ExportFormat = 'wav' | 'flac' | 'mp3';
export type ExportMode = 'mix' | 'zip';

export interface ExportItem {
	stem: string;
	gain: number;
}

/** Arredonda o ganho como o servidor o escreve no filtro (3 casas). */
function roundGain(g: number): number {
	return Math.round(g * 1000) / 1000;
}

/**
 * O parâmetro tracks do /export: "baixo:0.5,bateria". O ganho 1 vai
 * implícito.
 */
export function tracksParam(items: ExportItem[]): string {
	return items
		.map(({ stem, gain }) => {
			const g = roundGain(gain);
			return g === 1 ? stem : `${stem}:${g}`;
		})
		.join(',');
}

/** O URL de uma exportação. */
export function exportURL(
	session: string,
	items: ExportItem[],
	format: ExportFormat,
	mode: ExportMode
): string {
	const q = new URLSearchParams({ mode, format, tracks: tracksParam(items) });
	return `/api/sessions/${session}/export?${q}`;
}

/**
 * O que exportar a partir das pistas marcadas. Com withVolumes, cada pista
 * leva o volume do fader (o que se ouve no estúdio); sem ele, sai com o
 * volume original da separação.
 */
export function exportItems(tracks: TrackState[], withVolumes: boolean): ExportItem[] {
	return tracks
		.filter((t) => t.selected)
		.map((t) => ({ stem: t.stem, gain: withVolumes ? t.volume : 1 }));
}

/** Os mesmos itens a partir do que se está a ouvir agora. */
export function audibleItems(tracks: TrackState[]): ExportItem[] {
	const gains = effectiveGains(tracks);
	return tracks.map((t, i) => ({ stem: t.stem, gain: gains[i] })).filter((item) => item.gain > 0);
}

/** "baixo + bateria", para os botões e as mensagens. */
export function describeItems(items: ExportItem[]): string {
	if (items.length === 0) return 'nenhuma pista';
	return items.map((i) => i.stem).join(' + ');
}

/** Cores de cada pista, como nas DAWs: a mesma pista tem sempre a mesma cor. */
export const STEM_COLORS: Record<string, string> = {
	vozes: '#f472b6',
	bateria: '#fb923c',
	baixo: '#818cf8',
	guitarra: '#facc15',
	piano: '#2dd4bf',
	outros: '#94a3b8',
	instrumental: '#4ade80'
};

export function stemColor(stem: string): string {
	return STEM_COLORS[stem] ?? '#a78bfa';
}

/** Nome com maiúscula, para mostrar: "baixo" → "Baixo". */
export function stemLabel(stem: string): string {
	return stem.charAt(0).toUpperCase() + stem.slice(1);
}

/**
 * Reduz a forma de onda a n barras (o pico de cada intervalo), para a
 * largura do canvas.
 */
export function resamplePeaks(values: number[], n: number): number[] {
	if (n <= 0 || values.length === 0) return [];
	if (values.length <= n) return values.slice();
	const out = new Array<number>(n);
	for (let i = 0; i < n; i++) {
		const from = Math.floor((i * values.length) / n);
		const to = Math.max(from + 1, Math.floor(((i + 1) * values.length) / n));
		let peak = 0;
		for (let j = from; j < to; j++) peak = Math.max(peak, values[j]);
		out[i] = peak;
	}
	return out;
}
