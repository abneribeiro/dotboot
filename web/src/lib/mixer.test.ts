import { describe, expect, it } from 'vitest';
import {
	audibleItems,
	describeItems,
	effectiveGains,
	exportItems,
	exportURL,
	newTrack,
	resamplePeaks,
	tracksParam,
	tracksToResync,
	type TrackState
} from './mixer.js';

function tracks(...specs: Partial<TrackState>[]): TrackState[] {
	return specs.map((s, i) => ({ ...newTrack(s.stem ?? `p${i}`), ...s }));
}

describe('effectiveGains', () => {
	it('sem solo nem mute, cada pista tem o seu volume', () => {
		expect(effectiveGains(tracks({ volume: 1 }, { volume: 0.5 }))).toEqual([1, 0.5]);
	});
	it('com solo, só as pistas em solo se ouvem', () => {
		expect(effectiveGains(tracks({ solo: true }, {}, { solo: true, volume: 1.5 }))).toEqual([
			1, 0, 1.5
		]);
	});
	it('o mute ganha ao solo', () => {
		expect(effectiveGains(tracks({ solo: true, muted: true }, { solo: true }))).toEqual([0, 1]);
	});
	it('o volume geral multiplica tudo', () => {
		expect(effectiveGains(tracks({ volume: 0.5 }, {}), 0.5)).toEqual([0.25, 0.5]);
	});
});

describe('tracksToResync', () => {
	it('realinha só as pistas que se afastaram mais do que o limite', () => {
		expect(tracksToResync(10, [10, 10.02, 10.05, 9.9])).toEqual([2, 3]);
	});
	it('nada a fazer quando estão juntas', () => {
		expect(tracksToResync(3, [3, 3.01, 2.99])).toEqual([]);
	});
});

describe('exportação', () => {
	const ts = tracks(
		{ stem: 'vozes', volume: 0.8 },
		{ stem: 'bateria', selected: true },
		{ stem: 'baixo', selected: true, volume: 0.5 }
	);

	it('as marcadas, com ou sem os volumes do estúdio', () => {
		expect(exportItems(ts, true)).toEqual([
			{ stem: 'bateria', gain: 1 },
			{ stem: 'baixo', gain: 0.5 }
		]);
		expect(exportItems(ts, false).map((i) => i.gain)).toEqual([1, 1]);
	});

	it('o que se ouve, com o ganho efetivo', () => {
		const solo = tracks({ stem: 'vozes' }, { stem: 'baixo', solo: true, volume: 0.7 });
		expect(audibleItems(solo)).toEqual([{ stem: 'baixo', gain: 0.7 }]);
	});

	it('o parâmetro tracks deixa o ganho 1 implícito e arredonda', () => {
		expect(
			tracksParam([
				{ stem: 'bateria', gain: 1 },
				{ stem: 'baixo', gain: 0.33333 }
			])
		).toBe('bateria,baixo:0.333');
	});

	it('o URL leva o modo, o formato e as pistas', () => {
		const url = new URL(exportURL('abc', [{ stem: 'baixo', gain: 1 }], 'wav', 'mix'), 'http://x');
		expect(url.pathname).toBe('/api/sessions/abc/export');
		expect(Object.fromEntries(url.searchParams)).toEqual({
			mode: 'mix',
			format: 'wav',
			tracks: 'baixo'
		});
	});

	it('descreve o que se vai descarregar', () => {
		expect(describeItems(exportItems(ts, true))).toBe('bateria + baixo');
		expect(describeItems([])).toBe('nenhuma pista');
	});
});

describe('resamplePeaks', () => {
	it('fica com o pico de cada intervalo', () => {
		expect(resamplePeaks([0.1, 0.9, 0.2, 0.4, 0.3, 0.3], 3)).toEqual([0.9, 0.4, 0.3]);
	});
	it('com menos picos do que barras, devolve-os tal e qual', () => {
		expect(resamplePeaks([0.1, 0.2], 10)).toEqual([0.1, 0.2]);
	});
	it('sem largura ou sem dados, nada', () => {
		expect(resamplePeaks([0.5], 0)).toEqual([]);
		expect(resamplePeaks([], 5)).toEqual([]);
	});
});
