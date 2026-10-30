<script lang="ts">
	import './layout.css';
	import favicon from '#lib/assets/favicon.svg';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { live } from '#lib/events.svelte.js';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	onMount(() => live.connect());

	// O aviso de ligação só aparece se a falha durar: ao abrir a página há
	// sempre um instante antes de o EventSource ligar.
	let offline = $state(false);
	$effect(() => {
		if (live.connected) {
			offline = false;
			return;
		}
		const t = setTimeout(() => (offline = true), 1500);
		return () => clearTimeout(t);
	});

	const nav: { href: string; label: string; icon: IconName }[] = [
		{ href: '/', label: 'Novo', icon: 'plus' },
		{ href: '/trabalhos', label: 'Trabalhos', icon: 'list' },
		{ href: '/biblioteca', label: 'Biblioteca', icon: 'library' },
		{ href: '/definicoes', label: 'Definições', icon: 'settings' }
	];

	function current(href: string): boolean {
		const path = page.url.pathname;
		if (href === '/') return path === '/';
		// O estúdio pertence à biblioteca.
		if (href === '/biblioteca' && path.startsWith('/estudio')) return true;
		return path.startsWith(href);
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<div class="min-h-dvh md:grid md:grid-cols-[220px_1fr]">
	<aside
		class="sticky top-0 z-20 flex items-center gap-1 border-b border-line bg-bg/90 px-4 py-2 backdrop-blur md:h-dvh md:flex-col md:items-stretch md:gap-1 md:border-r md:border-b-0 md:px-3 md:py-5"
	>
		<a href="/" class="mr-2 flex items-center gap-2.5 md:mr-0 md:mb-6 md:px-2">
			<img src={favicon} alt="" class="size-7" />
			<span class="hidden text-base font-semibold tracking-tight sm:inline">dotboot</span>
		</a>
		<nav class="flex flex-1 gap-1 overflow-x-auto md:flex-none md:flex-col" aria-label="Principal">
			{#each nav as item (item.href)}
				<a
					href={item.href}
					aria-current={current(item.href) ? 'page' : undefined}
					class="flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium whitespace-nowrap transition-colors {current(
						item.href
					)
						? 'bg-panel-2 text-text'
						: 'text-muted hover:bg-panel hover:text-text'}"
				>
					<Icon name={item.icon} size={17} />
					<span class={item.href === '/' ? '' : 'hidden sm:inline'}>{item.label}</span>
					{#if item.href === '/trabalhos' && live.running > 0}
						<span
							class="ml-auto grid min-w-5 place-items-center rounded-full bg-accent px-1.5 text-xs font-semibold text-accent-ink"
							aria-label="{live.running} em curso"
						>
							{live.running}
						</span>
					{/if}
				</a>
			{/each}
		</nav>
	</aside>

	<main class="min-w-0">
		{#if offline}
			<div
				class="border-b border-warn/30 bg-warn/10 px-4 py-2 text-center text-sm text-warn"
				role="status"
			>
				A ligar ao dotboot… Se não passar disto, confirma que o <code>dotboot</code> está a correr.
			</div>
		{/if}
		{@render children()}
	</main>
</div>
