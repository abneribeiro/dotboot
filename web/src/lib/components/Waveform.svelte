<script lang="ts">
	// A forma de onda de uma pista, desenhada num canvas a partir dos picos que
	// o servidor mediu. A parte já tocada fica com a cor cheia; clicar ou
	// arrastar salta para esse ponto da música.
	import { resamplePeaks } from '#lib/mixer.js';

	let {
		peaks,
		color,
		progress = 0,
		dimmed = false,
		height = 64,
		label,
		onseek
	}: {
		peaks: number[];
		color: string;
		progress?: number;
		dimmed?: boolean;
		height?: number;
		label: string;
		onseek?: (fraction: number) => void;
	} = $props();

	let canvas: HTMLCanvasElement | undefined = $state();
	let width = $state(0);
	let dragging = false;

	const BAR = 2;
	const GAP = 1;

	$effect(() => {
		if (!canvas || width === 0) return;
		const dpr = window.devicePixelRatio || 1;
		canvas.width = Math.round(width * dpr);
		canvas.height = Math.round(height * dpr);
		const g = canvas.getContext('2d');
		if (!g) return;
		g.setTransform(dpr, 0, 0, dpr, 0, 0);
		g.clearRect(0, 0, width, height);

		const bars = resamplePeaks(peaks, Math.floor(width / (BAR + GAP)));
		const mid = height / 2;
		const played = progress * width;
		bars.forEach((v, i) => {
			const x = i * (BAR + GAP);
			// Um mínimo de 1px: o silêncio também se vê.
			const h = Math.max(1, v * (height - 4));
			g.globalAlpha = x + BAR <= played ? 1 : 0.38;
			g.fillStyle = color;
			g.fillRect(x, mid - h / 2, BAR, h);
		});
	});

	function seekAt(e: PointerEvent) {
		if (!onseek || !canvas) return;
		const rect = canvas.getBoundingClientRect();
		onseek(Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)));
	}

	function onkeydown(e: KeyboardEvent) {
		if (!onseek) return;
		const step = e.shiftKey ? 0.1 : 0.02;
		if (e.key === 'ArrowRight') onseek(Math.min(1, progress + step));
		else if (e.key === 'ArrowLeft') onseek(Math.max(0, progress - step));
		else return;
		e.preventDefault();
	}
</script>

<div
	class="relative w-full cursor-pointer touch-none transition-[filter,opacity] duration-150 select-none"
	class:opacity-35={dimmed}
	class:grayscale={dimmed}
	style="height: {height}px"
	bind:clientWidth={width}
	role="slider"
	tabindex="-1"
	aria-label={label}
	aria-valuemin="0"
	aria-valuemax="100"
	aria-valuenow={Math.round(progress * 100)}
	onpointerdown={(e) => {
		dragging = true;
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		seekAt(e);
	}}
	onpointermove={(e) => dragging && seekAt(e)}
	onpointerup={() => (dragging = false)}
	onpointercancel={() => (dragging = false)}
	{onkeydown}
>
	<canvas bind:this={canvas} class="absolute inset-0 h-full w-full"></canvas>
	<div
		class="pointer-events-none absolute inset-y-0 w-px bg-text"
		style="left: {progress * 100}%"
	></div>
</div>
