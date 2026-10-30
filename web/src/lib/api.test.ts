import { describe, expect, it } from 'vitest';
import { filenameFromDisposition } from './api.js';

describe('filenameFromDisposition', () => {
	it('lê o filename* com acentos (RFC 5987)', () => {
		expect(
			filenameFromDisposition(
				"attachment; filename*=utf-8''A%20Minha%20M%C3%BAsica%20(baixo%20+%20bateria).wav"
			)
		).toBe('A Minha Música (baixo + bateria).wav');
	});
	it('lê o filename simples', () => {
		expect(filenameFromDisposition('attachment; filename="musica.zip"')).toBe('musica.zip');
		expect(filenameFromDisposition('attachment; filename=musica.zip')).toBe('musica.zip');
	});
	it('sem cabeçalho, nada', () => {
		expect(filenameFromDisposition(null)).toBeUndefined();
	});
});
