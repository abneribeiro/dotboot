<script lang="ts">
	// Novo trabalho: descarregar vídeo, descarregar música ou separar uma
	// música em pistas. O pedido vai para a fila e a página passa para os
	// Trabalhos.
	import { api, uploadFile, type JobRequest, type Preset, type Upload } from '#lib/api.js';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import ProgressBar from '#lib/components/ProgressBar.svelte';
	import { formatBytes } from '#lib/format.js';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';

	type Tab = 'video' | 'musica' | 'separar';
	const tabs: { id: Tab; label: string; icon: IconName; hint: string }[] = [
		{ id: 'video', label: 'Vídeo', icon: 'video', hint: 'MP4 que toca em todo o lado' },
		{ id: 'musica', label: 'Música', icon: 'music', hint: 'MP3 ou M4A com capa e título' },
		{ id: 'separar', label: 'Separar', icon: 'split', hint: 'vozes, bateria, baixo, guitarra…' }
	];

	const tab = $derived<Tab>(
		(['video', 'musica', 'separar'] as const).find(
			(t) => t === page.url.searchParams.get('tipo')
		) ?? 'video'
	);

	// Vídeo
	let videoURLs = $state('');
	let quality = $state('best');
	let videoFormat = $state('mp4');
	// Música
	let musicURLs = $state('');
	let musicFormat = $state('mp3');
	// Separar
	let source = $state<'url' | 'ficheiro'>('url');
	let stemsURL = $state('');
	let model = $state('6s');
	let presets = $state<Preset[]>([]);
	let file = $state<File | undefined>();
	let uploaded = $state<Upload | undefined>();
	let uploadProgress = $state(-1);
	let abortUpload: (() => void) | undefined;
	let dragging = $state(false);
	// Comum
	let trim = $state(false);
	let start = $state('');
	let end = $state('');
	let error = $state('');
	let sending = $state(false);

	onMount(async () => {
		try {
			presets = await api.presets();
			model = presets.find((p) => p.default)?.name ?? model;
		} catch (e) {
			error = (e as Error).message;
		}
	});

	const lines = (s: string) =>
		s
			.split('\n')
			.map((l) => l.trim())
			.filter(Boolean);

	function selectTab(id: Tab) {
		error = '';
		const url = new URL(page.url.href);
		url.searchParams.set('tipo', id);
		void goto(url, { replaceState: true });
	}

	function pickFile(f: File | undefined) {
		if (!f) return;
		abortUpload?.();
		file = f;
		uploaded = undefined;
		error = '';
		uploadProgress = 0;
		const up = uploadFile('/api/uploads', f, (p) => (uploadProgress = p));
		abortUpload = up.abort;
		up.promise
			.then((u) => {
				uploaded = u;
				uploadProgress = 1;
			})
			.catch((e) => {
				if (file === f) {
					error = (e as Error).message;
					file = undefined;
					uploadProgress = -1;
				}
			});
	}

	function clearFile() {
		abortUpload?.();
		file = undefined;
		uploaded = undefined;
		uploadProgress = -1;
	}

	const canSubmit = $derived.by(() => {
		if (sending) return false;
		if (tab === 'video') return lines(videoURLs).length > 0;
		if (tab === 'musica') return lines(musicURLs).length > 0;
		return source === 'url' ? stemsURL.trim() !== '' : !!uploaded;
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!canSubmit) return;
		const section = trim ? { start: start.trim(), end: end.trim() } : {};
		let req: JobRequest;
		if (tab === 'video') {
			req = { kind: 'video', urls: lines(videoURLs), quality, format: videoFormat, ...section };
		} else if (tab === 'musica') {
			req = { kind: 'audio', urls: lines(musicURLs), format: musicFormat, ...section };
		} else {
			req =
				source === 'url'
					? { kind: 'stems', url: stemsURL.trim(), model, ...section }
					: { kind: 'stems', upload: uploaded!.id, uploadName: uploaded!.name, model, ...section };
		}
		sending = true;
		error = '';
		try {
			await api.createJob(req);
			if (tab === 'video') videoURLs = '';
			if (tab === 'musica') musicURLs = '';
			if (tab === 'separar') {
				stemsURL = '';
				file = undefined;
				uploaded = undefined;
				uploadProgress = -1;
			}
			await goto('/trabalhos');
		} catch (err) {
			error = (err as Error).message;
		} finally {
			sending = false;
		}
	}

	const multiURL = $derived(tab !== 'separar');
	const sources: ['url' | 'ficheiro', string, IconName][] = [
		['url', 'Um link', 'link'],
		['ficheiro', 'Um ficheiro', 'file']
	];
	const qualities = [
		['best', 'Melhor disponível'],
		['2160', '2160p (4K) — converte para H.264, demora'],
		['1440', '1440p — converte para H.264, demora'],
		['1080', '1080p — a melhor sem conversão no YouTube'],
		['720', '720p'],
		['480', '480p']
	];
