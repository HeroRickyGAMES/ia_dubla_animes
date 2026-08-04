# 04 — Resultados e estimativa (24 min)

Data de referência: 2026-08-03 (Run 2: isocronia calibrada — YameiiS01E01 EN→PT)

## Medições por estágio (máquina: 12 threads CPU, sem GPU)

Run completo: `./dub dub -i YameiiS01E01.mkv -o YameiiS01E01.dub.mkv --lang en`
(24 min, 341 falas, 5 personagens, 347 segmentos ASR + 18 splits de 2 vozes).

| Estágio | Tempo medido | Observação |
|---------|--------------|------------|
| probe + extract (ffmpeg) | ~0.1 s | cached entre runs |
| separação demucs (voz × fundo) | ~25 min (1ª vez) | cached depois; 24 min de áudio |
| ASR faster-whisper (en) | 141 s | small, int8, CPU |
| overlap SepFormer (2 vozes) | ~41 min | 128 falso-splits filtrados → 18 reais |
| diarização (ECAPA) + perfil de voz | ~33 s + refs 3.1 s | 5 clusters |
| tradução en→pt | ~17–39 s | 341 falas, workers paralelos |
| emoção por fala | ~10 s | cached |
| TTS OmniVoice (341 falas) | **13 450 s (~3.7 h)** | ~39–55 s/fala COM clone de voz |
| mix + mux (ffmpeg) | ~17 min | ducking + loudnorm -14 LUFS |
| **TOTAL** | **~5 h p/ 24 min de anime** | TTS domina |

## Premissas usadas na estimativa

- Episódio de 24 min, ~350-400 falas, ~5-10 personagens falantes.
- CPU 12 threads, ffmpeg com `-preset veryfast`/`ultrafast` no vídeo.
- Modelos: faster-whisper `small`; OmniVoice float32; demucs `htdemucs` 2-stems.
- **TTS com clone de voz domina (~90% do tempo)**: ~39–55 s/fala. Sem clone
  (voz padrão) era ~18 s/fala, porém sem identidade de personagem.

## Resultado do teste (clipe real — E2E completo)

- Entrada: `YameiiS01E01.mkv` (1440 s, áudio EN)
- Saída: `YameiiS01E01.dub.mkv` (1.47 GB, 1440 s, h264+aac 96 kHz stereo)
- Nº de falas dubladas: **341/341** (TTS n_ok=341, n_err=0)
- Personagens: 5 (clusters ECAPA) — refs de voz por personagem gerados
  (`work/refs/spk_*.wav` + prompts de clone cacheados `spk_*.pt`)
- Overlap: 18 segmentos com 2 vozes simultâneas divididos em 36 falas
- Mix: loudnorm input_i -14.58 LUFS (target -14), ducking ativo
- Qualidade observada: sotaque do dublador EN (limitação cross-lingual do
  OmniVoice, documentada), melhora vs. run sem clone (voz por personagem).

## Run 2 (2026-08-03): isocronia calibrada

Motivação: usuário reportou **assincronia** (voz dublada continua depois do
lábio fechar). Diagnóstico: a fala PT dublada era ~20–31% mais longa que a
janela original (mediana 1.20–1.31x); o timeline esticava 15% e cortava a
cauda (103 clipped). Janelas do ASR estavam corretas.

Fix: **TTS calibrado por fala** — `workers/tts.py` passa `duration = (end−start)*0.95`
ao `model.generate()` do OmniVoice (antes só `speed`), distribuído entre
sentenças proporcionalmente ao texto. Mesmo motor `k2-fsa/OmniVoice`, mesmos
clones cacheados. Também trim do artefato "at" melhorado (regra clássica +
curta + cluster colado), validado por ASR (1ª palavra preservada).

Resultado:
- **95.6% das falas cabem na janela original** (antes ~26–50%).
- Timeline: **clipped 103 → 23**, **stretched 36 → 2**, **long 233 → 15**
  (as 23 restantes são interjeições ultra-curtas tipo "Ah." com janela <0.3 s).
- Sync verificado por ASR word-timestamp em 3 trechos: dub começa ~0.1–0.2 s
  depois do lábio original (margem saudável, sem atropelar a fala seguinte).
- TTS do run 2: 3.7 h (341 falas, ~50–70 s/fala com `duration`).

## Lições

- O refs/ dir era apagado pelo stageOverlap e não recriado → TTS caía para
  voz padrão silenciosamente. Fix: recriar refs/dubs após invalidação.
- TTS pode falhar 1 fala em 341 por transiente de memória → retry (3x) +
  log de erros em `work/tts_errors.log` + pipeline falha se n_err>0.
- `stageOverlap` precisa ser idempotente: só reaplica split se o segmento
  original ainda existir no asr.json (evita duplicar ao retomar).
- `--speed` com default `1.0` no argparse sobrescrevia o `duration` do
  OmniVoice → fix: default `None` (usa `duration` a menos que --tts-speed).
- OmniVoice ignora `duration` abaixo de ~0.3 s: interjeições (janela <0.3 s)
  continuam longas; o timeline corta a cauda (aceitável, são 23/341).

