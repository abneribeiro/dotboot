<script lang="ts">
	// Tudo o que o dotboot guardou: as separações (abrem no estúdio) e os
	// vídeos e músicas descarregados. Nada sai daqui sem ser descarregado.
	import { api, downloadAllURL, downloadFileURL, type Download, type Session } from '#lib/api.js';
	import Icon from '#lib/components/Icon.svelte';
	import { live } from '#lib/events.svelte.js';
	import { formatAgo, formatBytes, formatTime } from '#lib/format.js';
	import { stemColor } from '#lib/mixer.js';

	let sessions = $state<Session[] | undefined>();
	let downloads = $state<Download[] | undefined>();
	let error = $state('');
	let preview = $state<string | undefined>();

	// Volta a pedir a lista sempre que a biblioteca muda (um trabalho que
	// acaba noutra página, ou noutro separador).
	$effect(() => {
		void live.library;
		void load();
	});

	async function load() {
		try {
			[sessions, downloads] = await Promise.all([api.sessions(), api.downloads()]);
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function remove(kind: 'session' | 'download', id: string, title: string) {
		if (!confirm(`Apagar "${title}"? Os ficheiros saem do dotboot de vez.`)) return;
		try {
			if (kind === 'session') await api.deleteSession(id);
			else await api.deleteDownload(id);
			await load();
		} catch (e) {
			error = (e as Error).message;
		}
	}

	const size = (d: Download) => d.files.reduce((n, f) => n + f.size, 0);
</script>

<svelte:head><title>Biblioteca · dotboot</title></svelte:head>

<div class="mx-auto max-w-5xl px-4 py-8 sm:px-6 sm:py-12">
	<h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">Biblioteca</h1>
	<p class="mt-1 text-muted">Ficam guardados no dotboot até os apagares.</p>

	{#if error}
		<p class="mt-4 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad" role="alert">{error}</p>
	{/if}

	<section class="mt-10" aria-labelledby="sep-title">
		<h2 id="sep-title" class="flex items-center gap-2 text-lg font-semibold">
			<Icon name="layers" size={18} class="text-accent" /> Músicas separadas
		</h2>
		{#if sessions === undefined}
			<p class="mt-4 text-sm text-muted">A carregar…</p>
		{:else if sessions.length === 0}
			<p class="mt-4 text-sm text-muted">
				Ainda não separaste nenhuma música. <a class="text-accent underline" href="/?tipo=separar"
					>Separar agora</a
				>
			</p>
		{:else}
			<div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
				{#each sessions as s (s.id)}
					<article class="card group flex flex-col p-4">
						<a href="/estudio/{s.id}" class="block min-w-0 flex-1">
							<div class="flex h-10 items-end gap-1" aria-hidden="true">
								{#each s.stems as stem (stem)}
									<span
										class="h-full flex-1 rounded-sm opacity-80"
										style="background: {stemColor(stem)}"
									></span>
								{/each}
							</div>
							<h3 class="mt-3 line-clamp-2 font-medium group-hover:text-accent">{s.title}</h3>
							<p class="mt-1 text-xs text-muted">
								{s.stems.length} pistas · modelo {s.model} · {formatTime(s.duration)}{s.section
									? ` · trecho ${s.section}`
									: ''}
							</p>
							<p class="mt-0.5 text-xs text-faint">{formatAgo(s.created)}</p>
						</a>
						<div class="mt-4 flex gap-2">
							<a class="btn btn-primary flex-1" href="/estudio/{s.id}">
								<Icon name="headphones" size={16} /> Abrir no estúdio
							</a>
							<button
								class="btn btn-ghost btn-danger"
								onclick={() => remove('session', s.id, s.title)}
								aria-label="Apagar {s.title}"
							>
								<Icon name="trash" size={16} />
							</button>
						</div>
					</article>
				{/each}
			</div>
		{/if}
	</section>

	<section class="mt-12" aria-labelledby="dl-title">
		<h2 id="dl-title" class="flex items-center gap-2 text-lg font-semibold">
			<Icon name="download" size={18} class="text-accent" /> Vídeos e músicas
		</h2>
		{#if downloads === undefined}
			<p class="mt-4 text-sm text-muted">A carregar…</p>
		{:else if downloads.length === 0}
			<p class="mt-4 text-sm text-muted">Ainda não descarregaste nada.</p>
		{:else}
			<ul class="card mt-4 divide-y divide-line">
				{#each downloads as d (d.id)}
					<li id="d-{d.id}" class="p-4">
						<div class="flex flex-wrap items-center gap-3">
							<div class="grid size-9 place-items-center rounded-lg bg-panel-2 text-muted">
								<Icon name={d.kind === 'video' ? 'video' : 'music'} size={17} />
							</div>
							<div class="min-w-0 flex-1">
								<p class="truncate font-medium" title={d.title}>{d.title}</p>
								<p class="text-xs text-muted">
									{d.files.length} ficheiro{d.files.length > 1 ? 's' : ''} · {formatBytes(size(d))} ·
									{formatAgo(d.created)}
								</p>
							</div>
							<div class="flex gap-2">
								{#if d.files.length === 1}
									<button
										class="btn btn-ghost"
										onclick={() => (preview = preview === d.id ? undefined : d.id)}
										aria-expanded={preview === d.id}
									>
										<Icon name="play" size={14} />
										{preview === d.id ? 'Fechar' : 'Ver'}
									</button>
								{/if}
								<a class="btn btn-primary" href={downloadAllURL(d.id)} download>
									<Icon name="download" size={16} />
									{d.files.length > 1 ? 'Tudo (ZIP)' : 'Descarregar'}
								</a>
								<button
									class="btn btn-ghost btn-danger"
									onclick={() => remove('download', d.id, d.title)}
									aria-label="Apagar {d.title}"
								>
									<Icon name="trash" size={16} />
								</button>
							</div>
						</div>
						{#if preview === d.id}
							{@const src = `${downloadFileURL(d.id, d.files[0].name)}?inline=1`}
							<div class="mt-3">
								{#if d.kind === 'video'}
									<!-- svelte-ignore a11y_media_has_caption -->
									<video {src} controls class="max-h-[60vh] w-full rounded-lg bg-black"></video>
								{:else}
									<audio {src} controls class="w-full"></audio>
								{/if}
							</div>
						{/if}
						{#if d.files.length > 1}
							<details class="mt-2 text-sm">
								<summary class="cursor-pointer text-muted">Ver os ficheiros</summary>
								<ul class="mt-2 space-y-1">
									{#each d.files as f (f.name)}
										<li class="flex items-center gap-2">
											<a
												class="truncate text-accent hover:underline"
												href={downloadFileURL(d.id, f.name)}
												download>{f.name}</a
											>
											<span class="shrink-0 text-xs text-faint">{formatBytes(f.size)}</span>
										</li>
									{/each}
								</ul>
							</details>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</section>
</div>
