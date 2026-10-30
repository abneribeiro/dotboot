import { describe, expect, it } from 'vitest';
import { estimateSeparation, formatAgo, formatBytes, formatETA, formatTime } from './format.js';

describe('formatTime', () => {
	it.each([
		[0, '0:00'],
		[42.9, '0:42'],
		[185, '3:05'],
		[3729, '1:02:09'],
		[-3, '0:00'],
		[Number.NaN, '0:00']
	])('%s s → %s', (s, want) => expect(formatTime(s)).toBe(want));
	it('com décimas para o relógio do estúdio', () => expect(formatTime(83.47, true)).toBe('1:23.4'));
});

describe('formatBytes', () => {
	it.each([
		[512, '512 B'],
		[2048, '2.0 KB'],
		[3.5 * 1024 * 1024, '3.5 MB'],
		[5 * 1024 ** 3, '5.0 GB']
	])('%s → %s', (b, want) => expect(formatBytes(b)).toBe(want));
});

describe('formatETA', () => {
	it('desconhecido fica vazio', () => expect(formatETA(-1)).toBe(''));
	it('conhecido', () => expect(formatETA(42)).toBe('falta 0:42'));
});

describe('formatAgo', () => {
	const now = Date.parse('2026-10-30T12:00:00Z');
	it.each([
		['2026-10-30T11:59:30Z', 'agora mesmo'],
		['2026-10-30T11:55:00Z', 'há 5 min'],
		['2026-10-30T09:00:00Z', 'há 3 h'],
		['2026-10-29T10:00:00Z', 'ontem']
	])('%s → %s', (iso, want) => expect(formatAgo(iso, now)).toBe(want));
});

describe('estimateSeparation', () => {
	it('curto', () => expect(estimateSeparation(30, 1.2)).toBe('menos de 1 min'));
	it('em minutos', () => expect(estimateSeparation(300, 5.3)).toBe('~27 min'));
});
