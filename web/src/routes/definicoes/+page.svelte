<script lang="ts">
	// Definições: cookies para os sites que pedem login, o espaço ocupado, o
	// tema e o diagnóstico do que está instalado.
	import { api, uploadFile, type Check, type Info } from '#lib/api.js';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { live } from '#lib/events.svelte.js';
	import { formatBytes } from '#lib/format.js';
	import { onMount } from 'svelte';

	let info = $state<Info | undefined>();
	let checks = $state<Check[] | undefined>();
	let error = $state('');
	let cookieMessage = $state('');
	let theme = $state<'system' | 'light' | 'dark'>('system');

	onMount(() => {
		try {
			const saved = localStorage.getItem('dotboot-tema');
			if (saved === 'light' || saved === 'dark') theme = saved;
		} catch {
			// sem localStorage (janela privada): fica o tema do sistema
		}
		void loadInfo();
		void loadChecks();
	});

	// O espaço ocupado muda quando a biblioteca muda.
	$effect(() => {
		void live.library;
		void loadInfo();
	});

	async function loadInfo() {
		try {
			info = await api.info();
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function loadChecks() {
		checks = undefined;
		try {
			checks = await api.doctor();
		} catch (e) {
			error = (e as Error).message;
		}
	}

	function setTheme(t: typeof theme) {
		theme = t;
		try {
			if (t === 'system') {
				localStorage.removeItem('dotboot-tema');
				delete document.documentElement.dataset.theme;
			} else {
				localStorage.setItem('dotboot-tema', t);
				document.documentElement.dataset.theme = t;
			}
		} catch {
			document.documentElement.dataset.theme = t === 'system' ? '' : t;
		}
	}

	async function sendCookies(file: File | undefined) {
		if (!file) return;
		cookieMessage = '';
		try {
			await uploadFile('/api/settings/cookies', file).promise;
			cookieMessage = 'Cookies guardados. Os próximos downloads já os usam.';
			await loadInfo();
		} catch (e) {
			cookieMessage = (e as Error).message;
		}
	}

	async function removeCookies() {
		try {
			await api.deleteCookies();
			cookieMessage = 'Cookies apagados.';
			await loadInfo();
		} catch (e) {
			cookieMessage = (e as Error).message;
		}
	}

	const levelIcon: Record<Check['level'], { icon: IconName; cls: string }> = {
		ok: { icon: 'check', cls: 'text-ok' },
		warn: { icon: 'alert', cls: 'text-warn' },
		fail: { icon: 'x', cls: 'text-bad' }
	};
	const themes: { id: typeof theme; label: string; icon: IconName }[] = [
		{ id: 'system', label: 'Sistema', icon: 'monitor' },
		{ id: 'dark', label: 'Escuro', icon: 'moon' },
		{ id: 'light', label: 'Claro', icon: 'sun' }
	];
</script>

<svelte:head><title>Definições · dotboot</title></svelte:head>

<div class="mx-auto max-w-3xl space-y-6 px-4 py-8 sm:px-6 sm:py-12">
	<div>
		<h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">Definições</h1>
		{#if info}<p class="mt-1 text-muted">dotboot {info.version}</p>{/if}
	</div>

	{#if error}
		<p class="rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad" role="alert">{error}</p>
	{/if}

	<section class="card p-5" aria-labelledby="cookies-title">
		<h2 id="cookies-title" class="font-semibold">Cookies</h2>
		<p class="mt-1 text-sm text-muted">
			Para vídeos que pedem login, ou quando o YouTube pede para confirmares que não és um robô.
			Exporta os cookies de um browser com sessão iniciada, no formato <code>cookies.txt</code> (Netscape)
			— por exemplo com a extensão "Get cookies.txt LOCALLY".
		</p>
		<div class="mt-4 flex flex-wrap items-center gap-3">
			{#if info?.cookiesEnv}
				<span class="text-sm text-muted">
					A usar <code class="text-text">{info.cookiesFile}</code> (de DOTBOOT_COOKIES_FILE).
				</span>
			{:else}
				<span class="flex items-center gap-2 text-sm">
					<Icon
						name={info?.cookies ? 'check' : 'x'}
						size={16}
						class={info?.cookies ? 'text-ok' : 'text-faint'}
					/>
					{info?.cookies ? 'Há cookies guardados' : 'Sem cookies'}
				</span>
				<label class="btn ml-auto cursor-pointer">
					<Icon name="upload" size={16} />
					{info?.cookies ? 'Trocar' : 'Enviar cookies.txt'}
					<input
						type="file"
						class="sr-only"
						accept=".txt,text/plain"
						onchange={(e) => sendCookies((e.currentTarget as HTMLInputElement).files?.[0])}
					/>
				</label>
				{#if info?.cookies}
					<button class="btn btn-danger" onclick={removeCookies}
						><Icon name="trash" size={16} /> Apagar</button
					>
				{/if}
			{/if}
		</div>
		{#if cookieMessage}<p class="mt-3 text-sm text-muted" role="status">{cookieMessage}</p>{/if}
	</section>

	<section class="card p-5" aria-labelledby="storage-title">
		<h2 id="storage-title" class="font-semibold">Armazenamento</h2>
		<p class="mt-1 text-sm text-muted">
			As separações, os downloads e os envios ficam em <code class="text-text"
				>{info?.dataDir ?? '…'}</code
			>, até os apagares na <a href="/biblioteca" class="text-accent underline">Biblioteca</a>.
		</p>
		<p class="mt-3 text-2xl font-semibold tabular-nums">{info ? formatBytes(info.usage) : '…'}</p>
	</section>

	<section class="card p-5" aria-labelledby="theme-title">
		<h2 id="theme-title" class="font-semibold">Aspeto</h2>
		<div
			class="mt-3 inline-flex rounded-lg border border-line bg-panel-2 p-1"
			role="radiogroup"
			aria-label="Tema"
		>
			{#each themes as t (t.id)}
				<button
					class="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors {theme ===
					t.id
						? 'bg-raised font-medium text-text'
						: 'text-muted hover:text-text'}"
					role="radio"
					aria-checked={theme === t.id}
					onclick={() => setTheme(t.id)}
				>
					<Icon name={t.icon} size={15} />{t.label}
				</button>
			{/each}
		</div>
	</section>

	<section class="card p-5" aria-labelledby="doctor-title">
		<div class="flex items-center justify-between gap-3">
			<h2 id="doctor-title" class="font-semibold">Diagnóstico</h2>
			<button class="btn btn-ghost text-sm" onclick={loadChecks}
				><Icon name="retry" size={15} /> Verificar</button
			>
		</div>
		<p class="mt-1 text-sm text-muted">
			O primeiro sítio a ver quando um download falha sem razão aparente.
		</p>
		{#if checks === undefined}
			<p class="mt-4 text-sm text-muted">A verificar…</p>
		{:else}
			<ul class="mt-4 divide-y divide-line">
				{#each checks as c (c.name)}
					<li class="flex gap-3 py-2.5">
						<Icon
							name={levelIcon[c.level].icon}
							size={17}
							class="mt-0.5 {levelIcon[c.level].cls}"
						/>
						<div class="min-w-0 flex-1 text-sm">
							<p>
								<span class="font-medium">{c.name}</span>
								<span class="break-words text-muted">{c.detail}</span>
							</p>
							{#if c.fix}<p class="mt-0.5 text-xs text-warn">→ {c.fix}</p>{/if}
						</div>
					</li>
				{/each}
			</ul>
			<p class="mt-3 text-sm font-medium">
				{#if checks.some((c) => c.level === 'fail')}
					<span class="text-bad">Há problemas por resolver.</span>
				{:else}
					<span class="text-ok">Tudo pronto.</span>
				{/if}
			</p>
		{/if}
	</section>
</div>
