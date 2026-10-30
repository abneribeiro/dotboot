<script lang="ts">
	// A fila: o que está a correr, o que espera e o que terminou, em tempo
	// real pelo /api/events.
	import { api } from '#lib/api.js';
	import Icon from '#lib/components/Icon.svelte';
	import JobCard from '#lib/components/JobCard.svelte';
	import { live } from '#lib/events.svelte.js';
	import { isActive } from '#lib/jobs.js';

	let error = $state('');

	// Os mais recentes primeiro, com os que estão a correr no topo.
	const jobs = $derived(
		[...live.jobs].reverse().sort((a, b) => Number(isActive(b)) - Number(isActive(a)))
	);
	const finished = $derived(live.jobs.filter((j) => !isActive(j)));

	async function clearFinished() {
		error = '';
		try {
			await Promise.all(finished.map((j) => api.removeJob(j.id)));
		} catch (e) {
			error = (e as Error).message;
		}
	}
</script>

<svelte:head><title>Trabalhos · dotboot</title></svelte:head>

<div class="mx-auto max-w-3xl px-4 py-8 sm:px-6 sm:py-12">
	<div class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">Trabalhos</h1>
			<p class="mt-1 text-muted">Correm um de cada vez, por ordem. Podes fechar esta página.</p>
		</div>
		{#if finished.length > 0}
			<button class="btn" onclick={clearFinished}>
				<Icon name="x" size={16} /> Limpar os terminados
			</button>
		{/if}
	</div>

	{#if error}
		<p class="mt-4 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad" role="alert">{error}</p>
	{/if}

	<div class="mt-8 space-y-3">
		{#each jobs as job (job.id)}
			<JobCard {job} onerror={(m) => (error = m)} />
		{:else}
			<div class="card flex flex-col items-center gap-3 px-6 py-14 text-center">
				<Icon name="list" size={28} class="text-faint" />
				<p class="font-medium">Ainda não há trabalhos</p>
				<p class="max-w-sm text-sm text-muted">
					O que pedires aparece aqui, com o progresso em tempo real. Os resultados ficam na
					Biblioteca.
				</p>
				<a href="/" class="btn btn-primary mt-2"><Icon name="plus" size={16} /> Novo trabalho</a>
			</div>
		{/each}
	</div>
</div>
