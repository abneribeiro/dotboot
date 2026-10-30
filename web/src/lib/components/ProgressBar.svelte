<script lang="ts">
	// Barra de progresso: com fraction < 0 é indeterminada (uma faixa que
	// atravessa a barra), para as etapas sem percentagem (juntar, converter).
	let {
		fraction,
		color = 'var(--c-accent)',
		label = 'progresso'
	}: { fraction: number; color?: string; label?: string } = $props();

	const indeterminate = $derived(fraction < 0);
	const percent = $derived(Math.round(Math.min(1, Math.max(0, fraction)) * 100));
</script>

<div
	class="relative h-1.5 w-full overflow-hidden rounded-full bg-raised"
	role="progressbar"
	aria-label={label}
	aria-valuemin="0"
	aria-valuemax="100"
	aria-valuenow={indeterminate ? undefined : percent}
>
	{#if indeterminate}
		<div
			class="absolute inset-y-0 w-2/5 rounded-full"
			style="background: {color}; animation: indeterminate 1.3s ease-in-out infinite"
		></div>
	{:else}
		<div
			class="h-full rounded-full transition-[width] duration-200 ease-out"
			style="width: {percent}%; background: {color}"
		></div>
	{/if}
</div>
