import { describe, expect, it } from 'vitest';
import type { Job } from './api.js';
import { applyMessage, parseMessage } from './jobs.js';

const job = (id: string, status: Job['status'] = 'queued'): Job => ({
	id,
	kind: 'video',
	title: id,
	status,
	stage: '',
	fraction: -1,
	speed: 0,
	eta: -1,
	item: '',
	index: 0,
	count: 0,
	created: '2026-10-30T12:00:00Z'
});

describe('applyMessage', () => {
	it('o estado completo substitui a lista', () => {
		expect(applyMessage([job('a')], { type: 'jobs', jobs: [job('b')] }).map((j) => j.id)).toEqual([
			'b'
		]);
		expect(applyMessage([job('a')], { type: 'jobs' })).toEqual([]);
	});
	it('um trabalho novo vai para o fim; um conhecido é atualizado no lugar', () => {
		let list = applyMessage([job('a')], { type: 'job', job: job('b') });
		list = applyMessage(list, { type: 'job', job: job('a', 'running') });
		expect(list.map((j) => `${j.id}:${j.status}`)).toEqual(['a:running', 'b:queued']);
	});
	it('removed tira da lista; library e log não mexem nela', () => {
		const list = [job('a'), job('b')];
		expect(applyMessage(list, { type: 'removed', id: 'a' }).map((j) => j.id)).toEqual(['b']);
		expect(applyMessage(list, { type: 'library' })).toBe(list);
		expect(applyMessage(list, { type: 'log', id: 'a', line: 'x' })).toBe(list);
	});
});

describe('parseMessage', () => {
	it('lê uma mensagem válida', () => {
		expect(parseMessage('{"type":"library"}')).toEqual({ type: 'library' });
	});
	it('ignora o que não percebe', () => {
		expect(parseMessage('não é json')).toBeUndefined();
		expect(parseMessage('{"sem":"tipo"}')).toBeUndefined();
	});
});
