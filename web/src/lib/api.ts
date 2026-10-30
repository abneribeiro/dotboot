// Tipos e chamadas da API do dotboot (internal/web). Os nomes dos campos
// seguem o JSON do servidor.

export type JobKind = 'video' | 'audio' | 'stems';
export type JobStatus = 'queued' | 'running' | 'done' | 'failed' | 'canceled';

export interface Job {
	id: string;
	kind: JobKind;
	title: string;
	status: JobStatus;
	stage: string;
	/** 0 a 1, ou -1 sem informação. */
	fraction: number;
	/** bytes/s */
	speed: number;
	/** segundos, ou -1 */
	eta: number;
	item: string;
	index: number;
	count: number;
	error?: string;
	partial?: boolean;
	sessionId?: string;
	downloadId?: string;
	created: string;
	started?: string;
	finished?: string;
}

export interface JobDetail extends Job {
	logs: string[];
}

export interface JobRequest {
	kind: JobKind;
	urls?: string[];
	url?: string;
	upload?: string;
	uploadName?: string;
	quality?: string;
	format?: string;
	model?: string;
	start?: string;
	end?: string;
}

export interface Session {
	id: string;
	title: string;
	source: string;
	model: string;
	stems: string[];
	duration: number;
	section?: string;
	created: string;
}

export interface Peaks {
	duration: number;
	values: number[];
}

export interface DownloadFile {
	name: string;
	size: number;
}

export interface Download {
	id: string;
	kind: 'video' | 'audio';
	title: string;
	urls: string[];
	files: DownloadFile[];
	created: string;
}

export interface Preset {
	name: string;
	description: string;
	stems: string[];
	speed: number;
	default: boolean;
}

export interface Info {
	version: string;
	dataDir: string;
	usage: number;
	cookies: boolean;
	cookiesEnv: boolean;
	cookiesFile?: string;
}

export interface Check {
	name: string;
	level: 'ok' | 'warn' | 'fail';
	detail: string;
	fix?: string;
}

export interface Upload {
	id: string;
	name: string;
	size: number;
}

/** Um erro da API, com a mensagem do servidor (já em português). */
export class ApiError extends Error {
	constructor(
		message: string,
		readonly status: number
	) {
		super(message);
	}
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const init: RequestInit = { method };
	if (body !== undefined) {
		init.body = JSON.stringify(body);
		init.headers = { 'Content-Type': 'application/json' };
	}
	const resp = await fetch(path, init);
	if (!resp.ok) {
		let message = `${resp.status} ${resp.statusText}`;
		try {
			const data = await resp.json();
			if (data?.error) message = data.error;
		} catch {
			// sem corpo JSON: fica o estado HTTP
		}
		throw new ApiError(message, resp.status);
	}
	if (resp.status === 204) return undefined as T;
	return resp.json() as Promise<T>;
}

export const api = {
	info: () => request<Info>('GET', '/api/info'),
	presets: () => request<Preset[]>('GET', '/api/presets'),
	doctor: () => request<Check[]>('GET', '/api/doctor'),

	jobs: () => request<Job[]>('GET', '/api/jobs'),
	job: (id: string) => request<JobDetail>('GET', `/api/jobs/${id}`),
	createJob: (req: JobRequest) => request<Job>('POST', '/api/jobs', req),
	cancelJob: (id: string) => request<void>('POST', `/api/jobs/${id}/cancel`),
	retryJob: (id: string) => request<Job>('POST', `/api/jobs/${id}/retry`),
	removeJob: (id: string) => request<void>('DELETE', `/api/jobs/${id}`),

	sessions: () => request<Session[]>('GET', '/api/sessions'),
	session: (id: string) => request<Session>('GET', `/api/sessions/${id}`),
	peaks: (id: string) => request<Record<string, Peaks>>('GET', `/api/sessions/${id}/peaks`),
	deleteSession: (id: string) => request<void>('DELETE', `/api/sessions/${id}`),

	downloads: () => request<Download[]>('GET', '/api/downloads'),
	deleteDownload: (id: string) => request<void>('DELETE', `/api/downloads/${id}`),

	deleteCookies: () => request<void>('DELETE', '/api/settings/cookies')
};

export const trackURL = (session: string, stem: string) =>
	`/api/sessions/${session}/tracks/${encodeURIComponent(stem)}`;

export const downloadFileURL = (id: string, name: string) =>
	`/api/downloads/${id}/files/${encodeURIComponent(name)}`;

export const downloadAllURL = (id: string) => `/api/downloads/${id}/all`;

/**
 * Envia um ficheiro (multipart) com progresso. O fetch ainda não dá o
 * progresso do envio, por isso usa-se o XMLHttpRequest.
 */
export function uploadFile(
	path: string,
	file: File,
	onProgress?: (fraction: number) => void
): { promise: Promise<Upload | undefined>; abort: () => void } {
	const xhr = new XMLHttpRequest();
	const promise = new Promise<Upload | undefined>((resolve, reject) => {
		xhr.open('POST', path);
		xhr.upload.onprogress = (e) => {
			if (e.lengthComputable) onProgress?.(e.loaded / e.total);
		};
		xhr.onload = () => {
			if (xhr.status >= 200 && xhr.status < 300) {
				resolve(xhr.responseText ? (JSON.parse(xhr.responseText) as Upload) : undefined);
				return;
			}
			let message = `${xhr.status} ${xhr.statusText}`;
			try {
				message = JSON.parse(xhr.responseText).error ?? message;
			} catch {
				// sem corpo JSON
			}
			reject(new ApiError(message, xhr.status));
		};
		xhr.onerror = () => reject(new ApiError('ligação ao dotboot perdida', 0));
		xhr.onabort = () => reject(new ApiError('envio cancelado', 0));
		const form = new FormData();
		form.append('file', file);
		xhr.send(form);
	});
	return { promise, abort: () => xhr.abort() };
}

/**
 * Descarrega uma resposta para o computador: usa o nome que o servidor
 * sugere (Content-Disposition) e devolve-o. Serve para as exportações, que
 * podem demorar — o fetch deixa a página mostrar que está a preparar.
 */
export async function saveFrom(url: string, signal?: AbortSignal): Promise<string> {
	const resp = await fetch(url, { signal });
	if (!resp.ok) {
		let message = `${resp.status} ${resp.statusText}`;
		try {
			message = (await resp.json()).error ?? message;
		} catch {
			// sem corpo JSON
		}
		throw new ApiError(message, resp.status);
	}
	const name = filenameFromDisposition(resp.headers.get('Content-Disposition')) ?? 'dotboot';
	const blob = await resp.blob();
	const href = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = href;
	a.download = name;
	document.body.append(a);
	a.click();
	a.remove();
	setTimeout(() => URL.revokeObjectURL(href), 60_000);
	return name;
}

/** Lê o nome de um Content-Disposition, incluindo o filename* (RFC 5987). */
export function filenameFromDisposition(header: string | null): string | undefined {
	if (!header) return undefined;
	const star = /filename\*\s*=\s*([^']*)'[^']*'([^;]+)/i.exec(header);
	if (star) {
		try {
			return decodeURIComponent(star[2].trim());
		} catch {
			// codificação inválida: tenta o filename simples
		}
	}
	const plain = /filename\s*=\s*"?([^";]+)"?/i.exec(header);
	return plain?.[1];
}