</script>

<svelte:head><title>Novo · dotboot</title></svelte:head>

<div class="mx-auto max-w-3xl px-4 py-8 sm:px-6 sm:py-12">
	<h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">O que queres fazer?</h1>
	<p class="mt-1 text-muted">
		Cola um link do YouTube, Instagram, X, TikTok, SoundCloud… ou envia uma música.
	</p>

	<div class="mt-8 grid grid-cols-3 gap-2" role="tablist" aria-label="Tipo de trabalho">
		{#each tabs as t (t.id)}
			<button
				role="tab"
				aria-selected={tab === t.id}
				class="card flex flex-col items-start gap-2 p-3 text-left transition-colors sm:p-4 {tab ===
				t.id
					? 'border-accent bg-accent/10'
					: 'hover:bg-panel-2'}"
				onclick={() => selectTab(t.id)}
			>
				<Icon name={t.icon} size={20} class={tab === t.id ? 'text-accent' : 'text-muted'} />
				<span class="font-medium">{t.label}</span>
				<span class="hidden text-xs text-muted sm:block">{t.hint}</span>
			</button>
		{/each}
	</div>

	<form class="card mt-4 space-y-6 p-5 sm:p-6" onsubmit={submit}>
		{#if tab === 'video'}
			<div>
				<label class="label" for="video-urls">Links</label>
				<textarea
					id="video-urls"
					class="field min-h-28 font-mono"
					placeholder="https://www.youtube.com/watch?v=…&#10;https://www.instagram.com/reel/…"
					bind:value={videoURLs}></textarea>
				<p class="mt-1.5 text-xs text-faint">Um por linha. Vídeos, playlists ou canais inteiros.</p>
			</div>
			<div class="grid gap-4 sm:grid-cols-2">
				<div>
					<label class="label" for="quality">Qualidade</label>
					<select id="quality" class="field" bind:value={quality}>
						{#each qualities as [value, label] (value)}
							<option {value}>{label}</option>
						{/each}
					</select>
				</div>
				<div>
					<span class="label">Formato</span>
					<div class="grid grid-cols-2 gap-2">
						{#each [['mp4', 'MP4', 'toca em todo o lado'], ['original', 'Original', 'tal como o site serve']] as [value, label, hint] (value)}
							<label
								class="cursor-pointer rounded-lg border px-3 py-2 text-sm transition-colors {videoFormat ===
								value
									? 'border-accent bg-accent/10'
									: 'border-line hover:bg-panel-2'}"
							>
								<input type="radio" class="sr-only" bind:group={videoFormat} {value} />
								<span class="block font-medium">{label}</span>
								<span class="block text-xs text-muted">{hint}</span>
							</label>
						{/each}
					</div>
				</div>
			</div>
		{:else if tab === 'musica'}
			<div>
				<label class="label" for="music-urls">Links</label>
				<textarea
					id="music-urls"
					class="field min-h-28 font-mono"
					placeholder="https://www.youtube.com/watch?v=…&#10;https://soundcloud.com/…"
					bind:value={musicURLs}></textarea>
				<p class="mt-1.5 text-xs text-faint">Um por linha. Músicas, playlists ou álbuns.</p>
			</div>
			<div>
				<span class="label">Formato</span>
				<div class="grid grid-cols-2 gap-2 sm:max-w-md">
					{#each [['mp3', 'MP3', 'universal'], ['m4a', 'M4A', 'melhor qualidade ao mesmo tamanho']] as [value, label, hint] (value)}
						<label
							class="cursor-pointer rounded-lg border px-3 py-2 text-sm transition-colors {musicFormat ===
							value
								? 'border-accent bg-accent/10'
								: 'border-line hover:bg-panel-2'}"
						>
							<input type="radio" class="sr-only" bind:group={musicFormat} {value} />
							<span class="block font-medium">{label}</span>
							<span class="block text-xs text-muted">{hint}</span>
						</label>
					{/each}
				</div>
			</div>
		{:else}
			<div>
				<span class="label">De onde vem a música?</span>
				<div class="inline-flex rounded-lg border border-line bg-panel-2 p-1">
					{#each sources as [value, label, icon] (value)}
						<button
							type="button"
							class="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-colors {source ===
							value
								? 'bg-raised text-text'
								: 'text-muted hover:text-text'}"
							aria-pressed={source === value}
							onclick={() => (source = value)}
						>
							<Icon name={icon} size={15} />
							{label}
						</button>
					{/each}
				</div>
			</div>

			{#if source === 'url'}
				<div>
					<label class="label" for="stems-url">Link da música</label>
					<input
						id="stems-url"
						class="field font-mono"
						placeholder="https://www.youtube.com/watch?v=…"
						bind:value={stemsURL}
					/>
				</div>
			{:else if file}
				<div class="rounded-lg border border-line bg-panel-2 p-4">
					<div class="flex items-center gap-3">
						<Icon name="music" class="text-accent" />
						<div class="min-w-0 flex-1">
							<p class="truncate font-medium">{file.name}</p>
							<p class="text-xs text-muted">
								{formatBytes(file.size)} · {uploaded
									? 'enviado'
									: `a enviar ${Math.round(uploadProgress * 100)}%`}
							</p>
						</div>
						<button
							type="button"
							class="btn btn-ghost"
							onclick={clearFile}
							aria-label="Escolher outro ficheiro"
						>
							<Icon name="x" size={16} />
						</button>
					</div>
					{#if !uploaded}
						<div class="mt-3">
							<ProgressBar fraction={uploadProgress} label="envio do ficheiro" />
						</div>
					{/if}
				</div>
			{:else}
				<label
					class="flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed px-4 py-10 text-center transition-colors {dragging
						? 'border-accent bg-accent/10'
						: 'border-line hover:bg-panel-2'}"
					ondragover={(e) => {
						e.preventDefault();
						dragging = true;
					}}
					ondragleave={() => (dragging = false)}
					ondrop={(e) => {
						e.preventDefault();
						dragging = false;
						pickFile(e.dataTransfer?.files[0]);
					}}
				>
					<Icon name="upload" size={26} class="text-muted" />
					<span class="font-medium">Arrasta a música para aqui, ou carrega para escolher</span>
					<span class="text-xs text-faint">MP3, WAV, FLAC, M4A, OGG… ou um vídeo — até 2 GB</span>
					<input
						type="file"
						class="sr-only"
						accept="audio/*,video/*,.flac,.m4a,.opus"
						onchange={(e) => pickFile((e.currentTarget as HTMLInputElement).files?.[0])}
					/>
				</label>
			{/if}

			<fieldset>
				<legend class="label">Modelo</legend>
				<div class="grid gap-2 sm:grid-cols-2">
					{#each presets as p (p.name)}
						<label
							class="cursor-pointer rounded-lg border p-3 text-sm transition-colors {model ===
							p.name
								? 'border-accent bg-accent/10'
								: 'border-line hover:bg-panel-2'}"
						>
							<input type="radio" class="sr-only" bind:group={model} value={p.name} />
							<span class="flex items-center justify-between gap-2">
								<span class="font-mono font-semibold">{p.name}</span>
								<span class="text-xs {p.speed > 3 ? 'text-warn' : 'text-faint'}">
									~{p.speed < 2 ? '1' : Math.round(p.speed)}× a duração
								</span>
							</span>
							<span class="mt-1 block text-muted">{p.description}</span>
						</label>
					{/each}
				</div>
				<p class="mt-2 text-xs text-faint">
					A separação corre no CPU. A primeira vez com cada modelo também o descarrega (50–650 MB).
				</p>
			</fieldset>
		{/if}

		<div>
			<label class="flex cursor-pointer items-center gap-2 text-sm">
				<input type="checkbox" class="size-4 accent-[var(--c-accent)]" bind:checked={trim} />
				<Icon name="scissors" size={15} class="text-muted" />
				{tab === 'separar' ? 'Separar só um pedaço' : 'Fazer um recorte'}
			</label>
			{#if trim}
				<div class="mt-3 grid max-w-sm grid-cols-2 gap-3">
					<div>
						<label class="label" for="start">Início</label>
						<input id="start" class="field font-mono" placeholder="01:30" bind:value={start} />
					</div>
					<div>
						<label class="label" for="end">Fim</label>
						<input id="end" class="field font-mono" placeholder="02:00" bind:value={end} />
					</div>
				</div>
				<p class="mt-1.5 text-xs text-faint">
					MM:SS ou HH:MM:SS.{multiURL ? ' Só com um link.' : ''}
				</p>
			{/if}
		</div>

		{#if error}
			<p
				class="flex items-start gap-2 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad"
				role="alert"
			>
				<Icon name="alert" size={16} class="mt-0.5" />{error}
			</p>
		{/if}

		<div class="flex items-center justify-end gap-3 border-t border-line pt-5">
			<button type="submit" class="btn btn-primary px-5" disabled={!canSubmit}>
				{#if tab === 'separar'}
					<Icon name="split" size={16} /> Separar
				{:else}
					<Icon name="download" size={16} /> Descarregar
				{/if}
			</button>
		</div>
	</form>
</div>
