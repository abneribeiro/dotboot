// O leitor do estúdio: toca as pistas de uma separação em simultâneo.
//
// Cada pista é um <audio> (o browser vai buscando os dados aos poucos, com
// Range, em vez de descodificar a música inteira em memória: 6 pistas de 5
// minutos seriam ~600 MB) ligado a um GainNode da Web Audio. O mute, o solo
// e o volume só mexem no ganho, com uma rampa curta, sem cortes nem
// recomeços. A primeira pista é a mestra: dá o tempo, e a cada frame as
// outras que se afastem mais de 40 ms são realinhadas com ela.

import { trackURL } from './api.js';
import { effectiveGains, newTrack, tracksToResync, type TrackState } from './mixer.js';

/** Constante de tempo da rampa de ganho: evita o clique de um corte seco. */
const GAIN_RAMP = 0.015;

export class StudioPlayer {
	tracks = $state<TrackState[]>([]);
	playing = $state(false);
	time = $state(0);
	duration = $state(0);
	loop = $state(false);
	master = $state(1);
	/** Todas as pistas já têm dados para começar. */
	ready = $state(false);
	/** Uma pista ficou sem dados a meio e as outras esperam por ela. */
	buffering = $state(false);
	error = $state('');

	/** O maior desvio medido entre pistas, em segundos (diagnóstico). */
	maxDrift = 0;
	/** Quantas vezes uma pista teve de ser realinhada (diagnóstico). */
	resyncs = 0;

	#audios: HTMLAudioElement[] = [];
	#gains: GainNode[] = [];
	#ctx: AudioContext | undefined;
	#raf = 0;
	#destroyed = false;

	constructor(session: string, stems: string[], duration: number) {
		this.tracks = stems.map(newTrack);
		this.duration = duration;
		this.#audios = stems.map((stem) => {
			const a = new Audio();
			a.preload = 'auto';
			a.src = trackURL(session, stem);
			a.addEventListener('canplay', () => this.#onCanPlay());
			a.addEventListener('waiting', () => this.#onWaiting());
			a.addEventListener('error', () => {
				this.error = `não foi possível carregar a pista ${stem}`;
			});
			return a;
		});
		const master = this.#audios[0];
		master?.addEventListener('loadedmetadata', () => {
			if (Number.isFinite(master.duration) && master.duration > 0) this.duration = master.duration;
		});
		master?.addEventListener('ended', () => this.#onEnded());
	}

	/**
	 * Cria o grafo da Web Audio. Tem de ser depois de um gesto do utilizador
	 * (o browser não deixa tocar som antes), e só uma vez por <audio>.
	 */
	#ensureGraph() {
		if (this.#ctx) return;
		const ctx = new AudioContext();
		this.#gains = this.#audios.map((a) => {
			const source = ctx.createMediaElementSource(a);
			const gain = ctx.createGain();
			source.connect(gain).connect(ctx.destination);
			return gain;
		});
		this.#ctx = ctx;
		this.applyGains();
	}

	/** Aplica o mute, o solo e os volumes (chamado por um $effect). */
	applyGains() {
		const gains = effectiveGains(this.tracks, this.master);
		const ctx = this.#ctx;
		if (!ctx) return;
		this.#gains.forEach((g, i) => g.gain.setTargetAtTime(gains[i], ctx.currentTime, GAIN_RAMP));
	}

	#allReady(): boolean {
		return this.#audios.every((a) => a.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA);
	}

	#onCanPlay() {
		if (!this.#allReady()) return;
		this.ready = true;
		if (this.buffering && this.playing) {
			this.buffering = false;
			this.#alignAll(this.#audios[0].currentTime);
			void this.#playAll();
		}
	}

	#onWaiting() {
		// Uma pista a tocar sozinha à frente das outras desalinhava tudo:
		// param todas até haver dados para todas.
		if (!this.playing || this.buffering) return;
		this.buffering = true;
		this.#audios.forEach((a) => a.pause());
	}

	#onEnded() {
		if (this.loop) {
			this.seek(0);
			void this.#playAll();
			return;
		}
		this.#stopLoop();
		this.playing = false;
		this.#audios.forEach((a) => a.pause());
		this.time = this.duration;
	}

	#alignAll(t: number) {
		for (const a of this.#audios) {
			if (Math.abs(a.currentTime - t) > 0.001) a.currentTime = t;
		}
	}

	async #playAll() {
		try {
			await Promise.all(this.#audios.map((a) => a.play()));
		} catch (e) {
			if ((e as DOMException)?.name !== 'AbortError') {
				this.error = 'o browser não deixou tocar: carrega outra vez no play';
				this.pause();
			}
		}
	}

	async play() {
		if (this.#destroyed || this.playing) return;
		this.#ensureGraph();
		await this.#ctx?.resume();
		if (this.time >= this.duration - 0.05) this.time = 0;
		this.error = '';
		this.#alignAll(this.time);
		this.playing = true;
		if (!this.#allReady()) {
			// Arranca quando todas tiverem dados (#onCanPlay).
			this.buffering = true;
		} else {
			await this.#playAll();
		}
		this.#startLoop();
	}

	pause() {
		if (!this.playing) return;
		this.playing = false;
		this.buffering = false;
		this.#stopLoop();
		this.#audios.forEach((a) => a.pause());
		this.time = this.#audios[0]?.currentTime ?? this.time;
		this.#alignAll(this.time);
	}

	toggle() {
		if (this.playing) this.pause();
		else void this.play();
	}

	seek(t: number) {
		const clamped = Math.min(Math.max(0, t), this.duration || 0);
		this.time = clamped;
		this.#alignAll(clamped);
	}

	/** Solo exclusivo (clique com Alt/Shift noutras DAWs): só esta pista. */
	soloOnly(index: number) {
		this.tracks.forEach((t, i) => (t.solo = i === index));
	}

	/** Volta a ouvir tudo: sem solos nem mutes. */
	resetMix() {
		this.tracks.forEach((t) => {
			t.solo = false;
			t.muted = false;
		});
	}

	#startLoop() {
		this.#stopLoop();
		const tick = () => {
			const master = this.#audios[0];
			if (master && this.playing) {
				this.time = master.currentTime;
				if (!this.buffering && !master.paused) {
					const times = this.#audios.map((a) => a.currentTime);
					for (const t of times)
						this.maxDrift = Math.max(this.maxDrift, Math.abs(t - master.currentTime));
					for (const i of tracksToResync(master.currentTime, times)) {
						if (i === 0) continue;
						this.#audios[i].currentTime = master.currentTime;
						this.resyncs++;
					}
				}
			}
			this.#raf = requestAnimationFrame(tick);
		};
		this.#raf = requestAnimationFrame(tick);
	}

	#stopLoop() {
		cancelAnimationFrame(this.#raf);
		this.#raf = 0;
	}

	/** O desvio atual entre a mestra e as outras (diagnóstico). */
	currentDrift(): number {
		const master = this.#audios[0];
		if (!master) return 0;
		return Math.max(0, ...this.#audios.map((a) => Math.abs(a.currentTime - master.currentTime)));
	}

	destroy() {
		this.#destroyed = true;
		this.#stopLoop();
		for (const a of this.#audios) {
			a.pause();
			a.removeAttribute('src');
			a.load();
		}
		void this.#ctx?.close();
	}
}
