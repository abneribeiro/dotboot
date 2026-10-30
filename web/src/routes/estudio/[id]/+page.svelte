<script lang="ts">
	// O estúdio: as pistas de uma música separada, como num editor de música.
	// Ouve-se a combinação que se quiser (mute, solo, volume) e descarrega-se
	// o que se escolher — num só ficheiro misturado, ou em ficheiros separados.
	import { api, saveFrom, type Peaks, type Session } from '#lib/api.js';
	import Icon from '#lib/components/Icon.svelte';
	import Waveform from '#lib/components/Waveform.svelte';
	import { formatTime } from '#lib/format.js';
	import {
		audibleItems,
		describeItems,
		effectiveGains,
		exportItems,
		exportURL,
		stemColor,
		stemLabel,
		type ExportFormat,
		type ExportItem,
		type ExportMode
	} from '#lib/mixer.js';
	import { StudioPlayer } from '#lib/player.svelte.js';
	import { page } from '$app/state';
	import { onDestroy } from 'svelte';

	let session = $state<Session | undefined>();
	let peaks = $state<Record<string, Peaks>>({});
	let player = $state<StudioPlayer | undefined>();
	let loadError = $state('');

	let format = $state<ExportFormat>('wav');
	let withVolumes = $state(true);
	let busy = $state('');
	let notice = $state<{ kind: 'ok' | 'bad'; text: string } | undefined>();
	let abort: AbortController | undefined;

	const id = $derived(page.params.id ?? '');

	$effect(() => {
		const current = id;
		let cancelled = false;
		loadError = '';
		Promise.all([api.session(current), api.peaks(current)])
			.then(([s, p]) => {
				if (cancelled) return;
				player?.destroy();
				session = s;
				peaks = p;
				player = new StudioPlayer(s.id, s.stems, s.duration);
				// Para diagnóstico (e para os testes no browser): o leitor
				// fica acessível na consola.
				(window as unknown as { dotbootPlayer: StudioPlayer }).dotbootPlayer = player;
			})
			.catch((e) => {
				if (!cancelled) loadError = (e as Error).message;
			});
		return () => {
			cancelled = true;
		};
	});

	onDestroy(() => {
		player?.destroy();
		abort?.abort();
	});

	// O mute, o solo e os volumes chegam ao som assim que mudam.
	$effect(() => {
		player?.applyGains();
	});

	const gains = $derived(player ? effectiveGains(player.tracks) : []);
	const progress = $derived(player && player.duration > 0 ? player.time / player.duration : 0);
	const selected = $derived(player ? exportItems(player.tracks, withVolumes) : []);
	const audible = $derived(player ? audibleItems(player.tracks) : []);
	const mixChanged = $derived(player?.tracks.some((t) => t.solo || t.muted) ?? false);

	const formats: { id: ExportFormat; label: string; hint: string }[] = [
		{ id: 'wav', label: 'WAV', hint: 'sem perdas' },
		{ id: 'flac', label: 'FLAC', hint: 'sem perdas, mais leve' },
		{ id: 'mp3', label: 'MP3', hint: '320 kbps' }
	];

	function selectAll(value: boolean) {
		player?.tracks.forEach((t) => (t.selected = value));
	}

	function selectAudible() {
		if (!player) return;
		player.tracks.forEach((t, i) => (t.selected = gains[i] > 0));
	}

	async function exportNow(items: ExportItem[], mode: ExportMode, what: string) {
		if (!session || items.length === 0 || busy) return;
		abort = new AbortController();
		busy = what;
		notice = undefined;
		try {
			const name = await saveFrom(exportURL(session.id, items, format, mode), abort.signal);
			notice = { kind: 'ok', text: `Guardado: ${name}` };
		} catch (e) {
			if ((e as Error).name !== 'AbortError') notice = { kind: 'bad', text: (e as Error).message };
		} finally {
			busy = '';
		}
	}

	function onkeydown(e: KeyboardEvent) {
		if (!player || e.ctrlKey || e.metaKey || e.altKey) return;
		const target = e.target as HTMLElement;
		if (target.closest('input, textarea, select, button, a, [role=slider]')) {
			if (e.key !== ' ' || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') return;
			if (target.closest('button, a')) return;
		}
		const digit = /^Digit([1-9])$/.exec(e.code);
		if (e.key === ' ') {
			player.toggle();
		} else if (digit) {
			const t = player.tracks[Number(digit[1]) - 1];
			if (!t) return;
			if (e.shiftKey) t.muted = !t.muted;
			else t.solo = !t.solo;
		} else if (e.key === 'ArrowRight') {
			player.seek(player.time + 5);
		} else if (e.key === 'ArrowLeft') {
			player.seek(player.time - 5);
		} else if (e.key === 'Home') {
			player.seek(0);
		} else {
			return;
		}
		e.preventDefault();
	}
</script>

<svelte:window {onkeydown} />
<svelte:head><title>{session ? `${session.title} · ` : ''}Estúdio · dotboot</title></svelte:head>

<div class="mx-auto max-w-6xl px-4 py-6 sm:px-6 sm:py-8">
	<a href="/biblioteca" class="inline-flex items-center gap-1 text-sm text-muted hover:text-text">
		<Icon name="back" size={16} /> Biblioteca
	</a>

	{#if loadError}
		<div class="card mt-6 p-6">
			<p class="font-medium text-bad">Não foi possível abrir esta separação</p>
			<p class="mt-1 text-sm text-muted">{loadError}</p>
		</div>
	{:else if !session || !player}
		<p class="mt-6 text-muted">A abrir o estúdio…</p>
	{:else}
		<header class="mt-3">
			<h1 class="text-xl font-semibold tracking-tight sm:text-2xl">{session.title}</h1>
			<p class="mt-1 text-sm text-muted">
				{session.stems.length} pistas · modelo {session.model} · {formatTime(
					player.duration
				)}{session.section ? ` · trecho ${session.section}` : ''}
			</p>
		</header>

		<!-- Transporte -->
		<div
			class="card sticky top-[57px] z-10 mt-5 flex flex-wrap items-center gap-x-4 gap-y-3 p-3 shadow-lg shadow-black/20 md:top-3"
		>
			<button
				class="grid size-12 place-items-center rounded-full bg-accent text-accent-ink transition-transform hover:scale-105 active:scale-95"
				onclick={() => player?.toggle()}
				aria-label={player.playing ? 'Pausa' : 'Tocar'}
				title="Espaço"
			>
				<Icon name={player.playing ? 'pause' : 'play'} size={22} />
			</button>
			<button
				class="btn btn-ghost px-2"
				onclick={() => player?.seek(0)}
				aria-label="Voltar ao início"
				title="Home"
			>
				<Icon name="rewind" size={18} />
			</button>
			<div class="font-mono text-sm tabular-nums">
				<span class="text-text">{formatTime(player.time, true)}</span>
				<span class="text-faint"> / {formatTime(player.duration, true)}</span>
			</div>
			<button
				class="btn btn-ghost px-2 {player.loop ? 'text-accent' : 'text-muted'}"
				onclick={() => player && (player.loop = !player.loop)}
				aria-pressed={player.loop}
				title="Repetir"
			>
				<Icon name="repeat" size={18} /><span class="sr-only">Repetir</span>
			</button>
			<label
				class="flex items-center gap-2 text-muted"
				title="Volume geral (não entra nos ficheiros)"
			>
				<Icon name="volume" size={18} />
				<input
					type="range"
					min="0"
					max="1"
					step="0.01"
					bind:value={player.master}
					class="w-24 accent-[var(--c-accent)]"
					aria-label="Volume geral"
				/>
			</label>
			<div class="ml-auto flex items-center gap-3 text-sm">
				{#if player.error}
					<span class="text-bad">{player.error}</span>
				{:else if player.playing && player.buffering}
					<span class="text-warn">a carregar…</span>
				{:else if !player.ready}
					<span class="text-faint">a preparar as pistas…</span>
				{/if}
				{#if mixChanged}
					<button class="btn" onclick={() => player?.resetMix()}>Ouvir tudo</button>
				{/if}
			</div>
		</div>

		<!-- Pistas -->
		<section class="card mt-4 overflow-hidden" aria-label="Pistas">
			<div class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-2.5 text-sm">
				<span class="font-medium">Pistas</span>
				<span class="hidden text-xs text-faint lg:inline">
					1–{player.tracks.length} solo · shift+número mute · ←/→ 5 s
				</span>
				<span class="ml-auto text-xs text-muted">Marcar:</span>
				<button class="btn btn-ghost px-2 py-1 text-xs" onclick={() => selectAll(true)}
					>todas</button
				>
				<button class="btn btn-ghost px-2 py-1 text-xs" onclick={selectAudible}
					>as que se ouvem</button
				>
				<button class="btn btn-ghost px-2 py-1 text-xs" onclick={() => selectAll(false)}
					>nenhuma</button
				>
			</div>

			{#each player.tracks as track, i (track.stem)}
				{@const color = stemColor(track.stem)}
				<div
					class="grid gap-3 border-b border-line px-4 py-3 last:border-b-0 md:grid-cols-[260px_1fr] md:items-center {track.selected
						? 'bg-accent/5'
						: ''}"
				>
					<div class="flex items-center gap-3">
						<input
							type="checkbox"
							class="size-5 shrink-0 cursor-pointer accent-[var(--c-accent)]"
							bind:checked={track.selected}
							aria-label="Marcar {track.stem} para descarregar"
						/>
						<span class="h-9 w-1 shrink-0 rounded-full" style="background: {color}"></span>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<span class="truncate font-medium">{stemLabel(track.stem)}</span>
								<kbd
									class="hidden rounded border border-line px-1 font-mono text-[10px] text-faint lg:inline"
									>{i + 1}</kbd
								>
							</div>
							<label class="mt-1 flex items-center gap-2">
								<input
									type="range"
									min="0"
									max="2"
									step="0.01"
									bind:value={track.volume}
									class="w-full max-w-28 accent-[var(--c-accent)]"
									aria-label="Volume de {track.stem}"
									ondblclick={() => (track.volume = 1)}
									title="Duplo clique: 100%"
								/>
								<span class="w-10 text-right font-mono text-xs text-muted tabular-nums"
									>{Math.round(track.volume * 100)}%</span
								>
							</label>
						</div>
						<div class="flex gap-1">
							<button
								class="grid size-8 place-items-center rounded-md border text-xs font-bold transition-colors {track.muted
									? 'border-bad bg-bad text-white'
									: 'border-line text-muted hover:bg-panel-2'}"
								onclick={() => (track.muted = !track.muted)}
								aria-pressed={track.muted}
								aria-label="Silenciar {track.stem}"
								title="Mute (shift+{i + 1})">M</button
							>
							<button
								class="grid size-8 place-items-center rounded-md border text-xs font-bold transition-colors {track.solo
									? 'border-warn bg-warn text-black'
									: 'border-line text-muted hover:bg-panel-2'}"
								onclick={(e) => {
									if (e.altKey || e.shiftKey) player?.soloOnly(i);
									else track.solo = !track.solo;
								}}
								aria-pressed={track.solo}
								aria-label="Solo de {track.stem}"
								title="Solo ({i + 1}); shift+clique: só esta">S</button
							>
						</div>
					</div>
					<Waveform
						peaks={peaks[track.stem]?.values ?? []}
						{color}
						{progress}
						dimmed={gains[i] === 0}
						label="Posição na pista {track.stem}"
						onseek={(f) => player?.seek(f * (player?.duration ?? 0))}
					/>
				</div>
			{/each}
		</section>

		<!-- Descarregar -->
		<section class="card mt-4 p-4 sm:p-5" aria-labelledby="export-title">
			<div class="flex flex-wrap items-start justify-between gap-4">
				<div>
					<h2 id="export-title" class="flex items-center gap-2 font-semibold">
						<Icon name="download" size={18} class="text-accent" /> Descarregar
					</h2>
					<p class="mt-1 text-sm text-muted">
						{#if selected.length}
							Marcadas: <span class="font-medium text-text">{describeItems(selected)}</span>
						{:else}
							Marca as pistas que queres (ex: baixo e bateria) e escolhe como as queres.
						{/if}
					</p>
				</div>
				<div class="flex flex-wrap items-center gap-3">
					<div
						class="inline-flex rounded-lg border border-line bg-panel-2 p-1"
						role="radiogroup"
						aria-label="Formato"
					>
						{#each formats as f (f.id)}
							<button
								class="rounded-md px-3 py-1.5 text-sm transition-colors {format === f.id
									? 'bg-raised font-semibold text-text'
									: 'text-muted hover:text-text'}"
								role="radio"
								aria-checked={format === f.id}
								title={f.hint}
								onclick={() => (format = f.id)}>{f.label}</button
							>
						{/each}
					</div>
					<label
						class="flex items-center gap-2 text-sm text-muted"
						title="Cada pista sai com o volume do seu fader"
					>
						<input
							type="checkbox"
							class="size-4 accent-[var(--c-accent)]"
							bind:checked={withVolumes}
						/>
						Com os volumes do estúdio
					</label>
				</div>
			</div>

			<div class="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
				<button
					class="btn btn-primary h-auto w-full min-w-0 flex-col items-start py-3 text-left"
					disabled={!selected.length || !!busy}
					onclick={() => exportNow(selected, 'mix', 'mix')}
				>
					<span class="flex items-center gap-2"
						><Icon name="layers" size={16} /> Juntar num ficheiro</span
					>
					<span class="max-w-full truncate text-xs font-normal opacity-80"
						>as marcadas, misturadas num só {format.toUpperCase()}</span
					>
				</button>
				<button
					class="btn h-auto w-full min-w-0 flex-col items-start py-3 text-left"
					disabled={!selected.length || !!busy}
					onclick={() => exportNow(selected, 'zip', 'zip')}
				>
					<span class="flex items-center gap-2"
						><Icon name="zip" size={16} /> Em ficheiros separados</span
					>
					<span class="max-w-full truncate text-xs font-normal text-muted"
						>as marcadas, uma por ficheiro (ZIP)</span
					>
				</button>
				<button
					class="btn h-auto w-full min-w-0 flex-col items-start py-3 text-left"
					disabled={!audible.length || !!busy}
					onclick={() => exportNow(audible, 'mix', 'audible')}
				>
					<span class="flex items-center gap-2"
						><Icon name="headphones" size={16} /> O que estou a ouvir</span
					>
					<span class="max-w-full truncate text-xs font-normal text-muted"
						>{describeItems(audible)}, num ficheiro</span
					>
				</button>
				<button
					class="btn h-auto w-full min-w-0 flex-col items-start py-3 text-left"
					disabled={!!busy}
					onclick={() =>
						player &&
						exportNow(
							player.tracks.map((t) => ({ stem: t.stem, gain: 1 })),
							'zip',
							'all'
						)}
				>
					<span class="flex items-center gap-2"><Icon name="zip" size={16} /> Todas as pistas</span>
					<span class="max-w-full truncate text-xs font-normal text-muted"
						>cada uma no seu ficheiro (ZIP)</span
					>
				</button>
			</div>

			{#if busy}
				<p class="mt-3 flex items-center gap-2 text-sm text-muted" role="status">
					<span class="size-3 animate-spin rounded-full border-2 border-accent border-t-transparent"
					></span>
					A preparar o ficheiro… (uma mistura longa em MP3 demora alguns segundos)
					<button class="ml-auto text-xs underline" onclick={() => abort?.abort()}>cancelar</button>
				</p>
			{:else if notice}
				<p class="mt-3 text-sm {notice.kind === 'ok' ? 'text-ok' : 'text-bad'}" role="status">
					{notice.text}
				</p>
			{/if}
		</section>
	{/if}
</div>
