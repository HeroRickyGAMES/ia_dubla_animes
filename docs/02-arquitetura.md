# 02 — Arquitetura do pipeline

Data: 2026-07-31

## Visão geral

```
entrada.mp4 ─▶ ffprobe ─▶ ffmpeg ─▶ 16k/48k wav
   │
   ├─▶ [sep.py] demucs ─▶ vocals.wav (voz) + nv.wav (fundo)
   │        │
   │        ├─▶ [asr.py] faster-whisper ─▶ asr.json (ja + timestamps + VAD)
   │        │        │
   │        │        └─▶ [diar] embeds de voz ─▶ clustering ─▶ speaker por fala
   │        │                    │
   │        │                    └─▶ [Go] perfil F0/spectral ─▶ sexo+idade por personagem
   │        │                             │
   │        │                             └─▶ validação × roteiro (script) ─▶ conflict?
   │        └─▶ refs/<spk>.wav (5-10 s limpos por personagem, do vocals)
   │
   ├─▶ [Go] translate ja→pt (endpoint gratuito, batching paralelo)
   │
   └─▶ [tts.py] OmniVoice ─▶ dubs/<line>.wav
            (prompt de clone cacheado por personagem; CPU float32)
   │
   ▼
[Go] timeline/alinhamento (janela da fala original; sem stretch; sem atropelo)
   ▼
[Go] mix ffmpeg: nv.wav (ducking) + dubs → loudnorm -14 LUFS → mux com vídeo
   ▼
final.mkv  +  lines.json  +  relatorio (docs/04-resultados.md)
```

## Decisões de design

| Tema | Decisão |
|------|---------|
| Idioma | JA → PT (via endpoint gratuito do Google Translate; fallback: Ollama local / Argos) |
| Transcrição | faster-whisper `small`/`medium` em CPU, word timestamps + VAD |
| Falante | clustering de embeddings de voz (speechbrain ECAPA) — clusters sem rótulo |
| Sexo/idade | heurística em Go: F0 (autocorrelação) + centroide espectral + HNR |
| Voz de cada personagem | clone zero-shot a partir de `refs/<spk>.wav` (trecho do `vocals` do próprio anime) |
| Fala dublada | não esticar (`stretch-max=0`), não acelerar; janela = fala original |
| Fundo | demucs `other` com ducking durante as falas; original com voz removida |
| Master | loudnorm -14 LUFS, fade in/out do episódio |

## JSONs intermediários (workdir `work/`)

- `asr.json` — `[{id, start, end, text_ja, words:[{w,start,end}], prob}]`
- `diar.json` — `{seg_id: spk_cluster_id}` (não é identidade, é número)
- `profile.json` — `{spk_id: {gender, age, pitch_hz, hnr, centroid, conf, conflict, script_role}}`
- `lines.json` — junção final: `[{id, start, end, ja, pt, speaker, gender, age,
  dub_file, dub_len, dub_place, flags[]}]`
- `report.json` — tempos por estágio + estimativa para 24 min

## Paralelismo (otimização de CPU 12 threads)

- `extract` e `probe` rodam em paralelo.
- `sep.py` e `asr.py` são independentes se o ASR usar o áudio original
  (qualidade "rápida"); por padrão o ASR espera o `vocals` (qualidade alta).
  Flag `--fast-asr` permite rodar em paralelo sobre o original.
- Tradução: batch paralelo (goroutines + worker pool, respeitando limite de
  requisições do endpoint gratuito).
- TTS: sequencial por linha, mas com prompt de clone **cacheado por
  personagem** (o ref não é re-encodado por linha).

## Ferramentas

- ffmpeg/ffprobe (binários do sistema)
- faster-whisper (MIT) — ASR JA
- speechbrain ECAPA (Apache-2.0) — embeddings de voz p/ diarização
- demucs (MIT) — separação voz × fundo
- OmniVoice k2-fsa (Apache-2.0) — TTS com clonagem zero-shot
- Google Translate endpoint gratuito (`translate.googleapis.com/translate_a/single?client=gtx`)
- Todas com `.venv` do projeto, CPU only.
