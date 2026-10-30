<script lang="ts">
	// Um trabalho na fila: estado, etapa, progresso em tempo real e o log.
	import { api, downloadAllURL, type Job } from '#lib/api.js';
	import { live } from '#lib/events.svelte.js';
	import { formatAgo, formatBytes, formatETA } from '#lib/format.js';
	import { KIND_LABEL, STATUS_LABEL } from '#lib/jobs.js';
	import Icon, { type IconName } from './Icon.svelte';
	import ProgressBar from './ProgressBar.svelte';

	let { job, onerror }: { job: Job; onerror?: (message: string) => void } = $props();

	let showLog = $state(false);
	let logBox: HTMLElement | undefined = $state();

	const icon: Record<Job['kind'], IconName> = { video: 'video', audio: 'music', stems: 'split' };
	const statusClass: Record<Job['status'], string> = {
		queued: 'bg-raised text-muted',
		running: 'bg-accent/15 text-accent',
		done: 'bg-ok/15 text-ok',
		failed: 'bg-bad/15 text-bad',
		canceled: 'bg-raised text-muted'
	};

	const logs = $derived(live.logs[job.id] ?? []);
	const active = $derived(job.status === 'queued' || job.status === 'running');
	const percent = $derived(job.fraction >= 0 ? `${Math.round(job.fraction * 100)}%` : '');

	async function toggleLog() {
		showLog = !showLog;
		if (showLog && !live.logs[job.id]) {
			try {
				const detail = await api.job(job.id);
				live.watchLog(job.id, detail.logs);
			} catch (e) {
				onerror?.((e as Error).message);
			}
		}
	}

	// O log desce sozinho enquanto chegam linhas.
	$effect(() => {
		void logs.length;
		if (logBox) logBox.scrollTop = logBox.scrollHeight;
	});

	async function act(fn: () => Promise<unknown>) {
		try {
			await fn();
		} catch (e) {
			onerror?.((e as Error).message);
		}
	}
</script>

<article class="card p-4">
	<div class="flex items-start gap-3">
		<div class="mt-0.5 grid size-9 place-items-center rounded-lg bg-panel-2 text-muted">
			<Icon name={icon[job.kind]} />
		</div>
		<div class="min-w-0 flex-1">
			<div class="flex flex-wrap items-center gap-2">
				<span class="text-xs font-semibold tracking-wide text-muted uppercase"
					>{KIND_LABEL[job.kind]}</span
				>
				<span class="rounded-full px-2 py-0.5 text-xs font-medium {statusClass[job.status]}">
					{STATUS_LABEL[job.status]}
				</span>
				<span class="text-xs text-faint">{formatAgo(job.created)}</span>
			</div>
			<p class="mt-1 truncate font-medium" title={job.item || job.title}>{job.item || job.title}</p>
			{#if job.item && job.item !== job.title}
				<p class="truncate text-xs text-faint" title={job.title}>{job.title}</p>
			{/if}
		</div>
	</div>

	{#if job.status === 'running'}
		<div class="mt-4 space-y-2">
			<div class="flex items-baseline justify-between gap-3 text-sm">
				<span class="truncate text-muted">
					{job.stage || 'a começar'}{job.count > 1 ? ` · ${job.index} de ${job.count}` : ''}
				</span>
				<span class="shrink-0 font-mono text-xs text-muted tabular-nums">
					{[percent, job.speed > 0 ? `${formatBytes(job.speed)}/s` : '', formatETA(job.eta)]
						.filter(Boolean)
						.join(' · ')}
				</span>
			</div>
			<ProgressBar fraction={job.fraction} label={job.stage} />
		</div>
	{/if}

	{#if job.error}
		<p
			class="mt-3 rounded-lg px-3 py-2 text-sm {job.partial
				? 'bg-warn/10 text-warn'
				: 'bg-bad/10 text-bad'}"
		>
			{job.partial ? 'Alguns itens falharam: ' : ''}{job.error}
		</p>
	{/if}

	<div class="mt-4 flex flex-wrap items-center gap-2">
		{#if job.status === 'done' && job.sessionId}
			<a class="btn btn-primary" href="/estudio/{job.sessionId}">
				<Icon name="headphones" size={16} /> Abrir no estúdio
			</a>
		{/if}
		{#if job.status === 'done' && job.downloadId}
			<a class="btn btn-primary" href={downloadAllURL(job.downloadId)} download>
				<Icon name="download" size={16} /> Descarregar
			</a>
			<a class="btn" href="/biblioteca#d-{job.downloadId}">Ver na biblioteca</a>
		{/if}
		{#if active}
			<button class="btn btn-danger" onclick={() => act(() => api.cancelJob(job.id))}>
				<Icon name="x" size={16} /> Cancelar
			</button>
		{/if}
		{#if job.status === 'failed' || job.status === 'canceled'}
			<button class="btn" onclick={() => act(() => api.retryJob(job.id))}>
				<Icon name="retry" size={16} /> Repetir
			</button>
		{/if}
		<button class="btn btn-ghost text-muted" onclick={toggleLog} aria-expanded={showLog}>
			<Icon name="chevron" size={16} class="transition-transform {showLog ? 'rotate-90' : ''}" /> Log
		</button>
		{#if !active}
			<button
				class="btn btn-ghost ml-auto text-muted"
				onclick={() => act(() => api.removeJob(job.id))}
				title="Tirar da lista (o resultado fica na biblioteca)"
			>
				<Icon name="x" size={16} /> Tirar da lista
			</button>
		{/if}
	</div>

	{#if showLog}
		<pre
			bind:this={logBox}
			class="mt-3 max-h-72 overflow-auto rounded-lg border border-line bg-bg p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap text-muted">{logs.length
				? logs.join('\n')
				: 'Sem linhas de log.'}</pre>
	{/if}
</article>
